package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andresbott/netcheckout/internal/config"
	"github.com/andresbott/netcheckout/internal/ident"
	"github.com/andresbott/netcheckout/internal/marker"
)

// heldFixture builds a checked-out profile whose local and remote agree on one
// file, with the base established by a real first sync (so mtimes in the base
// come from rsync listings, exactly as production does).
func heldFixture(t *testing.T) (name string, p config.Profile, id ident.Ident) {
	t.Helper()
	requireRsync(t)
	local, remote := fixture(t) // remote holds file.txt; local missing
	id = testIdent()
	p = config.Profile{LocalRoot: local, RemoteRoot: remote}
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, id, "", Options{}); err != nil {
		t.Fatalf("fixture checkout: %v", err)
	}
	// First sync pulls file.txt down and records the base.
	if _, err := (Runner{}).Sync(context.Background(), "work", p, id, "", Options{}); err != nil {
		t.Fatalf("fixture first sync: %v", err)
	}
	return "work", p, id
}

func TestSyncFailFastWithoutMarker(t *testing.T) {
	name, p, id := heldFixture(t)
	_ = marker.Remove(config.ExpandRoot(p.RemoteRoot))
	if _, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{}); err == nil {
		t.Fatal("sync must fail fast when no marker exists")
	}
}

func TestSyncRefusesForeignMarker(t *testing.T) {
	name, p, id := heldFixture(t)
	remote := config.ExpandRoot(p.RemoteRoot)
	_ = marker.Write(remote, &marker.Marker{CheckedOutBy: "other@laptop", Host: "laptop", Profile: name})
	if _, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{}); err == nil {
		t.Fatal("sync must refuse a foreign marker")
	}
}

// Force resolves same-file conflicts local-wins; it must NEVER override the
// lock ownership check (GOALS §9.5, and the sync --force help text).
func TestSyncForceDoesNotOverrideForeignLock(t *testing.T) {
	name, p, id := heldFixture(t)
	remote := config.ExpandRoot(p.RemoteRoot)
	_ = marker.Write(remote, &marker.Marker{CheckedOutBy: "other@laptop", Host: "laptop", Profile: name})
	if _, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{Force: true}); err == nil {
		t.Fatal("sync --force must still refuse a foreign marker")
	}
	// The foreign marker is untouched.
	m, _, _ := marker.Read(remote)
	if m == nil || m.CheckedOutBy != "other@laptop" {
		t.Errorf("foreign marker must be preserved, got %+v", m)
	}
}

// Checkin has no --force at all (GOALS §9): a Force option set by any caller
// must not let it past a foreign lock.
func TestCheckinForceDoesNotOverrideForeignLock(t *testing.T) {
	name, p, id := heldFixture(t)
	remote := config.ExpandRoot(p.RemoteRoot)
	_ = marker.Write(remote, &marker.Marker{CheckedOutBy: "other@laptop", Host: "laptop", Profile: name})
	if _, err := (Runner{}).Checkin(context.Background(), name, p, id, Options{Force: true}); err == nil {
		t.Fatal("checkin must refuse a foreign marker regardless of Force")
	}
	if m, _, _ := marker.Read(remote); m == nil || m.CheckedOutBy != "other@laptop" {
		t.Errorf("foreign marker must be preserved, got %+v", m)
	}
}

func TestSyncFirstPullCopiesRemoteDown(t *testing.T) {
	requireRsync(t)
	local, remote := fixture(t)
	id := testIdent()
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, id, "", Options{}); err != nil {
		t.Fatal(err)
	}
	rep, err := (Runner{}).Sync(context.Background(), "work", p, id, "", Options{})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(local, "file.txt")); string(got) != "data" {
		t.Errorf("local file.txt = %q", got)
	}
	if len(rep.Pulled) != 1 || rep.Pulled[0] != "file.txt" {
		t.Errorf("rep.Pulled = %v", rep.Pulled)
	}
}

func TestSyncPushesLocalEdit(t *testing.T) {
	name, p, id := heldFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	remote := config.ExpandRoot(p.RemoteRoot)
	// Edit locally after checkout (different size => detected without mtime games).
	if err := os.WriteFile(filepath.Join(local, "file.txt"), []byte("EDITED-LONGER"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Runner{ToolVersion: "test", Now: func() time.Time { return time.Unix(500, 0).UTC() }}
	rep, err := r.Sync(context.Background(), name, p, id, "", Options{})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(remote, "file.txt")); string(got) != "EDITED-LONGER" {
		t.Errorf("remote file.txt = %q", got)
	}
	m, _, _ := marker.Read(remote)
	if !m.OwnedBy(id.By, id.Host) {
		t.Error("marker ownership must be preserved")
	}
	if !m.LastSyncAt.Equal(time.Unix(500, 0).UTC()) {
		t.Errorf("last_sync_at = %v", m.LastSyncAt)
	}
	if len(rep.Pushed) != 1 {
		t.Errorf("rep.Pushed = %v", rep.Pushed)
	}
}

func TestSyncForwardsApplyEvents(t *testing.T) {
	name, p, id := heldFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	if err := os.WriteFile(filepath.Join(local, "file.txt"), []byte("EDITED-LONGER"), 0o644); err != nil {
		t.Fatal(err)
	}
	var events []Event
	_, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{
		OnApply: func(e Event) { events = append(events, e) },
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	want := Event{Kind: EventModify, Side: SideRemote, Path: "file.txt"}
	if len(events) != 1 || events[0] != want {
		t.Fatalf("events = %+v, want [%+v]", events, want)
	}
}

func TestSyncAddEventForNewFile(t *testing.T) {
	name, p, id := heldFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	if err := os.WriteFile(filepath.Join(local, "new.txt"), []byte("N"), 0o644); err != nil {
		t.Fatal(err)
	}
	var events []Event
	if _, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{
		OnApply: func(e Event) { events = append(events, e) },
	}); err != nil {
		t.Fatal(err)
	}
	want := Event{Kind: EventAdd, Side: SideRemote, Path: "new.txt"}
	if len(events) != 1 || events[0] != want {
		t.Fatalf("events = %+v, want [%+v]", events, want)
	}
}

func TestSyncDryRunEmitsNoEventsAndWritesNothing(t *testing.T) {
	name, p, id := heldFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	remote := config.ExpandRoot(p.RemoteRoot)
	if err := os.WriteFile(filepath.Join(local, "file.txt"), []byte("EDITED-LONGER"), 0o644); err != nil {
		t.Fatal(err)
	}
	var events []Event
	rep, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{
		DryRun:  true,
		OnApply: func(e Event) { events = append(events, e) },
	})
	if err != nil {
		t.Fatalf("dry-run sync: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("dry run must emit no events, got %+v", events)
	}
	if len(rep.Pushed) != 1 {
		t.Errorf("dry-run plan: %+v", rep)
	}
	if got, _ := os.ReadFile(filepath.Join(remote, "file.txt")); string(got) != "data" {
		t.Errorf("dry run wrote to the remote: %q", got)
	}
}

func TestSyncConflictStops(t *testing.T) {
	name, p, id := heldFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	remote := config.ExpandRoot(p.RemoteRoot)
	// Edit both sides to different sizes => conflict.
	if err := os.WriteFile(filepath.Join(local, "file.txt"), []byte("local-version"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "file.txt"), []byte("R"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{})
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want ConflictError", err)
	}
	if len(rep.Conflicts) != 1 || rep.Conflicts[0] != "file.txt" {
		t.Errorf("rep.Conflicts = %v", rep.Conflicts)
	}
	// Nothing was written on either side.
	if got, _ := os.ReadFile(filepath.Join(local, "file.txt")); string(got) != "local-version" {
		t.Errorf("local overwritten: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(remote, "file.txt")); string(got) != "R" {
		t.Errorf("remote overwritten: %q", got)
	}
}

func TestSyncForceResolvesConflictLocalWins(t *testing.T) {
	name, p, id := heldFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	remote := config.ExpandRoot(p.RemoteRoot)
	if err := os.WriteFile(filepath.Join(local, "file.txt"), []byte("local-version"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "file.txt"), []byte("R"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{Force: true})
	if err != nil {
		t.Fatalf("force sync: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(remote, "file.txt")); string(got) != "local-version" {
		t.Errorf("remote = %q, want local content", got)
	}
	if len(rep.Pushed) != 1 {
		t.Errorf("rep.Pushed = %v", rep.Pushed)
	}
}

func TestSyncDryRunOverConflictDoesNotError(t *testing.T) {
	name, p, id := heldFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	remote := config.ExpandRoot(p.RemoteRoot)
	if err := os.WriteFile(filepath.Join(local, "file.txt"), []byte("local-version"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "file.txt"), []byte("R"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{DryRun: true})
	if err != nil {
		t.Fatalf("dry-run over conflict: %v", err)
	}
	if len(rep.Conflicts) != 1 {
		t.Errorf("rep.Conflicts = %v", rep.Conflicts)
	}
}

// A local delete is NOT propagated by a plain sync: the delete is skipped and
// reported pending, and the remote copy is not pulled back (no resurrection).
// AllowDeletes propagates it.
func TestSyncLocalDeletePendingUntilAllowDeletes(t *testing.T) {
	name, p, id := heldFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	remote := config.ExpandRoot(p.RemoteRoot)
	// A second synced file keeps the delete below a full wipe of the remote.
	if err := os.WriteFile(filepath.Join(local, "other.txt"), []byte("O"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(local, "file.txt")); err != nil {
		t.Fatal(err)
	}
	rep, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(remote, "file.txt")); err != nil {
		t.Error("plain sync must NOT delete the remote file")
	}
	if _, err := os.Stat(filepath.Join(local, "file.txt")); !os.IsNotExist(err) {
		t.Error("plain sync must NOT resurrect the locally deleted file")
	}
	if len(rep.PendingRemote) != 1 || len(rep.RemovedRemote) != 0 {
		t.Errorf("want 1 pending remote delete and none removed, got pending=%v removed=%v", rep.PendingRemote, rep.RemovedRemote)
	}
	// Still pending on the next run.
	rep, err = (Runner{}).Sync(context.Background(), name, p, id, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.PendingRemote) != 1 {
		t.Errorf("pending delete must persist across runs, got %v", rep.PendingRemote)
	}
	// AllowDeletes propagates it.
	rep, err = (Runner{}).Sync(context.Background(), name, p, id, "", Options{AllowDeletes: true})
	if err != nil {
		t.Fatalf("sync --allow-deletes: %v", err)
	}
	if _, err := os.Stat(filepath.Join(remote, "file.txt")); !os.IsNotExist(err) {
		t.Error("--allow-deletes must delete the remote file")
	}
	if len(rep.RemovedRemote) != 1 || len(rep.PendingRemote) != 0 {
		t.Errorf("want 1 removed remote and none pending, got removed=%v pending=%v", rep.RemovedRemote, rep.PendingRemote)
	}
}

func TestSyncScopedRelpathOnlyTouchesScope(t *testing.T) {
	requireRsync(t)
	local, remote := fixture(t)
	// Remote gets a second top-level dir beside file.txt.
	if err := os.MkdirAll(filepath.Join(remote, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "docs", "d.txt"), []byte("D"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := testIdent()
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, id, "", Options{}); err != nil {
		t.Fatal(err)
	}
	// Scoped sync of docs only: file.txt must not be pulled.
	rep, err := (Runner{}).Sync(context.Background(), "work", p, id, "docs", Options{})
	if err != nil {
		t.Fatalf("scoped sync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(local, "docs", "d.txt")); err != nil {
		t.Errorf("scoped file not pulled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(local, "file.txt")); !os.IsNotExist(err) {
		t.Error("out-of-scope file must not be pulled")
	}
	// The scope dir itself is pulled too (directories are tracked entries).
	if len(rep.Pulled) != 2 || rep.Pulled[0] != "docs" || rep.Pulled[1] != "docs/d.txt" {
		t.Errorf("rep.Pulled = %v", rep.Pulled)
	}
}

// A sync relpath outside the checkout envelope must refuse: it would pull a
// subtree the recorded relpaths never cover, invisible to later unscoped syncs
// and to checkin's in-sync verification (which --clean would then delete).
func TestSyncRefusesRelpathOutsideEnvelope(t *testing.T) {
	requireRsync(t)
	local, remote := fixture(t)
	if err := os.MkdirAll(filepath.Join(remote, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "docs", "d.txt"), []byte("D"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := testIdent()
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	// Envelope = [docs] via an explicit checkout relpath.
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, id, "docs", Options{}); err != nil {
		t.Fatal(err)
	}
	// Syncing a relpath outside the envelope refuses and points at checkout.
	_, err := (Runner{}).Sync(context.Background(), "work", p, id, "other", Options{})
	if err == nil {
		t.Fatal("sync of a relpath outside the checkout envelope must refuse")
	}
	if !strings.Contains(err.Error(), "checkout") {
		t.Errorf("error should point at widening via checkout, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(local, "other")); !os.IsNotExist(statErr) {
		t.Error("the refused sync must not have pulled anything")
	}
	// A relpath inside the envelope still works.
	if _, err := (Runner{}).Sync(context.Background(), "work", p, id, "docs", Options{}); err != nil {
		t.Fatalf("in-envelope scoped sync: %v", err)
	}
}

// A missing local root while the baseline records synced files is what an
// unmounted local disk (or a wrongly deleted working copy) looks like.
// Recreating it as an empty dir would classify every baseline file as a local
// delete and push the deletions to the remote — sync must refuse instead.
func TestSyncRefusesMissingLocalRootWithBaseline(t *testing.T) {
	name, p, id := heldFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	remote := config.ExpandRoot(p.RemoteRoot)
	if err := os.RemoveAll(local); err != nil {
		t.Fatal(err)
	}
	if _, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{}); err == nil {
		t.Fatal("sync must refuse a missing local root when the baseline records files")
	}
	// The remote is untouched.
	if got, _ := os.ReadFile(filepath.Join(remote, "file.txt")); string(got) != "data" {
		t.Errorf("remote file.txt = %q, want untouched", got)
	}
	// And the local root was NOT recreated.
	if _, err := os.Stat(local); !os.IsNotExist(err) {
		t.Error("sync must not create the local root over a non-empty baseline")
	}
}

// A dry-run right after checkout (local root not yet created) must preview the
// pull plan without creating the local root — GOALS §9.5: dry-run mutates nothing.
func TestSyncDryRunFreshCheckoutCreatesNothing(t *testing.T) {
	requireRsync(t)
	local, remote := fixture(t)
	id := testIdent()
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, id, "", Options{}); err != nil {
		t.Fatal(err)
	}
	rep, err := (Runner{}).Sync(context.Background(), "work", p, id, "", Options{DryRun: true})
	if err != nil {
		t.Fatalf("dry-run sync: %v", err)
	}
	if len(rep.Pulled) != 1 || rep.Pulled[0] != "file.txt" {
		t.Errorf("rep.Pulled = %v", rep.Pulled)
	}
	if _, err := os.Stat(local); !os.IsNotExist(err) {
		t.Error("dry-run must not create the local root")
	}
}

// Checkin only diffs (it copies nothing), so it too must not create the local
// root as a side effect.
func TestCheckinFreshCheckoutCreatesNoLocalRoot(t *testing.T) {
	requireRsync(t)
	local, remote := fixture(t)
	id := testIdent()
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, id, "", Options{}); err != nil {
		t.Fatal(err)
	}
	// Refuses (the remote file is an un-pulled change) — and mutates nothing.
	if _, err := (Runner{}).Checkin(context.Background(), "work", p, id, Options{}); err == nil {
		t.Fatal("checkin must refuse an unsynced fresh checkout")
	}
	if _, err := os.Stat(local); !os.IsNotExist(err) {
		t.Error("checkin must not create the local root")
	}
}

// The baseline is keyed by profile name; if the profile's roots were edited
// since the checkout, merging against the old manifest would manufacture
// deletes and conflicts against the wrong trees. Sync must refuse instead.
func TestSyncRefusesEditedRoots(t *testing.T) {
	name, p, id := heldFixture(t)
	// Re-point the profile at different roots, as a config edit would.
	edited := p
	edited.RemoteRoot = t.TempDir()
	// Keep it "checked out" at the new remote so preflight reaches the state check.
	if err := marker.Write(edited.RemoteRoot, &marker.Marker{CheckedOutBy: id.By, Host: id.Host, Profile: name}); err != nil {
		t.Fatal(err)
	}
	_, err := (Runner{}).Sync(context.Background(), name, edited, id, "", Options{})
	if err == nil {
		t.Fatal("sync must refuse when the profile's roots changed since checkout")
	}
	if !strings.Contains(err.Error(), "root") {
		t.Errorf("error should mention the root mismatch, got: %v", err)
	}
}

// Deleting every checked-out file locally (here: the only one) is what an
// emptied-to-start-over working copy looks like. A plain sync skips the delete
// (pending, remote intact); with --allow-deletes the engine refuses the 100%
// wipe — nothing waives it — and the error names the abandon recovery, never
// --allow-deletes.
func TestSyncFullWipeIsNeverApplied(t *testing.T) {
	name, p, id := heldFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	remote := config.ExpandRoot(p.RemoteRoot)
	if err := os.Remove(filepath.Join(local, "file.txt")); err != nil {
		t.Fatal(err)
	}
	rep, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{})
	if err != nil {
		t.Fatalf("plain sync must skip the delete and proceed: %v", err)
	}
	if len(rep.PendingRemote) != 1 {
		t.Errorf("want the wipe-looking delete pending, got %v", rep.PendingRemote)
	}
	if _, statErr := os.Stat(filepath.Join(remote, "file.txt")); statErr != nil {
		t.Errorf("plain sync must leave the remote intact: %v", statErr)
	}
	_, err = (Runner{}).Sync(context.Background(), name, p, id, "", Options{AllowDeletes: true})
	if err == nil {
		t.Fatal("--allow-deletes must still refuse a 100% wipe")
	}
	if !strings.Contains(err.Error(), "--abandon") {
		t.Errorf("error must name the abandon recovery, got: %v", err)
	}
	if strings.Contains(err.Error(), "--allow-deletes") {
		t.Errorf("error must not suggest --allow-deletes (it no longer waives the wipe): %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(remote, "file.txt")); statErr != nil {
		t.Errorf("refused sync must leave the remote intact: %v", statErr)
	}
}

// The wipe valve is symmetric: a remote emptied of every synced file must not
// wipe the local working copy — pending with the flag off, refused with it on.
func TestSyncRefusesFullLocalWipeFromEmptiedRemote(t *testing.T) {
	name, p, id := heldFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	remote := config.ExpandRoot(p.RemoteRoot)
	if err := os.Remove(filepath.Join(remote, "file.txt")); err != nil {
		t.Fatal(err)
	}
	rep, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{})
	if err != nil {
		t.Fatalf("plain sync must skip the local delete and proceed: %v", err)
	}
	if len(rep.PendingLocal) != 1 {
		t.Errorf("want 1 pending local delete, got %v", rep.PendingLocal)
	}
	if _, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{AllowDeletes: true}); err == nil {
		t.Fatal("a sync wiping every local file from an emptied remote must refuse even with --allow-deletes")
	}
	if _, statErr := os.Stat(filepath.Join(local, "file.txt")); statErr != nil {
		t.Errorf("local copy must stay intact either way: %v", statErr)
	}
}

// A directory rename shows up as delete-all + add-all. A plain sync pushes the
// adds but skips the deletes (pending: the remote keeps the old dir alongside
// the new one); --allow-deletes then propagates the deletions.
func TestSyncRenameDeletesNeedAllowDeletes(t *testing.T) {
	requireRsync(t)
	local, remote := fixture(t)
	for i := 0; i < 12; i++ {
		dir := filepath.Join(remote, "docs")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.txt", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	id := testIdent()
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	if _, err := (Runner{}).Checkout(context.Background(), "work", p, id, "", Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := (Runner{}).Sync(context.Background(), "work", p, id, "", Options{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(local, "docs"), filepath.Join(local, "papers")); err != nil {
		t.Fatal(err)
	}
	rep, err := (Runner{}).Sync(context.Background(), "work", p, id, "", Options{})
	if err != nil {
		t.Fatalf("plain sync after rename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(remote, "papers", "f00.txt")); err != nil {
		t.Error("renamed dir must land on the remote even without --allow-deletes")
	}
	if _, err := os.Stat(filepath.Join(remote, "docs", "f00.txt")); err != nil {
		t.Error("old files must NOT be deleted without --allow-deletes")
	}
	// 12 files + the docs dir itself.
	if len(rep.PendingRemote) != 13 {
		t.Errorf("want 13 pending remote deletes, got %d", len(rep.PendingRemote))
	}
	if _, err := (Runner{}).Sync(context.Background(), "work", p, id, "", Options{AllowDeletes: true}); err != nil {
		t.Fatalf("sync --allow-deletes after rename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(remote, "docs", "f00.txt")); !os.IsNotExist(err) {
		t.Error("old files must be deleted from the remote with --allow-deletes")
	}
	if _, err := os.Stat(filepath.Join(remote, "docs")); !os.IsNotExist(err) {
		t.Error("the emptied dir itself must be deleted from the remote with --allow-deletes")
	}
}
