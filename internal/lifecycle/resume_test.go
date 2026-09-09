package lifecycle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/candy-tools/dibs/internal/baseline"
	"github.com/candy-tools/dibs/internal/config"
	"github.com/candy-tools/dibs/internal/ident"
	"github.com/candy-tools/dibs/internal/marker"
	"github.com/candy-tools/dibs/libs/threewayrsync"
)

// releasedFixture is heldFixture (checked out + synced: local == remote == base,
// remote holds file.txt) followed by a plain checkin, leaving the local copy on
// disk and the baseline kept as a released resume token.
func releasedFixture(t *testing.T) (name string, p config.Profile, id ident.Ident) {
	t.Helper()
	name, p, id = heldFixture(t)
	if _, err := (Runner{ToolVersion: "test"}).Checkin(context.Background(), name, p, id, Options{}); err != nil {
		t.Fatalf("fixture checkin: %v", err)
	}
	return name, p, id
}

func TestResumeAdoptsUntouchedLocalCopy(t *testing.T) {
	name, p, id := releasedFixture(t)
	remote := config.ExpandRoot(p.RemoteRoot)

	rep, err := (Runner{ToolVersion: "test"}).Checkout(context.Background(), name, p, id, "", Options{Resume: true})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !rep.Resumed {
		t.Error("report must be flagged resumed")
	}
	m, ok, _ := marker.Read(remote)
	if !ok || !m.OwnedBy(id.By, id.Host, p.ID) {
		t.Fatalf("marker after resume = %+v ok=%v", m, ok)
	}
	st, ok, _ := baseline.Load(name)
	if !ok || st.IsReleased() {
		t.Fatalf("resume must reactivate the baseline, got ok=%v released=%v", ok, ok && st.IsReleased())
	}
	if _, ok := st.Files["file.txt"]; !ok {
		t.Error("the adopted baseline must carry the verified local files")
	}
	// The first sync after a clean resume is a no-op.
	srep, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{})
	if err != nil {
		t.Fatalf("post-resume sync: %v", err)
	}
	if len(srep.Pulled)+len(srep.Pushed)+len(srep.RemovedLocal)+len(srep.RemovedRemote)+len(srep.Conflicts) != 0 {
		t.Errorf("post-resume sync must be a no-op, got %+v", srep)
	}
}

// Remote drift while released is resume's whole point: the released base tells
// the classifier which side moved, so a remote edit becomes a clean pull.
func TestResumeAfterRemoteEditPullsCleanly(t *testing.T) {
	name, p, id := releasedFixture(t)
	remote := config.ExpandRoot(p.RemoteRoot)
	local := config.ExpandRoot(p.LocalRoot)
	if err := os.WriteFile(filepath.Join(remote, "file.txt"), []byte("REMOTE-EDIT-LONGER"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := (Runner{ToolVersion: "test"}).Checkout(context.Background(), name, p, id, "", Options{Resume: true}); err != nil {
		t.Fatalf("resume must not inspect the remote: %v", err)
	}
	srep, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{})
	if err != nil {
		t.Fatalf("post-resume sync: %v", err)
	}
	if len(srep.Pulled) != 1 || srep.Pulled[0] != "file.txt" {
		t.Errorf("want a clean pull of file.txt, got %+v", srep)
	}
	if got, _ := os.ReadFile(filepath.Join(local, "file.txt")); string(got) != "REMOTE-EDIT-LONGER" {
		t.Errorf("local file.txt = %q after sync", got)
	}
}

// A base entry missing locally was cleaned up while released: it is pruned from
// the adopted baseline, so the first sync re-pulls it instead of propagating
// the disk cleanup to the remote as a deletion.
func TestResumePrunesLocallyDeletedFile(t *testing.T) {
	name, p, id := heldFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	if err := os.WriteFile(filepath.Join(local, "other.txt"), []byte("O"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := (Runner{ToolVersion: "test"}).Checkin(context.Background(), name, p, id, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(local, "other.txt")); err != nil {
		t.Fatal(err)
	}

	if _, err := (Runner{ToolVersion: "test"}).Checkout(context.Background(), name, p, id, "", Options{Resume: true}); err != nil {
		t.Fatalf("resume over a shrunk copy: %v", err)
	}
	st, _, _ := baseline.Load(name)
	if _, ok := st.Files["other.txt"]; ok {
		t.Error("the locally deleted file must be pruned from the adopted baseline")
	}
	srep, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(srep.Pulled) != 1 || srep.Pulled[0] != "other.txt" {
		t.Errorf("the pruned file must re-pull, got %+v", srep)
	}
	if len(srep.RemovedRemote)+len(srep.PendingRemote) != 0 {
		t.Errorf("a pruned file must never plan as a remote delete, got %+v", srep)
	}
}

func TestResumeRejectsModifiedLocalFile(t *testing.T) {
	name, p, id := releasedFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	if err := os.WriteFile(filepath.Join(local, "file.txt"), []byte("TOUCHED-WHILE-RELEASED"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := (Runner{ToolVersion: "test"}).Checkout(context.Background(), name, p, id, "", Options{Resume: true})
	if err == nil || !strings.Contains(err.Error(), "file.txt") {
		t.Fatalf("resume must refuse and name the touched file, got %v", err)
	}
	if _, ok, _ := marker.Read(config.ExpandRoot(p.RemoteRoot)); ok {
		t.Error("a refused resume must not write a marker")
	}
	if st, ok, _ := baseline.Load(name); !ok || !st.IsReleased() {
		t.Error("a refused resume must leave the released token intact")
	}
}

// A file added locally while released is safe to allow: kept OUT of the
// adopted baseline it is indistinguishable from a file created while checked
// out, so the first sync classifies it as a push (or a conflict when someone
// added the same path remotely) — never a delete on either side. Adopting it
// INTO the base would be the dangerous direction (the next sync would mirror
// a phantom remote deletion), which the baseline assertion below pins.
func TestResumeAllowsAddedLocalFile(t *testing.T) {
	name, p, id := releasedFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	remote := config.ExpandRoot(p.RemoteRoot)
	if err := os.MkdirAll(filepath.Join(local, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "sub", "stray.txt"), []byte("S"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := (Runner{ToolVersion: "test"}).Checkout(context.Background(), name, p, id, "", Options{Resume: true})
	if err != nil {
		t.Fatalf("resume over a locally added file must succeed, got %v", err)
	}
	if !rep.Resumed {
		t.Error("report must be flagged resumed")
	}
	st, _, _ := baseline.Load(name)
	for _, path := range []string{"sub/stray.txt", "sub"} {
		if _, ok := st.Files[path]; ok {
			t.Errorf("%s must stay OUT of the adopted baseline (adopting it would turn the next sync into a phantom local delete)", path)
		}
	}
	// The first sync pushes the addition — both sides end up in sync.
	srep, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{})
	if err != nil {
		t.Fatalf("post-resume sync: %v", err)
	}
	// Both the new directory and its file classify as pushes (sorted).
	if len(srep.Pushed) != 2 || srep.Pushed[0] != "sub" || srep.Pushed[1] != "sub/stray.txt" {
		t.Errorf("the added dir and file must classify as pushes, got %+v", srep)
	}
	if len(srep.RemovedLocal)+len(srep.PendingLocal) != 0 {
		t.Errorf("an added file must never plan as a local delete, got %+v", srep)
	}
	if got, err := os.ReadFile(filepath.Join(remote, "sub", "stray.txt")); err != nil || string(got) != "S" {
		t.Errorf("remote sub/stray.txt = %q err=%v after sync, want S", got, err)
	}
}

func TestResumeRequiresReleasedBaseline(t *testing.T) {
	local, remote := fixture(t)
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	_, err := (Runner{ToolVersion: "test"}).Checkout(context.Background(), "work", p, testIdent(), "", Options{Resume: true})
	if err == nil || !strings.Contains(err.Error(), "no released baseline") {
		t.Fatalf("resume without a released baseline must refuse, got %v", err)
	}
}

func TestResumeDetectsInterruptedResume(t *testing.T) {
	name, p, id := releasedFixture(t)
	remote := config.ExpandRoot(p.RemoteRoot)
	// Simulate an interrupted resume: state exists but is not released (ReleasedAt cleared), no marker
	st, ok, err := baseline.Load(name)
	if err != nil || !ok {
		t.Fatalf("load released state: ok=%v err=%v", ok, err)
	}
	st.ReleasedAt = time.Time{} // clear ReleasedAt as resumeCheckout would have done
	if err := baseline.Save(st); err != nil {
		t.Fatalf("save interrupted state: %v", err)
	}
	// Now the state is active but no marker exists
	_, err = (Runner{ToolVersion: "test"}).Checkout(context.Background(), name, p, id, "", Options{Resume: true})
	if err == nil {
		t.Fatal("resume must refuse when state is active but not released")
	}
	if !strings.Contains(err.Error(), "interrupted") || !strings.Contains(err.Error(), "remove the local state") {
		t.Errorf("error must mention interrupted resume and recovery, got: %v", err)
	}
	// Verify no marker was written
	if _, ok, _ := marker.Read(remote); ok {
		t.Error("refused resume must not write a marker")
	}
}

func TestResumeRefusesRelpath(t *testing.T) {
	name, p, id := releasedFixture(t)
	if _, err := (Runner{ToolVersion: "test"}).Checkout(context.Background(), name, p, id, "docs", Options{Resume: true}); err == nil {
		t.Fatal("resume with an explicit relpath must refuse")
	}
}

func TestResumeRefusesForeignMarker(t *testing.T) {
	name, p, id := releasedFixture(t)
	remote := config.ExpandRoot(p.RemoteRoot)
	_ = marker.Write(remote, &marker.Marker{CheckedOutBy: "other@laptop", Host: "laptop", Profile: name})
	if _, err := (Runner{ToolVersion: "test"}).Checkout(context.Background(), name, p, id, "", Options{Resume: true}); err == nil {
		t.Fatal("resume must respect the cooperative lock like any checkout")
	}
}

func TestResumeForceStealsForeignMarker(t *testing.T) {
	name, p, id := releasedFixture(t)
	remote := config.ExpandRoot(p.RemoteRoot)
	// Write a foreign marker
	foreign := &marker.Marker{CheckedOutBy: "other@laptop", Host: "laptop", Profile: name}
	if err := marker.Write(remote, foreign); err != nil {
		t.Fatalf("write foreign marker: %v", err)
	}
	// Resume without force must refuse
	if _, err := (Runner{ToolVersion: "test"}).Checkout(context.Background(), name, p, id, "", Options{Resume: true}); err == nil {
		t.Fatal("resume must refuse a foreign marker without --force")
	}
	// Resume with force must succeed
	rep, err := (Runner{ToolVersion: "test"}).Checkout(context.Background(), name, p, id, "", Options{Resume: true, Force: true})
	if err != nil {
		t.Fatalf("resume --force over foreign marker: %v", err)
	}
	if !rep.Resumed {
		t.Error("report must be flagged resumed")
	}
	// Marker must now be ours
	m, ok, _ := marker.Read(remote)
	if !ok {
		t.Fatal("marker must exist after forced resume")
	}
	if !m.OwnedBy(id.By, id.Host, p.ID) {
		t.Errorf("marker after forced resume must be owned by us, got %+v", m)
	}
}

func TestResumeRefusesChangedRoots(t *testing.T) {
	name, p, id := releasedFixture(t)
	p.RemoteRoot = t.TempDir() // reconfigured since release
	_, err := (Runner{ToolVersion: "test"}).Checkout(context.Background(), name, p, id, "", Options{Resume: true})
	if err == nil || !strings.Contains(err.Error(), "different roots") {
		t.Fatalf("resume must refuse a root-binding mismatch, got %v", err)
	}
}

func TestResumeDryRunWritesNothing(t *testing.T) {
	name, p, id := releasedFixture(t)
	rep, err := (Runner{ToolVersion: "test"}).Checkout(context.Background(), name, p, id, "", Options{Resume: true, DryRun: true})
	if err != nil {
		t.Fatalf("dry-run resume: %v", err)
	}
	if !rep.DryRun || !rep.Resumed {
		t.Errorf("report = %+v, want DryRun+Resumed", rep)
	}
	if _, ok, _ := marker.Read(config.ExpandRoot(p.RemoteRoot)); ok {
		t.Error("dry-run resume must not write a marker")
	}
	if st, _, _ := baseline.Load(name); !st.IsReleased() {
		t.Error("dry-run resume must leave the token released")
	}
}

// The plain-checkout vacancy refusal points at --resume when (and only when) a
// matching released token exists.
func TestVacancyRefusalHintsResume(t *testing.T) {
	name, p, id := releasedFixture(t)
	_, err := (Runner{ToolVersion: "test"}).Checkout(context.Background(), name, p, id, "", Options{})
	if err == nil || !strings.Contains(err.Error(), "--resume") {
		t.Fatalf("the vacancy refusal should hint at --resume, got %v", err)
	}
}

func TestHasResumeToken(t *testing.T) {
	name, p, id := releasedFixture(t)
	if !HasResumeToken(name, p) {
		t.Error("a released token against the same root must report true")
	}
	moved := p
	moved.LocalRoot = t.TempDir()
	if HasResumeToken(name, moved) {
		t.Error("a re-rooted profile must not offer resume")
	}
	// Resuming reactivates the state: the token is consumed.
	if _, err := (Runner{ToolVersion: "test"}).Checkout(context.Background(), name, p, id, "", Options{Resume: true}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if HasResumeToken(name, p) {
		t.Error("an active checkout must not offer resume")
	}
	if HasResumeToken("nonexistent", p) {
		t.Error("a profile with no state must not offer resume")
	}
}

func TestAdoptBaselineTable(t *testing.T) {
	mt := time.Unix(1000, 0).UTC()
	f := func(size int64) threewayrsync.FileState { return threewayrsync.FileState{Size: size, ModTime: mt} }
	base := threewayrsync.Manifest{
		"same.txt":    f(4),
		"gone.txt":    f(9),
		"touched.txt": f(7),
		"dir":         {IsDir: true},
	}
	local := threewayrsync.Manifest{
		"same.txt":    f(4),
		"touched.txt": f(8), // size drifted
		"extra.txt":   f(1), // not in base: an addition, allowed but not adopted
		"dir":         {IsDir: true},
		"newdir":      {IsDir: true}, // not in base: same rule as an added file
	}
	adopted, violations := adoptBaseline(base, local)
	if _, ok := adopted["same.txt"]; !ok {
		t.Error("matching entries must be adopted")
	}
	if _, ok := adopted["dir"]; !ok {
		t.Error("directory entries compare presence-only and must be adopted")
	}
	if _, ok := adopted["gone.txt"]; ok {
		t.Error("base entries missing locally are pruned, not adopted")
	}
	// Local-only entries are unsynced additions: no violation, but they must
	// stay OUT of the adopted base so the first sync classifies them as pushes
	// (adopted, they would read as phantom remote deletions and mirror back as
	// local deletes).
	for _, path := range []string{"extra.txt", "newdir"} {
		if _, ok := adopted[path]; ok {
			t.Errorf("locally added %s must not be adopted into the base", path)
		}
	}
	if len(violations) != 1 || !strings.Contains(violations[0], "touched.txt") {
		t.Fatalf("violations = %v, want only touched.txt", violations)
	}
}
