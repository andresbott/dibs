//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScenarioA(t *testing.T) {
	forEachRemoteFlavor(t, func(t *testing.T, f remoteFixture) {
		randomTree(t, f.dir)
		remoteSnapshot := snapshot(t, f.dir)
		configPath := writeConfig(t, "e2e-test@localhost", "e2e", f.local, f.root)
		state := t.TempDir()
		env := []string{"NETCHECKOUT_STATE=" + state}

		if !t.Run("status reports no checkout before checkout", func(t *testing.T) {
			if got := snapshot(t, f.local); len(got) != 0 {
				t.Fatalf("local should start empty, got %d files: %#v", len(got), got)
			}
			stdout, _, exitCode := runCLIEnv(t, configPath, env, "status", "e2e")
			if exitCode != 0 {
				t.Fatalf("status exit = %d, want 0 (stdout: %s)", exitCode, stdout)
			}
			if !strings.Contains(stdout, "not checked out") {
				t.Fatalf("status stdout = %q, want it to report no checkout", stdout)
			}
		}) {
			t.FailNow()
		}

		if !t.Run("checkin refuses a profile that is not checked out", func(t *testing.T) {
			stdout, _, exitCode := runCLIEnv(t, configPath, env, "checkin", "e2e")
			if exitCode == 0 {
				t.Fatal("checkin of a not-checked-out profile should fail, got exit 0")
			}
			if !strings.Contains(stdout, "not checked out") {
				t.Fatalf("checkin output = %q, want it to report the profile is not checked out", stdout)
			}
		}) {
			t.FailNow()
		}

		if !t.Run("checkout locks the profile without copying files", func(t *testing.T) {
			_, _, exitCode := runCLIEnv(t, configPath, env, "checkout", "e2e")
			if exitCode != 0 {
				t.Fatalf("checkout exit = %d, want 0", exitCode)
			}
			// Checkout only writes the marker; the local working copy stays empty
			// (pulling the tree down is sync's job).
			if got := snapshot(t, f.local); len(got) != 0 {
				t.Fatalf("checkout must not copy files; local has %d: %#v", len(got), got)
			}
			if _, err := os.Stat(markerPath(f.dir)); err != nil {
				t.Fatalf("expected checkout marker at %s: %v", markerPath(f.dir), err)
			}
		}) {
			t.FailNow()
		}

		if !t.Run("checkout refuses a second checkout while held", func(t *testing.T) {
			stdout, _, exitCode := runCLIEnv(t, configPath, env, "checkout", "e2e")
			if exitCode == 0 {
				t.Fatal("a second checkout of a held profile should fail, got exit 0")
			}
			if !strings.Contains(stdout, "already checked out") {
				t.Fatalf("checkout output = %q, want it to report the profile is already checked out", stdout)
			}
		}) {
			t.FailNow()
		}

		if !t.Run("checkin refuses while the first pull is still pending", func(t *testing.T) {
			// The remote tree is not local yet, so the profile is not in sync: checkin
			// must refuse and point at sync, leaving the marker in place.
			stdout, _, exitCode := runCLIEnv(t, configPath, env, "checkin", "e2e")
			if exitCode == 0 {
				t.Fatal("checkin before the first sync should fail, got exit 0")
			}
			if !strings.Contains(stdout, "sync") {
				t.Fatalf("checkin output = %q, want it to point at sync", stdout)
			}
			if _, err := os.Stat(markerPath(f.dir)); err != nil {
				t.Fatalf("a refused checkin must keep the marker: %v", err)
			}
		}) {
			t.FailNow()
		}

		if !t.Run("sync pulls the remote down to local", func(t *testing.T) {
			_, _, exitCode := runCLIEnv(t, configPath, env, "sync", "e2e")
			if exitCode != 0 {
				t.Fatalf("sync exit = %d, want 0", exitCode)
			}
			assertSnapshotsEqual(t, remoteSnapshot, snapshot(t, f.local))
			if _, err := os.Stat(markerPath(f.dir)); err != nil {
				t.Fatalf("expected marker to remain after sync at %s: %v", markerPath(f.dir), err)
			}
		}) {
			t.FailNow()
		}

		var editedSnapshot map[string][]byte
		if !t.Run("editing the local copy", func(t *testing.T) {
			before := snapshot(t, f.local)
			var existing string
			for rel := range before {
				existing = rel
				break
			}
			writeRandomFile(t, filepath.Join(f.local, existing))
			writeRandomFile(t, filepath.Join(f.local, "e2e-added.dat"))
			editedSnapshot = snapshot(t, f.local)
		}) {
			t.FailNow()
		}

		if !t.Run("status reports local changes after editing", func(t *testing.T) {
			stdout, _, exitCode := runCLIEnv(t, configPath, env, "status", "e2e")
			if exitCode != 0 {
				t.Fatalf("status exit = %d, want 0 (stdout: %s)", exitCode, stdout)
			}
			if !strings.Contains(stdout, "e2e-added.dat") {
				t.Fatalf("status stdout = %q, want it to report the locally added file", stdout)
			}
		}) {
			t.FailNow()
		}

		if !t.Run("sync propagates local changes to remote and keeps the marker", func(t *testing.T) {
			_, _, exitCode := runCLIEnv(t, configPath, env, "sync", "e2e")
			if exitCode != 0 {
				t.Fatalf("sync exit = %d, want 0", exitCode)
			}
			assertSnapshotsEqual(t, editedSnapshot, snapshot(t, f.dir))
			if _, err := os.Stat(markerPath(f.dir)); err != nil {
				t.Fatalf("expected marker to remain after sync at %s: %v", markerPath(f.dir), err)
			}
		}) {
			t.FailNow()
		}

		t.Run("checkin verifies in sync and releases the marker", func(t *testing.T) {
			_, _, exitCode := runCLIEnv(t, configPath, env, "checkin", "e2e")
			if exitCode != 0 {
				t.Fatalf("checkin exit = %d, want 0", exitCode)
			}
			// checkin moves no data — the prior sync already made local and remote match.
			assertSnapshotsEqual(t, editedSnapshot, snapshot(t, f.dir))
			if _, err := os.Stat(markerPath(f.dir)); !os.IsNotExist(err) {
				t.Fatalf("expected marker to be removed after checkin, stat err = %v", err)
			}
		})
	})
}
