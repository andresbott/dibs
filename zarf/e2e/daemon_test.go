//go:build e2e

package e2e

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startRsyncDaemon launches a loopback rsync daemon exporting moduleDir as module "data"
// on a free port, and kills it on test cleanup. The module is writable and chroot-less so
// the suite can run unprivileged. The config, pid, and log files live in their own temp
// dir — never in moduleDir, which snapshots must see exactly as the scenario left it.
func startRsyncDaemon(t *testing.T, moduleDir string) int {
	t.Helper()
	lst, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := lst.Addr().(*net.TCPAddr).Port
	_ = lst.Close()

	dir := t.TempDir()
	conf := filepath.Join(dir, "rsyncd.conf")
	confData := fmt.Sprintf(
		"use chroot = false\npid file = %s/rsyncd.pid\nlog file = %s/rsyncd.log\n\n[data]\n  path = %s\n  read only = false\n",
		dir, dir, moduleDir)
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

	// Wait for the daemon to accept connections.
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

func TestStartRsyncDaemonServesModule(t *testing.T) {
	requireRsync(t)
	moduleDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(moduleDir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	port := startRsyncDaemon(t, moduleDir)

	out, err := exec.Command("rsync", fmt.Sprintf("rsync://127.0.0.1:%d/data/", port)).CombinedOutput()
	if err != nil {
		t.Fatalf("listing the module failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "hello.txt") {
		t.Fatalf("module listing should contain hello.txt, got:\n%s", out)
	}
}
