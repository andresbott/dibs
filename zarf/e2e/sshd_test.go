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

// findSSHD locates the sshd binary: PATH first, then the sbin locations that are
// usually not on PATH for a regular user.
func findSSHD() string {
	if p, err := exec.LookPath("sshd"); err == nil {
		return p
	}
	for _, p := range []string{"/usr/sbin/sshd", "/usr/local/sbin/sshd", "/opt/homebrew/sbin/sshd"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// requireSSHD skips the calling test when sshd, ssh, or ssh-keygen is unavailable
// (mirrors requireRsync), and returns the sshd path.
func requireSSHD(t *testing.T) string {
	t.Helper()
	sshd := findSSHD()
	if sshd == "" {
		t.Skip("sshd not found (PATH or the usual sbin locations)")
	}
	for _, bin := range []string{"ssh", "ssh-keygen"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH", bin)
		}
	}
	return sshd
}

// startLoopbackSSHD launches an unprivileged loopback sshd on a free port and kills it
// on test cleanup. It generates its own host key and a client keypair in a temp dir, so
// no root and no system ssh state are involved; a non-root sshd only accepts logins as
// the invoking user, which is exactly what a loopback test wants. StrictModes is off
// because temp-dir permissions never satisfy sshd's checks.
//
// It returns the value for RSYNC_RSH rather than port+key: the config under test keeps a
// bare "ssh://127.0.0.1/..." remote root (no port, no identity file), which makes
// threewayrsync emit no --rsh flag, and rsync then honors RSYNC_RSH — carrying the port,
// key, and the switches that keep the unknown loopback host key from prompting.
func startLoopbackSSHD(t *testing.T) (rsh string) {
	t.Helper()
	sshd := requireSSHD(t)

	lst, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := lst.Addr().(*net.TCPAddr).Port
	_ = lst.Close()

	dir := t.TempDir()
	hostKey := filepath.Join(dir, "host_key")
	userKey := filepath.Join(dir, "id_ed25519")
	for _, key := range []string{hostKey, userKey} {
		if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
			t.Fatalf("ssh-keygen %s: %v\n%s", key, err, out)
		}
	}
	pub, err := os.ReadFile(userKey + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "authorized_keys"), pub, 0o600); err != nil {
		t.Fatal(err)
	}

	conf := filepath.Join(dir, "sshd_config")
	logFile := filepath.Join(dir, "sshd.log")
	confData := fmt.Sprintf(`ListenAddress 127.0.0.1
Port %d
HostKey %s
AuthorizedKeysFile %s
PubkeyAuthentication yes
PasswordAuthentication no
KbdInteractiveAuthentication no
UsePAM no
StrictModes no
PidFile %s
`, port, hostKey, filepath.Join(dir, "authorized_keys"), filepath.Join(dir, "sshd.pid"))
	if err := os.WriteFile(conf, []byte(confData), 0o644); err != nil {
		t.Fatal(err)
	}

	// sshd requires an absolute path to itself (it re-executes per connection); -D keeps
	// it in the foreground so Cleanup's kill reaps the whole thing.
	cmd := exec.Command(sshd, "-D", "-f", conf, "-E", logFile)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	// Wait for sshd to accept connections.
	deadline := time.Now().Add(5 * time.Second)
	up := false
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			up = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !up {
		log, _ := os.ReadFile(logFile)
		t.Fatalf("sshd did not come up on port %d; log:\n%s", port, log)
	}

	// Space-free option forms only: rsync splits the rsh string on whitespace.
	return strings.Join([]string{
		"ssh",
		fmt.Sprintf("-p%d", port),
		"-i" + userKey,
		"-oStrictHostKeyChecking=no",
		"-oUserKnownHostsFile=/dev/null",
		"-oIdentitiesOnly=yes",
		"-oBatchMode=yes",
	}, " ")
}

func TestStartLoopbackSSHDServesFiles(t *testing.T) {
	requireRsync(t)
	remoteDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteDir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	rsh := startLoopbackSSHD(t)
	t.Setenv("RSYNC_RSH", rsh)

	out, err := exec.Command("rsync", "127.0.0.1:"+remoteDir+"/").CombinedOutput()
	if err != nil {
		t.Fatalf("listing over ssh failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "hello.txt") {
		t.Fatalf("ssh listing should contain hello.txt, got:\n%s", out)
	}

	dst := filepath.Join(t.TempDir(), "hello.txt")
	if out, err := exec.Command("rsync", "127.0.0.1:"+filepath.Join(remoteDir, "hello.txt"), dst).CombinedOutput(); err != nil {
		t.Fatalf("fetching over ssh failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hi" {
		t.Fatalf("fetched content = %q, want %q", data, "hi")
	}
}
