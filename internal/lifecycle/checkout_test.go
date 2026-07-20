package lifecycle

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andresbott/dibs/internal/baseline"
	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/internal/ident"
	"github.com/andresbott/dibs/internal/marker"
	"github.com/andresbott/dibs/libs/threewayrsync"
)

// requireRsync skips tests that drive the real engine when rsync is not on PATH
// (the design assumes it is installed, so this is a CI safety valve, not a mock).
func requireRsync(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not on PATH")
	}
}

func fixture(t *testing.T) (local, remote string) {
	t.Helper()
	t.Setenv("DIBS_STATE", t.TempDir())
	root := t.TempDir()
	local = filepath.Join(root, "local")
	remote = filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "file.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	return local, remote
}

func testIdent() ident.Ident { return ident.Ident{By: "me@host", Host: "host"} }

func TestCheckoutWritesMarkerAndEmptyBaseline(t *testing.T) {
	local, remote := fixture(t)
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	r := Runner{ToolVersion: "test", Now: func() time.Time { return time.Unix(1000, 0).UTC() }}

	rep, err := r.Checkout(context.Background(), "work", p, testIdent(), "", Options{})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	m, ok, err := marker.Read(remote)
	if err != nil || !ok {
		t.Fatalf("marker: ok=%v err=%v", ok, err)
	}
	if !m.OwnedBy("me@host", "host", "") || m.Profile != "work" {
		t.Errorf("marker = %+v", m)
	}
	st, ok, err := baseline.Load("work")
	if err != nil || !ok {
		t.Fatalf("state: ok=%v err=%v", ok, err)
	}
	if len(st.Files) != 0 {
		t.Errorf("baseline must be empty, got %d files", len(st.Files))
	}
	if len(st.Relpaths) != 1 || st.Relpaths[0] != "." {
		t.Errorf("relpaths = %v", st.Relpaths)
	}
	// No file was copied: checkout is marker-only.
	if _, err := os.Stat(filepath.Join(local, "file.txt")); !os.IsNotExist(err) {
		t.Error("checkout must not copy files")
	}
	if rep.Marker == nil {
		t.Error("report must carry the marker")
	}
}

func TestCheckoutRefusesForeignMarker(t *testing.T) {
	local, remote := fixture(t)
	_ = marker.Write(remote, &marker.Marker{CheckedOutBy: "other@laptop", Host: "laptop", Profile: "work"})
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, testIdent(), "", Options{}); err == nil {
		t.Fatal("must refuse a foreign marker")
	}
}

func TestCheckoutForceOverridesForeignMarker(t *testing.T) {
	local, remote := fixture(t)
	_ = marker.Write(remote, &marker.Marker{CheckedOutBy: "other@laptop", Host: "laptop", Profile: "work"})
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, testIdent(), "", Options{Force: true}); err != nil {
		t.Fatalf("force checkout: %v", err)
	}
	m, _, _ := marker.Read(remote)
	if !m.OwnedBy("me@host", "host", "") {
		t.Errorf("marker = %+v", m)
	}
}

// Two profiles on the SAME machine (same identity, same host) pointing at the
// same remote root are distinct lock holders: the second profile's checkout
// must refuse the first one's marker instead of treating it as its own
// (issue #14). Force still steals it, like any foreign lock.
func TestCheckoutRefusesOtherProfileSameMachine(t *testing.T) {
	_, remote := fixture(t)
	id := testIdent()
	pA := config.Profile{ID: "uuid-a", LocalRoot: t.TempDir() + "/a", RemoteRoot: remote}
	pB := config.Profile{ID: "uuid-b", LocalRoot: t.TempDir() + "/b", RemoteRoot: remote}
	if _, err := (Runner{}).Checkout(context.Background(), "profA", pA, id, "", Options{}); err != nil {
		t.Fatalf("first checkout: %v", err)
	}
	if _, err := (Runner{}).Checkout(context.Background(), "profB", pB, id, "", Options{}); err == nil {
		t.Fatal("a second profile on the same machine must not pass the first profile's ownership check")
	}
	m, _, _ := marker.Read(remote)
	if m.ProfileID != "uuid-a" {
		t.Errorf("marker profile_id = %q, want uuid-a (untouched by the refused checkout)", m.ProfileID)
	}
	// The lock is foreign to profB, so Force steals it like any other.
	if _, err := (Runner{}).Checkout(context.Background(), "profB", pB, id, "", Options{Force: true}); err != nil {
		t.Fatalf("force checkout: %v", err)
	}
	m, _, _ = marker.Read(remote)
	if m.ProfileID != "uuid-b" {
		t.Errorf("marker profile_id = %q, want uuid-b after the steal", m.ProfileID)
	}
}

func TestCheckoutRefusesSelfHeld(t *testing.T) {
	local, remote := fixture(t)
	id := testIdent()
	_ = marker.Write(remote, &marker.Marker{CheckedOutBy: id.By, Host: id.Host, Profile: "work"})
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, id, "", Options{}); err == nil {
		t.Fatal("must refuse an already-held profile")
	}
}

func TestCheckoutRefusesNonEmptyLocalTarget(t *testing.T) {
	local, remote := fixture(t)
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "existing.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	// The guard is absolute: force does not bypass it.
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, testIdent(), "", Options{Force: true}); err == nil {
		t.Fatal("must refuse a non-empty local target")
	}
	if _, ok, _ := marker.Read(remote); ok {
		t.Error("no marker may be written on refusal")
	}
}

// The vacancy guard is absolute, so it must fail closed: a target it cannot
// stat (permission denied) is not proof of vacancy.
func TestCheckoutRefusesUnstatableLocalTarget(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permission bits do not block root")
	}
	local, remote := fixture(t)
	// local/blocked/target exists but a 0-perm parent makes it unstatable.
	target := filepath.Join(local, "blocked", "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "data.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(local, "blocked"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(local, "blocked"), 0o755) })

	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, testIdent(), "blocked/target", Options{}); err == nil {
		t.Fatal("an unstatable local target must refuse, not pass as vacant")
	}
	if _, ok, _ := marker.Read(remote); ok {
		t.Error("no marker may be written on refusal")
	}
}

func TestCheckoutAllowsEmptyLocalTarget(t *testing.T) {
	local, remote := fixture(t)
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, testIdent(), "", Options{}); err != nil {
		t.Fatalf("checkout over empty dir: %v", err)
	}
}

func TestCheckoutDryRunWritesNothing(t *testing.T) {
	local, remote := fixture(t)
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	rep, err := (Runner{}).Checkout(context.Background(), "work", p, testIdent(), "", Options{DryRun: true})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !rep.DryRun {
		t.Error("report must be flagged dry-run")
	}
	if _, ok, _ := marker.Read(remote); ok {
		t.Error("dry-run must not write a marker")
	}
	if _, ok, _ := baseline.Load("work"); ok {
		t.Error("dry-run must not write state")
	}
}

func TestCheckoutUnmountedRemoteRefuses(t *testing.T) {
	t.Setenv("DIBS_STATE", t.TempDir())
	p := config.Profile{LocalRoot: t.TempDir(), RemoteRoot: filepath.Join(t.TempDir(), "missing")}
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, testIdent(), "", Options{}); err == nil {
		t.Fatal("must refuse an unmounted remote root")
	}
}

func TestCheckoutRelpathScopesState(t *testing.T) {
	local, remote := fixture(t)
	if err := os.MkdirAll(filepath.Join(remote, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, testIdent(), "docs", Options{}); err != nil {
		t.Fatalf("checkout: %v", err)
	}
	st, _, _ := baseline.Load("work")
	if len(st.Relpaths) != 1 || st.Relpaths[0] != "docs" {
		t.Errorf("relpaths = %v", st.Relpaths)
	}
	m, _, _ := marker.Read(remote)
	if len(m.Relpaths) != 1 || m.Relpaths[0] != "docs" {
		t.Errorf("marker relpaths = %v", m.Relpaths)
	}
}

// Relpath omitted = all declared subpaths (not the whole root). The
// recorded relpaths drive the first sync's scope, so seeding them from the
// profile keeps sync from pulling out-of-scope trees the UnlistedLocal guard
// would then refuse to push back.
func TestCheckoutSeedsRelpathsFromSubpaths(t *testing.T) {
	local, remote := fixture(t)
	for _, d := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(remote, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p := config.Profile{LocalRoot: local, RemoteRoot: remote, Subpaths: []string{"a", "b"}}
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, testIdent(), "", Options{}); err != nil {
		t.Fatalf("checkout: %v", err)
	}
	st, _, _ := baseline.Load("work")
	if len(st.Relpaths) != 2 || st.Relpaths[0] != "a" || st.Relpaths[1] != "b" {
		t.Errorf("relpaths = %v, want [a b]", st.Relpaths)
	}
	m, _, _ := marker.Read(remote)
	if len(m.Relpaths) != 2 || m.Relpaths[0] != "a" || m.Relpaths[1] != "b" {
		t.Errorf("marker relpaths = %v, want [a b]", m.Relpaths)
	}
}

// An explicit relpath outside the declared subpaths must refuse: checking out
// an undeclared subtree wedges every later sync/checkin on the unlisted-local
// guard, and the tempting exit (delete the local files) propagates deletions.
// A relpath UNDER a declared subpath, and any relpath on a profile with no
// declarations, remain fine.
func TestCheckoutRefusesRelpathOutsideSubpaths(t *testing.T) {
	local, remote := fixture(t)
	for _, d := range []string{"a", "a/inner", "other"} {
		if err := os.MkdirAll(filepath.Join(remote, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p := config.Profile{LocalRoot: local, RemoteRoot: remote, Subpaths: []string{"a"}}
	_, err := (Runner{}).Checkout(context.Background(), "work", p, testIdent(), "other", Options{})
	if err == nil {
		t.Fatal("checkout of a relpath outside the declared subpaths must refuse")
	}
	if !strings.Contains(err.Error(), "subpaths") {
		t.Errorf("error should point at the declared subpaths, got: %v", err)
	}
	if _, ok, _ := marker.Read(remote); ok {
		t.Error("no marker may be written on refusal")
	}
	// A nested relpath under a declared subpath is covered.
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, testIdent(), "a/inner", Options{}); err != nil {
		t.Fatalf("nested relpath under a declared subpath must be allowed: %v", err)
	}
}

// A this-machine re-checkout with a new relpath widens the
// pulled set under the same lock — it must not refuse, must keep the existing
// baseline manifest, and must record the union of relpaths.
func TestCheckoutWidensHeldRelpaths(t *testing.T) {
	requireRsync(t)
	local, remote := fixture(t)
	for _, d := range []string{"docs", "notes"} {
		if err := os.MkdirAll(filepath.Join(remote, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(remote, "docs", "d.txt"), []byte("D"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := testIdent()
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, id, "docs", Options{}); err != nil {
		t.Fatal(err)
	}
	// Pull docs down so the baseline holds real entries.
	if _, err := (Runner{}).Sync(context.Background(), "work", p, id, "", Options{}); err != nil {
		t.Fatal(err)
	}

	if _, err := (Runner{}).Checkout(context.Background(), "work", p, id, "notes", Options{}); err != nil {
		t.Fatalf("widening checkout: %v", err)
	}
	st, _, _ := baseline.Load("work")
	if len(st.Relpaths) != 2 || st.Relpaths[0] != "docs" || st.Relpaths[1] != "notes" {
		t.Errorf("relpaths = %v, want [docs notes]", st.Relpaths)
	}
	if _, ok := st.Files["docs/d.txt"]; !ok {
		t.Error("widening must preserve the existing baseline manifest")
	}
	m, _, _ := marker.Read(remote)
	if len(m.Relpaths) != 2 {
		t.Errorf("marker relpaths = %v, want [docs notes]", m.Relpaths)
	}
	if !m.OwnedBy(id.By, id.Host, p.ID) {
		t.Error("lock ownership must be untouched by widening")
	}
}

// Re-checking out a relpath already covered by the held set is a no-op ask:
// refuse with the existing "already checked out" guidance.
func TestCheckoutRefusesAlreadyCoveredRelpath(t *testing.T) {
	local, remote := fixture(t)
	if err := os.MkdirAll(filepath.Join(remote, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	id := testIdent()
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, id, "docs", Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, id, "docs", Options{}); err == nil {
		t.Fatal("must refuse a relpath already held")
	}
	// The whole root covers everything: adding under it is also refused.
	local2, remote2 := fixture(t)
	p2 := config.Profile{LocalRoot: local2, RemoteRoot: remote2}
	if _, err := (Runner{}).Checkout(context.Background(), "whole", p2, id, "", Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := (Runner{}).Checkout(context.Background(), "whole", p2, id, "docs", Options{}); err == nil {
		t.Fatal("must refuse a relpath under an already-held whole root")
	}
}

// racingAccessor simulates the claim race on a shared filesystem: reads report
// no marker until a write happens; after OUR write, reads return the FOREIGN
// marker (the other machine wrote last... from our Read's point of view it won).
type racingAccessor struct {
	winner *marker.Marker
	wrote  bool
}

func (a *racingAccessor) Read(context.Context) (*marker.Marker, bool, error) {
	if a.wrote {
		return a.winner, true, nil
	}
	return nil, false, nil
}
func (a *racingAccessor) Write(_ context.Context, _ *marker.Marker) error {
	a.wrote = true
	return nil
}
func (a *racingAccessor) Remove(context.Context) error { return nil }

// Two machines checking out simultaneously can both pass the "no marker" check;
// the verify read-back must detect the lost race, roll the baseline back, and
// report who holds the lock — instead of both machines believing they own it.
func TestCheckoutDetectsLostClaimRace(t *testing.T) {
	t.Setenv("DIBS_STATE", t.TempDir())
	local, remote := fixture(t)
	foreign := &marker.Marker{CheckedOutBy: "other@laptop", Host: "laptop", Profile: "work"}
	r := Runner{
		ToolVersion: "test",
		NewAccessor: func(threewayrsync.Endpoint) marker.Accessor { return &racingAccessor{winner: foreign} },
	}
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	_, err := r.Checkout(context.Background(), "work", p, testIdent(), "", Options{})
	if err == nil {
		t.Fatal("a lost claim race must fail the checkout")
	}
	if !strings.Contains(err.Error(), "other@laptop") {
		t.Errorf("error should name the winning holder, got: %v", err)
	}
	if _, ok, _ := baseline.Load("work"); ok {
		t.Error("the loser's baseline must be rolled back")
	}
}

func TestCheckoutRollsBackBaselineOnMarkerFailure(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("read-only dirs do not block root")
	}
	local, remote := fixture(t)
	// A read-only remote root lets every check pass but fails the marker write.
	if err := os.Chmod(remote, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(remote, 0o755) })
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, testIdent(), "", Options{}); err == nil {
		t.Fatal("marker write must fail on a read-only remote")
	}
	if _, ok, _ := baseline.Load("work"); ok {
		t.Error("baseline must be rolled back when the marker write fails")
	}
}
