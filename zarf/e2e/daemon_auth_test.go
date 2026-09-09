//go:build e2e

package e2e

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/candy-tools/dibs/internal/config"
)

// startAuthRsyncDaemon launches a loopback rsync daemon exporting moduleDir as the
// auth-required module "secure": only user "alice" with password "s3cret" may use it.
func startAuthRsyncDaemon(t *testing.T, moduleDir string) int {
	t.Helper()
	lst, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := lst.Addr().(*net.TCPAddr).Port
	_ = lst.Close()

	dir := t.TempDir()
	secrets := filepath.Join(dir, "rsyncd.secrets")
	if err := os.WriteFile(secrets, []byte("alice:s3cret\n"), 0o600); err != nil {
		t.Fatal(err) // rsyncd refuses world-readable secrets files
	}
	conf := filepath.Join(dir, "rsyncd.conf")
	confData := fmt.Sprintf(
		"use chroot = false\npid file = %s/rsyncd.pid\nlog file = %s/rsyncd.log\n\n"+
			"[secure]\n  path = %s\n  read only = false\n  auth users = alice\n  secrets file = %s\n",
		dir, dir, moduleDir, secrets)
	if err := os.WriteFile(conf, []byte(confData), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("rsync", "--daemon", "--no-detach", "--config="+conf, "--port="+fmt.Sprint(port))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return port
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("rsync daemon did not come up on port %d", port)
	return 0
}

// TestDaemonAuthModule pins the credentials path the profile editor's rsync fields
// feed: a profile whose rsync:// remote points at an auth-required module works with
// rsyncd_password_file set, and fails cleanly without it.
func TestDaemonAuthModule(t *testing.T) {
	requireRsync(t)
	local, remote := newFixture(t)
	port := startAuthRsyncDaemon(t, remote)
	writeRandomFile(t, filepath.Join(remote, "hello.dat"))
	root := fmt.Sprintf("rsync://alice@127.0.0.1:%d/secure", port)
	state := t.TempDir()
	env := []string{"DIBS_STATE=" + state}

	pw := filepath.Join(t.TempDir(), "rsync.pass")
	if err := os.WriteFile(pw, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	writeAuthConfig := func(passwordFile string) string {
		path := filepath.Join(t.TempDir(), "config.yaml")
		cfg := &config.Config{
			Identity: "e2e-test@localhost",
			Profiles: map[string]config.Profile{
				"e2e": {LocalRoot: local, RemoteRoot: root, RsyncdPasswordFile: passwordFile},
			},
		}
		serverizeConfig(cfg)
		if err := config.Save(path, cfg); err != nil {
			t.Fatalf("write config: %v", err)
		}
		return path
	}

	if !t.Run("checkout without credentials fails", func(t *testing.T) {
		_, _, exitCode := runCLIEnv(t, writeAuthConfig(""), env, "checkout", "e2e")
		if exitCode == 0 {
			t.Fatal("checkout against an auth module without a password file should fail, got exit 0")
		}
	}) {
		t.FailNow()
	}

	configPath := writeAuthConfig(pw)
	if !t.Run("checkout and sync succeed with the password file", func(t *testing.T) {
		if _, stderr, exitCode := runCLIEnv(t, configPath, env, "checkout", "e2e"); exitCode != 0 {
			t.Fatalf("checkout exit = %d, want 0 (stderr: %s)", exitCode, stderr)
		}
		if _, stderr, exitCode := runCLIEnv(t, configPath, env, "sync", "e2e"); exitCode != 0 {
			t.Fatalf("sync exit = %d, want 0 (stderr: %s)", exitCode, stderr)
		}
		assertSnapshotsEqual(t, snapshot(t, remote), snapshot(t, local))
	}) {
		t.FailNow()
	}

	t.Run("checkin releases the lock", func(t *testing.T) {
		if _, stderr, exitCode := runCLIEnv(t, configPath, env, "checkin", "e2e"); exitCode != 0 {
			t.Fatalf("checkin exit = %d, want 0 (stderr: %s)", exitCode, stderr)
		}
		if _, err := os.Stat(markerPath(remote)); !os.IsNotExist(err) {
			t.Fatalf("marker should be gone after checkin, stat err = %v", err)
		}
	})
}
