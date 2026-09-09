//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/candy-tools/dibs/internal/config"
)

// writeConfigProfile is writeConfig for a fully specified profile (subpaths,
// ignore patterns, ...), written via the real production writer.
func writeConfigProfile(t *testing.T, identity, name string, p config.Profile) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{
		Identity: identity,
		Profiles: map[string]config.Profile{name: p},
	}
	serverizeConfig(cfg)
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// The .DS_Store scenario this feature exists for: mounting a share drops
// metadata files onto the remote. With the profile ignoring them, sync must
// never pull, push, or delete them; status and sync report them under their
// own "ignored" category; and checkin succeeds with the file still in place.
func TestIgnoredFileIsNeverSyncedAndDoesNotBlockCheckin(t *testing.T) {
	forEachRemoteFlavor(t, func(t *testing.T, f remoteFixture) {
		writeRandomFile(t, filepath.Join(f.dir, "seed.dat"))
		cfg := writeConfigProfile(t, "e2e@localhost", "e2e", config.Profile{
			LocalRoot:  f.local,
			RemoteRoot: f.root,
			Ignore:     []string{".DS_Store", ".directory"},
		})
		state := t.TempDir()
		env := []string{"DIBS_STATE=" + state}

		if _, _, code := runCLIEnv(t, cfg, env, "checkout", "e2e"); code != 0 {
			t.Fatalf("checkout exit %d", code)
		}
		if _, _, code := runCLIEnv(t, cfg, env, "sync", "e2e"); code != 0 {
			t.Fatalf("initial sync exit %d", code)
		}

		// A file manager drops metadata on both sides after the first sync.
		if err := os.WriteFile(filepath.Join(f.dir, ".DS_Store"), []byte("finder"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(f.local, ".directory"), []byte("dolphin"), 0o644); err != nil {
			t.Fatal(err)
		}

		// Status: both files under "ignored", target otherwise in sync.
		stdout, _, code := runCLIEnv(t, cfg, env, "status", "e2e")
		if code != 0 {
			t.Fatalf("status exit %d:\n%s", code, stdout)
		}
		if !strings.Contains(stdout, "in sync") {
			t.Errorf("status should report in sync despite ignored files:\n%s", stdout)
		}
		for _, want := range []string{"ignored", ".DS_Store", ".directory"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("status missing %q:\n%s", want, stdout)
			}
		}

		// Sync (deletes allowed, to prove ignored paths dodge deletion too):
		// copies nothing, reports the ignored pair.
		stdout, _, code = runCLIEnv(t, cfg, env, "sync", "e2e", "--allow-deletes")
		if code != 0 {
			t.Fatalf("sync exit %d:\n%s", code, stdout)
		}
		if !strings.Contains(stdout, "pull 0, push 0, del-remote 0, del-local 0") {
			t.Errorf("sync must move nothing:\n%s", stdout)
		}
		if !strings.Contains(stdout, "ignored") {
			t.Errorf("sync report missing the ignored category:\n%s", stdout)
		}
		if _, err := os.Stat(filepath.Join(f.local, ".DS_Store")); !os.IsNotExist(err) {
			t.Error("the remote .DS_Store must not be pulled")
		}
		if _, err := os.Stat(filepath.Join(f.dir, ".directory")); !os.IsNotExist(err) {
			t.Error("the local .directory must not be pushed")
		}

		// Checkin: succeeds despite the one-sided ignored files, which stay put.
		if stdout, _, code := runCLIEnv(t, cfg, env, "checkin", "e2e"); code != 0 {
			t.Fatalf("checkin exit %d:\n%s", code, stdout)
		}
		if _, err := os.Stat(filepath.Join(f.dir, ".DS_Store")); err != nil {
			t.Errorf("checkin must leave the remote .DS_Store in place: %v", err)
		}
	})
}

// A local .DS_Store outside a scoped profile's declared subpaths used to trip
// the unlisted-local guard and block every sync; with the pattern ignored the
// scoped sync proceeds.
func TestIgnoredFileOutsideSubpathsDoesNotBlockScopedSync(t *testing.T) {
	forEachRemoteFlavor(t, func(t *testing.T, f remoteFixture) {
		writeRandomFile(t, filepath.Join(f.dir, "docs", "a.dat"))
		cfg := writeConfigProfile(t, "e2e@localhost", "e2e", config.Profile{
			LocalRoot:  f.local,
			RemoteRoot: f.root,
			Subpaths:   []string{"docs"},
			Ignore:     []string{".DS_Store"},
		})
		state := t.TempDir()
		env := []string{"DIBS_STATE=" + state}

		if _, _, code := runCLIEnv(t, cfg, env, "checkout", "e2e"); code != 0 {
			t.Fatalf("checkout exit %d", code)
		}
		if _, _, code := runCLIEnv(t, cfg, env, "sync", "e2e"); code != 0 {
			t.Fatalf("initial sync exit %d", code)
		}

		// Finder drops metadata at the local root — outside the declared subpath.
		if err := os.WriteFile(filepath.Join(f.local, ".DS_Store"), []byte("finder"), 0o644); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := runCLIEnv(t, cfg, env, "sync", "e2e")
		if code != 0 {
			t.Fatalf("sync must not be blocked by an ignored file outside the subpaths (exit %d):\n%s%s", code, stdout, stderr)
		}
	})
}
