package lifecycle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andresbott/dibs/internal/baseline"
	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/internal/marker"
)

// TestCheckinReleasesWhenInSync: heldFixture leaves local == remote == base,
// so the profile is already in sync; checkin releases it — removing the marker and
// clearing the state — without moving any data.
func TestCheckinReleasesWhenInSync(t *testing.T) {
	name, p, id := heldFixture(t)
	remote := config.ExpandRoot(p.RemoteRoot)

	rep, err := (Runner{ToolVersion: "test"}).Checkin(context.Background(), name, p, id, Options{})
	if err != nil {
		t.Fatalf("checkin of an in-sync profile: %v", err)
	}
	if _, ok, _ := marker.Read(remote); ok {
		t.Error("marker must be removed after checkin")
	}
	if _, ok, _ := baseline.Load(name); ok {
		t.Error("state must be cleared after checkin")
	}
	if !rep.Released {
		t.Error("report should mark the profile released")
	}
	// The data itself is untouched: the remote keeps its files.
	if _, err := os.Stat(filepath.Join(remote, "file.txt")); err != nil {
		t.Errorf("remote content must survive checkin: %v", err)
	}
}

func TestCheckinCleanRemovesLocalCopy(t *testing.T) {
	name, p, id := heldFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	if _, err := (Runner{ToolVersion: "test"}).Checkin(context.Background(), name, p, id, Options{Clean: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(local, "file.txt")); !os.IsNotExist(err) {
		t.Error("--clean should remove the local working copy")
	}
}

// --clean is os.RemoveAll of the local root: a config mistake pointing the
// local root at the home directory (or /) must refuse up front, before the
// marker or baseline are touched, so nothing is released and nothing deleted.
func TestCheckinCleanRefusesHomeDir(t *testing.T) {
	name, p, id := heldFixture(t)
	remote := config.ExpandRoot(p.RemoteRoot)
	// Pretend the user's home IS the local root (the dangerous misconfig).
	t.Setenv("HOME", config.ExpandRoot(p.LocalRoot))

	_, err := (Runner{ToolVersion: "test"}).Checkin(context.Background(), name, p, id, Options{Clean: true})
	if err == nil {
		t.Fatal("checkin --clean must refuse to remove the home directory")
	}
	// Nothing was released: marker and baseline still there, data intact.
	if _, ok, _ := marker.Read(remote); !ok {
		t.Error("marker must be left in place on refusal")
	}
	if _, ok, _ := baseline.Load(name); !ok {
		t.Error("baseline must be left in place on refusal")
	}
	if _, statErr := os.Stat(filepath.Join(config.ExpandRoot(p.LocalRoot), "file.txt")); statErr != nil {
		t.Error("local working copy must be untouched on refusal")
	}
}

func TestCheckinCleanRefusesShallowRoot(t *testing.T) {
	for _, root := range []string{"/", "/home", "/etc"} {
		if err := validateCleanTarget(root); err == nil {
			t.Errorf("validateCleanTarget(%q) must refuse", root)
		}
	}
	if err := validateCleanTarget("/home/someone/checkouts/photos"); err != nil {
		t.Errorf("a deep working-copy path must be allowed, got %v", err)
	}
}

// TestCheckinRefusesUnsyncedChanges: a pending local edit (a push that sync would
// carry) makes the profile un-releasable. checkin fails, pushes nothing, surfaces
// the pending change, and leaves the marker in place. There is no --force.
func TestCheckinRefusesUnsyncedChanges(t *testing.T) {
	name, p, id := heldFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	remote := config.ExpandRoot(p.RemoteRoot)
	if err := os.WriteFile(filepath.Join(local, "file.txt"), []byte("EDITED-LONGER"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := (Runner{ToolVersion: "test"}).Checkin(context.Background(), name, p, id, Options{})
	if err == nil {
		t.Fatal("checkin must refuse a profile with unsynced changes")
	}
	if len(rep.Pushed) != 1 || rep.Pushed[0] != "file.txt" {
		t.Errorf("report should surface the pending push, got Pushed=%v", rep.Pushed)
	}
	// Nothing was pushed to the remote, and the marker stays.
	if got, _ := os.ReadFile(filepath.Join(remote, "file.txt")); string(got) != "data" {
		t.Errorf("checkin must not push; remote file.txt = %q, want data", got)
	}
	if _, ok, _ := marker.Read(remote); !ok {
		t.Error("marker must remain when checkin is refused")
	}
}

func TestCheckinConflictKeepsMarker(t *testing.T) {
	name, p, id := heldFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	remote := config.ExpandRoot(p.RemoteRoot)
	if err := os.WriteFile(filepath.Join(local, "file.txt"), []byte("local-version"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "file.txt"), []byte("R"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := (Runner{ToolVersion: "test"}).Checkin(context.Background(), name, p, id, Options{})
	if err == nil {
		t.Fatal("want a refusal when the same file changed on both sides")
	}
	if len(rep.Conflicts) == 0 {
		t.Error("report should surface the conflicting path")
	}
	if _, ok, _ := marker.Read(remote); !ok {
		t.Error("marker must remain after a conflict-stopped checkin")
	}
}

// --abandon is the sanctioned "start over" path: it releases the lock and
// clears the state even when unsynced local changes exist, moving no data —
// the remote keeps its files exactly as they are.
func TestCheckinAbandonReleasesDespiteUnsyncedChanges(t *testing.T) {
	name, p, id := heldFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	remote := config.ExpandRoot(p.RemoteRoot)
	// An unsynced local edit that a plain checkin would refuse over.
	if err := os.WriteFile(filepath.Join(local, "file.txt"), []byte("EDITED-LONGER"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := (Runner{ToolVersion: "test"}).Checkin(context.Background(), name, p, id, Options{Abandon: true})
	if err != nil {
		t.Fatalf("checkin --abandon: %v", err)
	}
	if !rep.Released || !rep.Abandoned {
		t.Errorf("report should mark the profile released+abandoned, got %+v", rep)
	}
	if _, ok, _ := marker.Read(remote); ok {
		t.Error("marker must be removed after abandon")
	}
	if _, ok, _ := baseline.Load(name); ok {
		t.Error("state must be cleared after abandon")
	}
	// The remote keeps its (pre-edit) content; the local edit stays local.
	if got, _ := os.ReadFile(filepath.Join(remote, "file.txt")); string(got) != "data" {
		t.Errorf("abandon must not push; remote file.txt = %q, want data", got)
	}
	if got, _ := os.ReadFile(filepath.Join(local, "file.txt")); string(got) != "EDITED-LONGER" {
		t.Errorf("abandon without --clean must keep the local copy; got %q", got)
	}
}

// --abandon --clean is the full start-over: release the lock AND remove the
// local working copy, so a fresh checkout finds a vacant target.
func TestCheckinAbandonCleanRemovesLocalCopy(t *testing.T) {
	name, p, id := heldFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	if err := os.WriteFile(filepath.Join(local, "file.txt"), []byte("EDITED-LONGER"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Runner{ToolVersion: "test"}).Checkin(context.Background(), name, p, id, Options{Abandon: true, Clean: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(local); !os.IsNotExist(err) {
		t.Error("--abandon --clean should remove the local working copy")
	}
	// The full start-over round trip: checkout again succeeds on the vacant target.
	if _, err := (Runner{ToolVersion: "test"}).Checkout(context.Background(), name, p, id, "", Options{}); err != nil {
		t.Fatalf("re-checkout after abandon --clean: %v", err)
	}
}

// Abandon must still respect the cooperative lock: a foreign marker refuses.
func TestCheckinAbandonRefusesForeignMarker(t *testing.T) {
	name, p, id := heldFixture(t)
	remote := config.ExpandRoot(p.RemoteRoot)
	_ = marker.Write(remote, &marker.Marker{CheckedOutBy: "other@laptop", Host: "laptop", Profile: name})
	if _, err := (Runner{ToolVersion: "test"}).Checkin(context.Background(), name, p, id, Options{Abandon: true}); err == nil {
		t.Fatal("abandon must refuse a foreign marker")
	}
	if m, _, _ := marker.Read(remote); m == nil || m.CheckedOutBy != "other@laptop" {
		t.Errorf("foreign marker must be preserved, got %+v", m)
	}
}

// A dry-run abandon previews without releasing anything.
func TestCheckinAbandonDryRunReleasesNothing(t *testing.T) {
	name, p, id := heldFixture(t)
	remote := config.ExpandRoot(p.RemoteRoot)
	rep, err := (Runner{ToolVersion: "test"}).Checkin(context.Background(), name, p, id, Options{Abandon: true, DryRun: true})
	if err != nil {
		t.Fatalf("dry-run abandon: %v", err)
	}
	if rep.Released {
		t.Error("dry run must not release")
	}
	if !rep.Abandoned {
		t.Error("report must mark the dry run as an abandon preview")
	}
	if _, ok, _ := marker.Read(remote); !ok {
		t.Error("marker must remain after a dry-run abandon")
	}
	if _, ok, _ := baseline.Load(name); !ok {
		t.Error("state must remain after a dry-run abandon")
	}
}

func TestCheckinDryRunPreviewsWithoutReleasing(t *testing.T) {
	name, p, id := heldFixture(t)
	remote := config.ExpandRoot(p.RemoteRoot)
	rep, err := (Runner{ToolVersion: "test"}).Checkin(context.Background(), name, p, id, Options{DryRun: true})
	if err != nil {
		t.Fatalf("dry-run checkin: %v", err)
	}
	if rep.Released {
		t.Error("dry run must not release")
	}
	if _, ok, _ := marker.Read(remote); !ok {
		t.Error("marker must remain after a dry-run checkin")
	}
}

// Pending deletes (a local delete a plain sync skipped) block checkin like any
// other pending change: release requires --allow-deletes sync or --abandon.
func TestCheckinBlockedByPendingDeletes(t *testing.T) {
	name, p, id := heldFixture(t)
	local := config.ExpandRoot(p.LocalRoot)
	if err := os.WriteFile(filepath.Join(local, "other.txt"), []byte("O"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(local, "file.txt")); err != nil {
		t.Fatal(err)
	}
	// The plain sync leaves the delete pending — the profile is not in sync.
	if _, err := (Runner{}).Sync(context.Background(), name, p, id, "", Options{}); err != nil {
		t.Fatal(err)
	}
	rep, err := (Runner{}).Checkin(context.Background(), name, p, id, Options{})
	if err == nil || !strings.Contains(err.Error(), "unsynced changes") {
		t.Fatalf("checkin must refuse over a pending delete, got %v", err)
	}
	if len(rep.RemovedRemote) != 1 {
		t.Errorf("the pending delete must be listed as blocking, got %v", rep.RemovedRemote)
	}
	if rep.Released {
		t.Error("nothing may be released")
	}
}
