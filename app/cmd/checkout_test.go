package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andresbott/netcheckout/internal/config"
	"github.com/andresbott/netcheckout/internal/lifecycle"
	"github.com/andresbott/netcheckout/internal/marker"
)

func TestCheckoutCommandWritesMarker(t *testing.T) {
	t.Setenv("NETCHECKOUT_STATE", t.TempDir())
	root := t.TempDir()
	local := filepath.Join(root, "local")
	remote := filepath.Join(root, "remote")
	_ = os.MkdirAll(remote, 0o755)
	_ = os.WriteFile(filepath.Join(remote, "file.txt"), []byte("x"), 0o644)

	cfgPath := writeStatusTestConfig(t, map[string]config.Profile{
		"work": {LocalRoot: local, RemoteRoot: remote},
	})
	runner := lifecycle.Runner{ToolVersion: "test"}
	cmd := newCheckoutCmdWithRunner(&cfgPath, runner)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"work"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := marker.Read(remote); !ok {
		t.Fatal("expected a marker after checkout")
	}
}

func TestCheckoutCommandRefusesNonEmptyLocal(t *testing.T) {
	t.Setenv("NETCHECKOUT_STATE", t.TempDir())
	root := t.TempDir()
	local := filepath.Join(root, "local")
	remote := filepath.Join(root, "remote")
	_ = os.MkdirAll(remote, 0o755)
	_ = os.WriteFile(filepath.Join(remote, "file.txt"), []byte("x"), 0o644)
	// Pre-existing local content must block the checkout.
	_ = os.MkdirAll(local, 0o755)
	_ = os.WriteFile(filepath.Join(local, "keep.dat"), []byte("mine"), 0o644)

	cfgPath := writeStatusTestConfig(t, map[string]config.Profile{
		"work": {LocalRoot: local, RemoteRoot: remote},
	})
	runner := lifecycle.Runner{ToolVersion: "test"}
	cmd := newCheckoutCmdWithRunner(&cfgPath, runner)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	// --force must not bypass the guard.
	cmd.SetArgs([]string{"work", "--force"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected checkout to refuse a non-empty local target")
	}
	if !strings.Contains(err.Error(), "not empty") {
		t.Errorf("want a 'not empty' error, got: %v", err)
	}
	if _, ok, _ := marker.Read(remote); ok {
		t.Error("refused checkout must not write a marker")
	}
}

func TestCheckoutRegisteredOnRoot(t *testing.T) {
	for _, c := range newRootCommand().Commands() {
		if c.Name() == "checkout" {
			return
		}
	}
	t.Fatal("checkout command not registered on root")
}

func TestCheckoutCommandPrintsLockedMessage(t *testing.T) {
	t.Setenv("NETCHECKOUT_STATE", t.TempDir())
	root := t.TempDir()
	local := filepath.Join(root, "local")
	remote := filepath.Join(root, "remote")
	_ = os.MkdirAll(remote, 0o755)
	_ = os.WriteFile(filepath.Join(remote, "file.txt"), []byte("x"), 0o644)

	cfgPath := writeStatusTestConfig(t, map[string]config.Profile{
		"work": {LocalRoot: local, RemoteRoot: remote},
	})
	runner := lifecycle.Runner{ToolVersion: "test"}
	cmd := newCheckoutCmdWithRunner(&cfgPath, runner)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"work"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// Checkout only locks now: it reports the lock and points at sync, and copies
	// no per-file changes.
	if !strings.Contains(out, "checked out") || !strings.Contains(out, "sync") {
		t.Errorf("want a 'checked out … run sync' summary, got:\n%s", out)
	}
	if entries, err := os.ReadDir(local); err == nil && len(entries) != 0 {
		t.Errorf("checkout must not populate the local dir, got %d entries", len(entries))
	}
}

func TestCheckoutDryRunPrintsNoChangeLines(t *testing.T) {
	t.Setenv("NETCHECKOUT_STATE", t.TempDir())
	root := t.TempDir()
	local := filepath.Join(root, "local")
	remote := filepath.Join(root, "remote")
	_ = os.MkdirAll(remote, 0o755)
	_ = os.WriteFile(filepath.Join(remote, "file.txt"), []byte("x"), 0o644)

	cfgPath := writeStatusTestConfig(t, map[string]config.Profile{
		"work": {LocalRoot: local, RemoteRoot: remote},
	})
	runner := lifecycle.Runner{ToolVersion: "test"}
	cmd := newCheckoutCmdWithRunner(&cfgPath, runner)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"work", "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "→") {
		t.Errorf("dry run must not stream applied changes, got:\n%s", buf.String())
	}
}
