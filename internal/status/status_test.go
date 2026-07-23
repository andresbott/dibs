package status

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/andresbott/dibs/internal/baseline"
	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/internal/marker"
	"github.com/andresbott/dibs/libs/threewayrsync"
)

func requireRsync(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not on PATH")
	}
}

// fixture creates local/ and remote/ dirs under a fresh temp root and points
// DIBS_STATE at a temp state dir so baseline.Load/Save resolve there. It
// returns the profile name, profile, and the two roots.
func fixture(t *testing.T) (name string, p config.Profile, local, remote string) {
	t.Helper()
	requireRsync(t) // Compute enumerates via the real engine
	root := t.TempDir()
	local = filepath.Join(root, "local")
	remote = filepath.Join(root, "remote")
	for _, d := range []string{local, remote} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DIBS_STATE", t.TempDir())
	return "prof", config.Profile{LocalRoot: local, RemoteRoot: remote}, local, remote
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func markCheckedOut(t *testing.T, remote string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(remote, marker.FileName), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// baselineFromLocal fingerprints the local tree (size + second-truncated mtime,
// as rsync listings do) and saves it as the profile's base manifest, mimicking
// the state a checkout+sync leaves behind.
func baselineFromLocal(t *testing.T, name, local string) {
	t.Helper()
	files := threewayrsync.Manifest{}
	err := filepath.WalkDir(local, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == marker.FileName || !d.Type().IsRegular() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(local, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = threewayrsync.FileState{Size: info.Size(), ModTime: info.ModTime()}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := baseline.Save(&baseline.State{Profile: name, Relpaths: []string{"."}, Files: files}); err != nil {
		t.Fatal(err)
	}
}

// Status is a read-only preview: a fresh checkout whose local root does not
// exist yet must be reported as all-pulls without creating the directory.
func TestComputeDoesNotCreateLocalRoot(t *testing.T) {
	name, p, local, remote := fixture(t)
	if err := os.RemoveAll(local); err != nil {
		t.Fatal(err)
	}
	writeFile(t, remote, "file.txt", "data")
	markCheckedOut(t, remote)
	// Fresh-checkout state: empty baseline.
	if err := baseline.Save(&baseline.State{Profile: name, Relpaths: []string{"."}, Files: threewayrsync.Manifest{}}); err != nil {
		t.Fatal(err)
	}
	st, err := Compute(context.Background(), name, p, "")
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if len(st.Targets) != 1 || len(st.Targets[0].Pull) != 1 {
		t.Errorf("want one pull, got %#v", st.Targets)
	}
	if _, err := os.Stat(local); !os.IsNotExist(err) {
		t.Error("status must not create the local root")
	}
}

func TestComputeNotCheckedOut(t *testing.T) {
	name, p, _, _ := fixture(t)
	st, err := Compute(context.Background(), name, p, "")
	if err != nil {
		t.Fatal(err)
	}
	if st.CheckedOut {
		t.Error("want CheckedOut false when no marker is present")
	}
}

func TestComputeCheckedOutNoBaseline(t *testing.T) {
	name, p, _, remote := fixture(t)
	markCheckedOut(t, remote)
	// No baseline saved: checked out, but not on this machine.
	st, err := Compute(context.Background(), name, p, "")
	if err != nil {
		t.Fatal(err)
	}
	if !st.CheckedOut || st.HasBaseline {
		t.Errorf("want CheckedOut true, HasBaseline false; got %#v", st)
	}
}

func TestComputeInSync(t *testing.T) {
	name, p, local, remote := fixture(t)
	writeFile(t, local, "a.txt", "hello")
	writeFile(t, remote, "a.txt", "hello")
	markCheckedOut(t, remote)
	baselineFromLocal(t, name, local)

	st, err := Compute(context.Background(), name, p, "")
	if err != nil {
		t.Fatal(err)
	}
	if !st.InSync() {
		t.Errorf("want in sync, got %#v", st)
	}
	// A whole-root profile resolves to a single "(root)" target.
	if len(st.Targets) != 1 || st.Targets[0].Label() != "(root)" {
		t.Errorf("want one (root) target, got %#v", st.Targets)
	}
}

// only returns the single target of a whole-root profile's status, failing if
// the shape is unexpected.
func only(t *testing.T, st ProfileStatus) TargetStatus {
	t.Helper()
	if len(st.Targets) != 1 {
		t.Fatalf("want exactly one target, got %#v", st.Targets)
	}
	return st.Targets[0]
}

// TestComputeRemoteDeleteIsLocalDelete is the regression guard: a file that was
// in the baseline and is still local but has been deleted on the remote must be
// previewed as a local deletion (what sync does), never as a push that would
// resurrect it on the remote.
func TestComputeRemoteDeleteIsLocalDelete(t *testing.T) {
	name, p, local, remote := fixture(t)
	writeFile(t, local, "song.flac", "audio")
	writeFile(t, remote, "song.flac", "audio")
	markCheckedOut(t, remote)
	baselineFromLocal(t, name, local)

	// Delete on the remote only.
	if err := os.Remove(filepath.Join(remote, "song.flac")); err != nil {
		t.Fatal(err)
	}

	st, err := Compute(context.Background(), name, p, "")
	if err != nil {
		t.Fatal(err)
	}
	tt := only(t, st)
	if len(tt.Push) != 0 {
		t.Errorf("remote delete must not become a push; got Push=%#v", tt.Push)
	}
	if len(tt.LocalDeletes) != 1 || tt.LocalDeletes[0] != "song.flac" {
		t.Errorf("want LocalDeletes=[song.flac], got %#v", tt.LocalDeletes)
	}
}

func TestComputeLocalDeletePropagatesToRemote(t *testing.T) {
	name, p, local, remote := fixture(t)
	writeFile(t, local, "notes.md", "x")
	writeFile(t, remote, "notes.md", "x")
	markCheckedOut(t, remote)
	baselineFromLocal(t, name, local)

	// Delete on the local side only.
	if err := os.Remove(filepath.Join(local, "notes.md")); err != nil {
		t.Fatal(err)
	}

	st, err := Compute(context.Background(), name, p, "")
	if err != nil {
		t.Fatal(err)
	}
	tt := only(t, st)
	if len(tt.RemoteDeletes) != 1 || tt.RemoteDeletes[0] != "notes.md" {
		t.Errorf("want RemoteDeletes=[notes.md], got %#v", tt.RemoteDeletes)
	}
}

func TestComputeLocalAddIsPush(t *testing.T) {
	name, p, local, remote := fixture(t)
	markCheckedOut(t, remote)
	baselineFromLocal(t, name, local) // empty baseline
	writeFile(t, local, "new.txt", "fresh")

	st, err := Compute(context.Background(), name, p, "")
	if err != nil {
		t.Fatal(err)
	}
	tt := only(t, st)
	if len(tt.Push) != 1 || tt.Push[0].Path != "new.txt" || tt.Push[0].Modify {
		t.Errorf("want a single add push of new.txt, got %#v", tt.Push)
	}
}

func TestComputeRemoteAddIsPull(t *testing.T) {
	name, p, local, remote := fixture(t)
	markCheckedOut(t, remote)
	baselineFromLocal(t, name, local) // empty baseline
	writeFile(t, remote, "incoming.txt", "fresh")

	st, err := Compute(context.Background(), name, p, "")
	if err != nil {
		t.Fatal(err)
	}
	tt := only(t, st)
	if len(tt.Pull) != 1 || tt.Pull[0].Path != "incoming.txt" || tt.Pull[0].Modify {
		t.Errorf("want a single add pull of incoming.txt, got %#v", tt.Pull)
	}
}

// A remote-only file matching the profile's ignore patterns is reported under
// Ignored — never as a pull — and the target still reads in sync, so a
// .DS_Store dropped on the share by mounting it can't block a checkin.
func TestComputeIgnoredIsNotAPull(t *testing.T) {
	name, p, local, remote := fixture(t)
	p.Ignore = []string{".DS_Store"}
	writeFile(t, local, "a.txt", "hello")
	writeFile(t, remote, "a.txt", "hello")
	markCheckedOut(t, remote)
	baselineFromLocal(t, name, local)
	writeFile(t, remote, ".DS_Store", "finder junk")

	st, err := Compute(context.Background(), name, p, "")
	if err != nil {
		t.Fatal(err)
	}
	tt := only(t, st)
	if len(tt.Pull) != 0 {
		t.Errorf("ignored file must not be pulled; got Pull=%#v", tt.Pull)
	}
	if len(tt.Ignored) != 1 || tt.Ignored[0] != ".DS_Store" {
		t.Errorf("want Ignored=[.DS_Store], got %#v", tt.Ignored)
	}
	if !tt.InSync() {
		t.Errorf("an ignored file must not break in-sync; got %#v", tt)
	}
}

func TestComputeModifyFlaggedOnEditedFile(t *testing.T) {
	name, p, local, remote := fixture(t)
	writeFile(t, local, "doc.txt", "v1")
	writeFile(t, remote, "doc.txt", "v1")
	markCheckedOut(t, remote)
	baselineFromLocal(t, name, local)

	// Edit locally: an in-baseline file that changed is a modify push.
	writeFile(t, local, "doc.txt", "v2-longer")

	st, err := Compute(context.Background(), name, p, "")
	if err != nil {
		t.Fatal(err)
	}
	tt := only(t, st)
	if len(tt.Push) != 1 || !tt.Push[0].Modify {
		t.Errorf("want a modify push, got %#v", tt.Push)
	}
}

func TestComputeConflict(t *testing.T) {
	name, p, local, remote := fixture(t)
	writeFile(t, local, "f.txt", "base")
	writeFile(t, remote, "f.txt", "base")
	markCheckedOut(t, remote)
	baselineFromLocal(t, name, local)

	writeFile(t, local, "f.txt", "local-edit")
	writeFile(t, remote, "f.txt", "remote-edit")

	st, err := Compute(context.Background(), name, p, "")
	if err != nil {
		t.Fatal(err)
	}
	tt := only(t, st)
	if len(tt.Conflicts) != 1 || tt.Conflicts[0] != "f.txt" {
		t.Errorf("want Conflicts=[f.txt], got %#v", tt.Conflicts)
	}
}

func TestComputeRemoteNotMounted(t *testing.T) {
	name, p, _, _ := fixture(t)
	p.RemoteRoot = filepath.Join(t.TempDir(), "not-mounted")
	_, err := Compute(context.Background(), name, p, "")
	if err == nil || err.Error() != "remote root "+p.RemoteRoot+" is not mounted" {
		t.Fatalf("err = %v, want not-mounted error", err)
	}
}

func TestProfileStatusInSync(t *testing.T) {
	if !(ProfileStatus{CheckedOut: true, HasBaseline: true}).InSync() {
		t.Error("no targets should be in sync")
	}
	if !(ProfileStatus{Targets: []TargetStatus{{}}}).InSync() {
		t.Error("an empty target should be in sync")
	}
	if (ProfileStatus{Targets: []TargetStatus{{Push: []Change{{Path: "x"}}}}}).InSync() {
		t.Error("a pending push means not in sync")
	}
	if (ProfileStatus{Targets: []TargetStatus{{LocalDeletes: []string{"y"}}}}).InSync() {
		t.Error("a pending local delete means not in sync")
	}
}

func TestTargetStatusLabel(t *testing.T) {
	if got := (TargetStatus{}).Label(); got != "(root)" {
		t.Errorf("Label() = %q, want (root)", got)
	}
	if got := (TargetStatus{Subpath: "albums/live"}).Label(); got != "albums/live" {
		t.Errorf("Label() = %q, want albums/live", got)
	}
}

// TestComputeGroupsBySubpath: a change is reported under the subpath (target) it
// falls within, and only there.
func TestComputeGroupsBySubpath(t *testing.T) {
	name, p, local, remote := fixture(t)
	p.Subpaths = []string{"albums", "stems"}
	markCheckedOut(t, remote)
	baselineFromLocal(t, name, local) // empty baseline
	// A new local file under albums/ only.
	writeFile(t, local, "albums/track.flac", "audio")

	st, err := Compute(context.Background(), name, p, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Targets) != 2 {
		t.Fatalf("want two targets (albums, stems), got %#v", st.Targets)
	}
	albums, stems := st.Targets[0], st.Targets[1]
	// The albums dir itself is a tracked entry, pushed alongside the file.
	if albums.Subpath != "albums" || len(albums.Push) != 2 ||
		albums.Push[0].Path != "albums" || albums.Push[1].Path != "albums/track.flac" {
		t.Errorf("albums target = %#v, want pushes of albums and albums/track.flac", albums)
	}
	if stems.Subpath != "stems" || !stems.InSync() {
		t.Errorf("stems target = %#v, want in sync", stems)
	}
}

// Status previews what sync will do, so it must scope by the same recorded
// checkout relpaths sync uses — not by the config's subpaths. A checkout held
// for docs/ only must not report an out-of-scope root file as a pending pull
// (sync would never pull it).
func TestComputeScopesByCheckoutRelpaths(t *testing.T) {
	name, p, _, remote := fixture(t)
	markCheckedOut(t, remote)
	writeFile(t, remote, "docs/d.txt", "D")
	writeFile(t, remote, "outside.txt", "X")
	// Checkout state scoped to docs only, empty baseline (fresh checkout).
	if err := baseline.Save(&baseline.State{Profile: name, Relpaths: []string{"docs"}, Files: threewayrsync.Manifest{}}); err != nil {
		t.Fatal(err)
	}

	st, err := Compute(context.Background(), name, p, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Targets) != 1 {
		t.Fatalf("want one (root) target, got %#v", st.Targets)
	}
	pulls := st.Targets[0].Pull
	// The docs dir is a tracked entry; outside.txt stays out of the checkout's scope.
	if len(pulls) != 2 || pulls[0].Path != "docs" || pulls[1].Path != "docs/d.txt" {
		t.Errorf("Pull = %#v, want docs and docs/d.txt only", pulls)
	}
}

func TestComputeInvalidSubpath(t *testing.T) {
	name, p, _, remote := fixture(t)
	p.Subpaths = []string{"../escape"}
	markCheckedOut(t, remote)
	baselineFromLocal(t, name, p.LocalRoot)
	if _, err := Compute(context.Background(), name, p, ""); err == nil {
		t.Fatal("want an error for a subpath escaping the root")
	}
}

// A released baseline must not feed the status diff: with a marker present but
// only a released state on disk, status reports "no baseline" — same as a
// checkout held on another machine.
func TestComputeTreatsReleasedBaselineAsAbsent(t *testing.T) {
	name, p, local, remote := fixture(t)
	writeFile(t, local, "file.txt", "data")
	writeFile(t, remote, "file.txt", "data")
	markCheckedOut(t, remote)
	baselineFromLocal(t, name, local)
	// Stamp the state released, as a checkin without --clean leaves it.
	st0, ok, err := baseline.Load(name)
	if err != nil || !ok {
		t.Fatalf("load fixture state: ok=%v err=%v", ok, err)
	}
	st0.ReleasedAt = time.Unix(5000, 0).UTC()
	if err := baseline.Save(st0); err != nil {
		t.Fatal(err)
	}

	st, err := Compute(context.Background(), name, p, "")
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if !st.CheckedOut || st.HasBaseline {
		t.Errorf("status = %+v, want CheckedOut=true HasBaseline=false over a released baseline", st)
	}
}
