//go:build e2e

package e2e

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestScenarioResume covers the keep-and-resume lifecycle: checkin without
// --clean leaves the local copy and a released baseline; both sides then drift
// — the remote gets an edit, an add, and a delete by "someone else", and a new
// file is added locally (allowed: an addition is unsynced work, not tampering);
// checkout --resume adopts the copy without transferring anything; one sync
// reconciles the drift in both directions (remote changes land locally, the
// local addition pushes); and a locally MODIFIED copy is refused by name. Runs
// on all three transports — the daemon leg exercises the pure-local validation
// against an rsync server remote.
func TestScenarioResume(t *testing.T) {
	forEachRemoteFlavor(t, func(t *testing.T, f remoteFixture) {
		randomTree(t, f.dir)
		configPath := writeConfig(t, "e2e-test@localhost", "e2e", f.local, f.root)
		state := t.TempDir()
		env := []string{"DIBS_STATE=" + state}

		if !t.Run("checkout, sync, checkin keeps the local copy", func(t *testing.T) {
			for _, args := range [][]string{{"checkout", "e2e"}, {"sync", "e2e"}, {"checkin", "e2e"}} {
				if _, stderr, exit := runCLIEnv(t, configPath, env, args...); exit != 0 {
					t.Fatalf("%v exit = %d (stderr: %s)", args, exit, stderr)
				}
			}
			if _, err := os.Stat(markerPath(f.dir)); !os.IsNotExist(err) {
				t.Fatalf("marker must be gone after checkin, stat err = %v", err)
			}
			if got := snapshot(t, f.local); len(got) == 0 {
				t.Fatal("checkin without --clean must keep the local copy")
			}
		}) {
			t.FailNow()
		}

		if !t.Run("plain re-checkout refuses and points at --resume", func(t *testing.T) {
			_, stderr, exit := runCLIEnv(t, configPath, env, "checkout", "e2e")
			if exit == 0 {
				t.Fatal("checkout over the kept copy must fail without --resume")
			}
			if !strings.Contains(stderr, "not empty") || !strings.Contains(stderr, "--resume") {
				t.Fatalf("stderr = %q, want the vacancy refusal with the --resume hint", stderr)
			}
		}) {
			t.FailNow()
		}

		// The remote drifts while released: an edit, an add, and a delete by
		// "someone else". The 600-byte fixed body guarantees a size change
		// against randomTree's 16-512 byte files, so the size+mtime quick-check
		// cannot miss it even when the mtime lands in the same second.
		var modified, deleted string
		if !t.Run("both sides drift while released", func(t *testing.T) {
			var rels []string
			for rel := range snapshot(t, f.local) {
				rels = append(rels, rel)
			}
			sort.Strings(rels)
			if len(rels) < 2 {
				t.Fatalf("need at least two files, have %d", len(rels))
			}
			modified, deleted = rels[0], rels[1]
			if err := os.WriteFile(filepath.Join(f.dir, modified), bytes.Repeat([]byte("R"), 600), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(f.dir, deleted)); err != nil {
				t.Fatal(err)
			}
			writeRandomFile(t, filepath.Join(f.dir, "server-added.dat"))
			// A file added LOCALLY while released is unsynced work, not
			// tampering: resume must allow it and the reconcile sync pushes it.
			writeRandomFile(t, filepath.Join(f.local, "local-added.dat"))
		}) {
			t.FailNow()
		}

		if !t.Run("checkout --resume adopts the copy without transferring", func(t *testing.T) {
			before := snapshot(t, f.local)
			stdout, stderr, exit := runCLIEnv(t, configPath, env, "checkout", "e2e", "--resume")
			if exit != 0 {
				t.Fatalf("checkout --resume exit = %d (stderr: %s)", exit, stderr)
			}
			if !strings.Contains(stdout, "resumed") {
				t.Fatalf("stdout = %q, want a 'resumed' summary", stdout)
			}
			if _, err := os.Stat(markerPath(f.dir)); err != nil {
				t.Fatalf("resume must write the marker: %v", err)
			}
			// Resume moves no data: remote drift stays remote until sync.
			assertSnapshotsEqual(t, before, snapshot(t, f.local))
		}) {
			t.FailNow()
		}

		if !t.Run("one sync reconciles the drift", func(t *testing.T) {
			// --allow-deletes lets the sync mirror the server-side deletion.
			if _, stderr, exit := runCLIEnv(t, configPath, env, "sync", "e2e", "--allow-deletes"); exit != 0 {
				t.Fatalf("sync exit = %d (stderr: %s)", exit, stderr)
			}
			local := snapshot(t, f.local)
			assertSnapshotsEqual(t, snapshot(t, f.dir), local)
			if _, ok := local[deleted]; ok {
				t.Errorf("%s was deleted remotely and must be mirrored locally", deleted)
			}
			if _, ok := local["server-added.dat"]; !ok {
				t.Error("the remotely added file must have pulled")
			}
			// The snapshot equality above already proves it, but say it plainly:
			// the file added locally while released survived the resume and
			// reached the remote as a push.
			if _, err := os.Stat(filepath.Join(f.dir, "local-added.dat")); err != nil {
				t.Errorf("the locally added file must have pushed to the remote: %v", err)
			}
		}) {
			t.FailNow()
		}

		if !t.Run("checkin releases again", func(t *testing.T) {
			if _, stderr, exit := runCLIEnv(t, configPath, env, "checkin", "e2e"); exit != 0 {
				t.Fatalf("checkin exit = %d (stderr: %s)", exit, stderr)
			}
		}) {
			t.FailNow()
		}

		t.Run("resume refuses a locally modified copy by name", func(t *testing.T) {
			// 700 fixed bytes: guaranteed size drift against the 600-byte remote edit.
			if err := os.WriteFile(filepath.Join(f.local, modified), bytes.Repeat([]byte("L"), 700), 0o644); err != nil {
				t.Fatal(err)
			}
			_, stderr, exit := runCLIEnv(t, configPath, env, "checkout", "e2e", "--resume")
			if exit == 0 {
				t.Fatal("resume over a tampered copy must fail")
			}
			if !strings.Contains(stderr, modified) {
				t.Fatalf("stderr = %q, want the tampered path %q named", stderr, modified)
			}
			if _, err := os.Stat(markerPath(f.dir)); !os.IsNotExist(err) {
				t.Fatalf("a refused resume must not write a marker, stat err = %v", err)
			}
		})
	})
}
