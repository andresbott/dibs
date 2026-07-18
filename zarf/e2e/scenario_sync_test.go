//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncPullsRemoteAdd(t *testing.T) {
	forEachRemoteFlavor(t, func(t *testing.T, f remoteFixture) {
		writeRandomFile(t, filepath.Join(f.dir, "seed.dat"))
		cfg := writeConfig(t, "e2e@localhost", "e2e", f.local, f.root)
		state := t.TempDir()
		env := []string{"NETCHECKOUT_STATE=" + state}

		if _, _, code := runCLIEnv(t, cfg, env, "checkout", "e2e"); code != 0 {
			t.Fatalf("checkout exit %d", code)
		}
		// Add a brand-new file on the remote only.
		writeRandomFile(t, filepath.Join(f.dir, "remote-add.dat"))
		if _, _, code := runCLIEnv(t, cfg, env, "sync", "e2e"); code != 0 {
			t.Fatalf("sync exit %d", code)
		}
		if _, err := os.Stat(filepath.Join(f.local, "remote-add.dat")); err != nil {
			t.Errorf("remote-only add should be pulled down: %v", err)
		}
	})
}

func TestSyncDisambiguatesDeleteVsAdd(t *testing.T) {
	forEachRemoteFlavor(t, func(t *testing.T, f remoteFixture) {
		writeRandomFile(t, filepath.Join(f.dir, "from-checkout.dat"))
		cfg := writeConfig(t, "e2e@localhost", "e2e", f.local, f.root)
		state := t.TempDir()
		env := []string{"NETCHECKOUT_STATE=" + state}

		if _, _, code := runCLIEnv(t, cfg, env, "checkout", "e2e"); code != 0 {
			t.Fatalf("checkout exit %d", code)
		}
		// Pull the remote down so from-checkout.dat exists locally and is recorded in
		// the baseline (checkout copies nothing on its own).
		if _, _, code := runCLIEnv(t, cfg, env, "sync", "e2e"); code != 0 {
			t.Fatalf("initial sync exit %d", code)
		}
		// (a) delete a checked-out file locally.
		if err := os.Remove(filepath.Join(f.local, "from-checkout.dat")); err != nil {
			t.Fatal(err)
		}
		// (b) add a brand-new file on the remote.
		writeRandomFile(t, filepath.Join(f.dir, "brand-new.dat"))

		// A plain sync pulls the add but leaves the delete pending on the remote.
		if _, _, code := runCLIEnv(t, cfg, env, "sync", "e2e"); code != 0 {
			t.Fatalf("sync exit %d", code)
		}
		if _, err := os.Stat(filepath.Join(f.local, "brand-new.dat")); err != nil {
			t.Errorf("remote add should be pulled locally: %v", err)
		}
		if _, err := os.Stat(filepath.Join(f.dir, "from-checkout.dat")); err != nil {
			t.Errorf("plain sync must not delete on the remote: %v", err)
		}
		if _, err := os.Stat(filepath.Join(f.local, "from-checkout.dat")); !os.IsNotExist(err) {
			t.Error("plain sync must not resurrect the locally deleted file")
		}
		// --allow-deletes propagates the delete (brand-new.dat keeps the remote
		// non-empty, so this is not a wipe).
		if _, _, code := runCLIEnv(t, cfg, env, "sync", "e2e", "--allow-deletes"); code != 0 {
			t.Fatalf("sync --allow-deletes exit %d", code)
		}
		if _, err := os.Stat(filepath.Join(f.dir, "from-checkout.dat")); !os.IsNotExist(err) {
			t.Error("local delete should propagate to the remote with --allow-deletes")
		}
	})
}

func TestSyncMirrorsRemoteDeleteLocally(t *testing.T) {
	forEachRemoteFlavor(t, func(t *testing.T, f remoteFixture) {
		writeRandomFile(t, filepath.Join(f.dir, "seed.dat"))
		writeRandomFile(t, filepath.Join(f.dir, "keep.dat")) // keeps the delete below a full wipe
		cfg := writeConfig(t, "e2e@localhost", "e2e", f.local, f.root)
		state := t.TempDir()
		env := []string{"NETCHECKOUT_STATE=" + state}

		if _, _, code := runCLIEnv(t, cfg, env, "checkout", "e2e"); code != 0 {
			t.Fatalf("checkout exit %d", code)
		}
		if _, _, code := runCLIEnv(t, cfg, env, "sync", "e2e"); code != 0 {
			t.Fatalf("first sync exit %d", code)
		}

		// Delete one file on the remote only. A plain sync must NOT touch the local
		// copy (the mirror-delete stays pending) and must NOT resurrect the file by
		// pushing the local copy back; --allow-deletes then mirrors the delete.
		if err := os.Remove(filepath.Join(f.dir, "seed.dat")); err != nil {
			t.Fatal(err)
		}
		stdout, _, code := runCLIEnv(t, cfg, env, "sync", "e2e")
		if code != 0 {
			t.Fatalf("plain sync exit %d:\n%s", code, stdout)
		}
		if _, err := os.Stat(filepath.Join(f.local, "seed.dat")); err != nil {
			t.Fatalf("plain sync must leave the local copy intact: %v", err)
		}
		if _, err := os.Stat(filepath.Join(f.dir, "seed.dat")); !os.IsNotExist(err) {
			t.Fatal("plain sync must not resurrect the file on the remote")
		}
		if _, _, code := runCLIEnv(t, cfg, env, "sync", "e2e", "--allow-deletes"); code != 0 {
			t.Fatalf("sync --allow-deletes exit %d", code)
		}
		if _, err := os.Stat(filepath.Join(f.local, "seed.dat")); !os.IsNotExist(err) {
			t.Error("remote delete must be mirrored locally with --allow-deletes")
		}
	})
}

// Deleting a whole folder locally must remove the folder on the remote too, not
// just the files inside it — and a brand-new empty folder must propagate.
func TestSyncPropagatesFolderDeleteAndCreate(t *testing.T) {
	forEachRemoteFlavor(t, func(t *testing.T, f remoteFixture) {
		writeRandomFile(t, filepath.Join(f.dir, "keep.dat")) // keeps the delete below a full wipe
		writeRandomFile(t, filepath.Join(f.dir, "artwork", "cover.png"))
		cfg := writeConfig(t, "e2e@localhost", "e2e", f.local, f.root)
		state := t.TempDir()
		env := []string{"NETCHECKOUT_STATE=" + state}

		if _, _, code := runCLIEnv(t, cfg, env, "checkout", "e2e"); code != 0 {
			t.Fatalf("checkout exit %d", code)
		}
		if _, _, code := runCLIEnv(t, cfg, env, "sync", "e2e"); code != 0 {
			t.Fatalf("first sync exit %d", code)
		}
		// Delete the folder locally; a plain sync leaves it pending on the remote.
		if err := os.RemoveAll(filepath.Join(f.local, "artwork")); err != nil {
			t.Fatal(err)
		}
		if _, _, code := runCLIEnv(t, cfg, env, "sync", "e2e"); code != 0 {
			t.Fatalf("plain sync exit %d", code)
		}
		if _, err := os.Stat(filepath.Join(f.dir, "artwork", "cover.png")); err != nil {
			t.Fatalf("plain sync must not delete on the remote: %v", err)
		}
		// --allow-deletes removes the files AND the now-empty folder itself.
		if _, _, code := runCLIEnv(t, cfg, env, "sync", "e2e", "--allow-deletes"); code != 0 {
			t.Fatalf("sync --allow-deletes exit %d", code)
		}
		if _, err := os.Stat(filepath.Join(f.dir, "artwork")); !os.IsNotExist(err) {
			t.Errorf("the emptied folder must be gone on the remote, stat err = %v", err)
		}
		// A brand-new empty folder propagates on the next sync.
		if err := os.MkdirAll(filepath.Join(f.local, "new-empty"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, _, code := runCLIEnv(t, cfg, env, "sync", "e2e"); code != 0 {
			t.Fatalf("sync exit %d", code)
		}
		if st, err := os.Stat(filepath.Join(f.dir, "new-empty")); err != nil || !st.IsDir() {
			t.Errorf("a brand-new empty folder must propagate: %v", err)
		}
	})
}

// Deleting EVERY synced file on one side is a full wipe of the other: a plain
// sync leaves it pending, and --allow-deletes refuses it — abandon is the only
// path through.
func TestSyncFullWipeRequiresAbandon(t *testing.T) {
	forEachRemoteFlavor(t, func(t *testing.T, f remoteFixture) {
		writeRandomFile(t, filepath.Join(f.dir, "seed.dat"))
		cfg := writeConfig(t, "e2e@localhost", "e2e", f.local, f.root)
		state := t.TempDir()
		env := []string{"NETCHECKOUT_STATE=" + state}

		if _, _, code := runCLIEnv(t, cfg, env, "checkout", "e2e"); code != 0 {
			t.Fatalf("checkout exit %d", code)
		}
		if _, _, code := runCLIEnv(t, cfg, env, "sync", "e2e"); code != 0 {
			t.Fatalf("first sync exit %d", code)
		}
		if err := os.Remove(filepath.Join(f.dir, "seed.dat")); err != nil {
			t.Fatal(err)
		}
		// Plain sync: pending, local intact.
		if _, _, code := runCLIEnv(t, cfg, env, "sync", "e2e"); code != 0 {
			t.Fatalf("plain sync exit %d", code)
		}
		if _, err := os.Stat(filepath.Join(f.local, "seed.dat")); err != nil {
			t.Fatalf("local copy must stay intact: %v", err)
		}
		// --allow-deletes: the engine refuses the full local wipe.
		stdout, stderr, code := runCLIEnv(t, cfg, env, "sync", "e2e", "--allow-deletes")
		if code == 0 {
			t.Fatalf("a full-wipe sync must refuse even with --allow-deletes:\n%s", stdout)
		}
		if !strings.Contains(stdout+stderr, "--abandon") {
			t.Errorf("refusal must name the abandon recovery, got:\n%s%s", stdout, stderr)
		}
		if _, err := os.Stat(filepath.Join(f.local, "seed.dat")); err != nil {
			t.Errorf("refused sync must leave the local copy intact: %v", err)
		}
	})
}

// TestStatusPreviewsRemoteDeleteAsLocalDelete pins status/sync parity: after a
// clean baseline, a file deleted on the remote only must be previewed by `status`
// as the local deletion that `sync` will actually perform (mirror the remote
// delete), NOT as a push that would resurrect the file on the remote. status must
// be a true three-way dry-run of sync, not a raw two-way rsync diff.
func TestStatusPreviewsRemoteDeleteAsLocalDelete(t *testing.T) {
	forEachRemoteFlavor(t, func(t *testing.T, f remoteFixture) {
		writeRandomFile(t, filepath.Join(f.dir, "seed.dat"))
		cfg := writeConfig(t, "e2e@localhost", "e2e", f.local, f.root)
		state := t.TempDir()
		env := []string{"NETCHECKOUT_STATE=" + state}

		if _, _, code := runCLIEnv(t, cfg, env, "checkout", "e2e"); code != 0 {
			t.Fatalf("checkout exit %d", code)
		}
		if _, _, code := runCLIEnv(t, cfg, env, "sync", "e2e"); code != 0 {
			t.Fatalf("first sync exit %d", code)
		}
		// Delete the file on the remote only.
		if err := os.Remove(filepath.Join(f.dir, "seed.dat")); err != nil {
			t.Fatal(err)
		}

		stdout, _, code := runCLIEnv(t, cfg, env, "status", "e2e")
		if code != 0 {
			t.Fatalf("status exit %d: %s", code, stdout)
		}
		if !strings.Contains(stdout, "del-local") {
			t.Errorf("status must preview the remote delete as a local deletion (del-local); got:\n%s", stdout)
		}
		if !strings.Contains(stdout, "seed.dat") {
			t.Errorf("status must mention the affected path seed.dat; got:\n%s", stdout)
		}
		// The file must never be presented as a push (local -> remote add): that is
		// the exact misclassification this guards against.
		for _, line := range strings.Split(stdout, "\n") {
			if strings.Contains(line, "seed.dat") && strings.Contains(line, "remote") && strings.Contains(line, "add") {
				t.Errorf("status must not present the remote-deleted file as an add-to-remote push; got line: %q", line)
			}
		}
	})
}

func TestSyncConflictStopsWithoutWriting(t *testing.T) {
	forEachRemoteFlavor(t, func(t *testing.T, f remoteFixture) {
		writeRandomFile(t, filepath.Join(f.dir, "F.dat"))
		cfg := writeConfig(t, "e2e@localhost", "e2e", f.local, f.root)
		state := t.TempDir()
		env := []string{"NETCHECKOUT_STATE=" + state}

		if _, _, code := runCLIEnv(t, cfg, env, "checkout", "e2e"); code != 0 {
			t.Fatalf("checkout exit %d", code)
		}
		// Edit F on BOTH sides with distinct content.
		if err := os.WriteFile(filepath.Join(f.local, "F.dat"), []byte("LOCAL-EDIT"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(f.dir, "F.dat"), []byte("REMOTE-EDIT"), 0o644); err != nil {
			t.Fatal(err)
		}
		remoteBefore := snapshot(t, f.dir)

		stdout, _, code := runCLIEnv(t, cfg, env, "sync", "e2e")
		if code == 0 {
			t.Fatalf("sync should exit non-zero on a conflict; stdout: %s", stdout)
		}
		// Remote byte-for-byte unchanged.
		assertSnapshotsEqual(t, remoteBefore, snapshot(t, f.dir))
		if _, err := os.Stat(markerPath(f.dir)); err != nil {
			t.Errorf("marker must be untouched on conflict: %v", err)
		}
	})
}

func TestSyncFailsFastWithoutLock(t *testing.T) {
	forEachRemoteFlavor(t, func(t *testing.T, f remoteFixture) {
		writeRandomFile(t, filepath.Join(f.dir, "x.dat"))
		cfg := writeConfig(t, "e2e@localhost", "e2e", f.local, f.root)
		state := t.TempDir()
		env := []string{"NETCHECKOUT_STATE=" + state}
		remoteBefore := snapshot(t, f.dir)

		// No marker at all.
		if _, _, code := runCLIEnv(t, cfg, env, "sync", "e2e"); code == 0 {
			t.Fatal("sync must fail fast with no marker")
		}
		assertSnapshotsEqual(t, remoteBefore, snapshot(t, f.dir))

		// A foreign marker.
		if err := os.WriteFile(markerPath(f.dir), []byte(`{"checked_out_by":"alice@nas","host":"nas","profile":"e2e"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, code := runCLIEnv(t, cfg, env, "sync", "e2e"); code == 0 {
			t.Fatal("sync must fail fast against a foreign marker")
		}
		assertSnapshotsEqual(t, remoteBefore, snapshot(t, f.dir))
	})
}

func TestSyncDryRunMutatesNothing(t *testing.T) {
	forEachRemoteFlavor(t, func(t *testing.T, f remoteFixture) {
		writeRandomFile(t, filepath.Join(f.dir, "d.dat"))
		cfg := writeConfig(t, "e2e@localhost", "e2e", f.local, f.root)
		state := t.TempDir()
		env := []string{"NETCHECKOUT_STATE=" + state}

		if _, _, code := runCLIEnv(t, cfg, env, "checkout", "e2e"); code != 0 {
			t.Fatalf("checkout exit %d", code)
		}
		writeRandomFile(t, filepath.Join(f.local, "local-only.dat"))
		remoteBefore := snapshot(t, f.dir)

		if _, _, code := runCLIEnv(t, cfg, env, "sync", "e2e", "--dry-run"); code != 0 {
			t.Fatalf("dry-run sync exit %d", code)
		}
		assertSnapshotsEqual(t, remoteBefore, snapshot(t, f.dir))
	})
}
