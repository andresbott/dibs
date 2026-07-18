package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/internal/lifecycle"
	"github.com/andresbott/dibs/internal/marker"
)

func requireRsync(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not on PATH")
	}
}

// heldCmdFixture builds a checked-out, already-synced profile by driving the
// real checkout and sync commands, and returns the config path and remote root.
func heldCmdFixture(t *testing.T) (cfgPath, remote string) {
	t.Helper()
	requireRsync(t)
	t.Setenv("DIBS_STATE", t.TempDir())
	root := t.TempDir()
	local := filepath.Join(root, "local")
	remote = filepath.Join(root, "remote")
	_ = os.MkdirAll(remote, 0o755)
	_ = os.WriteFile(filepath.Join(remote, "keep.txt"), []byte("base"), 0o644)
	cfgPath = writeStatusTestConfig(t, map[string]config.Profile{"work": {LocalRoot: local, RemoteRoot: remote}})

	co := newCheckoutCmdWithRunner(&cfgPath, lifecycle.Runner{ToolVersion: "test"})
	co.SetOut(&bytes.Buffer{})
	co.SetArgs([]string{"work"})
	if err := co.Execute(); err != nil {
		t.Fatalf("fixture checkout: %v", err)
	}
	sc := newSyncCmdWithRunner(&cfgPath, lifecycle.Runner{ToolVersion: "test"})
	sc.SetOut(&bytes.Buffer{})
	sc.SetArgs([]string{"work"})
	if err := sc.Execute(); err != nil {
		t.Fatalf("fixture first sync: %v", err)
	}
	return cfgPath, remote
}

func TestSyncCommandPushesLocalEdit(t *testing.T) {
	cfgPath, remote := heldCmdFixture(t)
	// Edit locally (different size => detected by the quick-check).
	cfg, _ := config.Load(cfgPath)
	lroot := cfg.Profiles["work"].LocalRoot
	_ = os.WriteFile(filepath.Join(lroot, "keep.txt"), []byte("EDITED-LONGER"), 0o644)

	cmd := newSyncCmdWithRunner(&cfgPath, lifecycle.Runner{ToolVersion: "test"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"work"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(remote, "keep.txt")); string(got) != "EDITED-LONGER" {
		t.Errorf("remote keep.txt = %q, want EDITED-LONGER", got)
	}
	if _, ok, _ := marker.Read(remote); !ok {
		t.Error("sync must leave the marker in place")
	}
}

func TestSyncCommandPrintsProgressiveChanges(t *testing.T) {
	cfgPath, _ := heldCmdFixture(t)
	cfg, _ := config.Load(cfgPath)
	lroot := cfg.Profiles["work"].LocalRoot
	_ = os.WriteFile(filepath.Join(lroot, "keep.txt"), []byte("EDITED-LONGER"), 0o644)

	cmd := newSyncCmdWithRunner(&cfgPath, lifecycle.Runner{ToolVersion: "test"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"work"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "modify") || !strings.Contains(out, "remote") || !strings.Contains(out, "keep.txt") {
		t.Errorf("want a per-file change line for keep.txt, got:\n%s", out)
	}
	// The one-line summary must still follow the streamed changes.
	if !strings.Contains(out, "push 1") {
		t.Errorf("want the summary line after the changes, got:\n%s", out)
	}
}

func TestSyncDryRunPrintsNoChangeLines(t *testing.T) {
	cfgPath, _ := heldCmdFixture(t)
	cfg, _ := config.Load(cfgPath)
	lroot := cfg.Profiles["work"].LocalRoot
	_ = os.WriteFile(filepath.Join(lroot, "keep.txt"), []byte("EDITED-LONGER"), 0o644)

	cmd := newSyncCmdWithRunner(&cfgPath, lifecycle.Runner{ToolVersion: "test"})
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

func TestSyncCommandConflictListsPaths(t *testing.T) {
	cfgPath, remote := heldCmdFixture(t)
	cfg, _ := config.Load(cfgPath)
	lroot := cfg.Profiles["work"].LocalRoot
	_ = os.WriteFile(filepath.Join(lroot, "keep.txt"), []byte("local-version"), 0o644)
	_ = os.WriteFile(filepath.Join(remote, "keep.txt"), []byte("R"), 0o644)

	cmd := newSyncCmdWithRunner(&cfgPath, lifecycle.Runner{ToolVersion: "test"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"work"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("a conflict must exit non-zero")
	}
	if out := buf.String(); !strings.Contains(out, "conflict") || !strings.Contains(out, "keep.txt") {
		t.Errorf("want the conflicting path listed, got:\n%s", out)
	}
}

// A plain sync never deletes: the 12 locally removed files stay on the remote
// and the output reports them pending; --allow-deletes then propagates them.
func TestSyncCommandAllowDeletesFlag(t *testing.T) {
	cfgPath, remote := heldCmdFixture(t)
	cfg, _ := config.Load(cfgPath)
	lroot := cfg.Profiles["work"].LocalRoot
	for i := 0; i < 12; i++ {
		name := fmt.Sprintf("bulk%02d.txt", i)
		_ = os.WriteFile(filepath.Join(lroot, name), []byte("x"), 0o644)
	}
	sc := newSyncCmdWithRunner(&cfgPath, lifecycle.Runner{ToolVersion: "test"})
	sc.SetOut(&bytes.Buffer{})
	sc.SetArgs([]string{"work"})
	if err := sc.Execute(); err != nil {
		t.Fatalf("grow sync: %v", err)
	}
	for i := 0; i < 12; i++ {
		_ = os.Remove(filepath.Join(lroot, fmt.Sprintf("bulk%02d.txt", i)))
	}

	cmd := newSyncCmdWithRunner(&cfgPath, lifecycle.Runner{ToolVersion: "test"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"work"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plain sync must succeed with the deletes pending, got %v", err)
	}
	if !strings.Contains(out.String(), "pending") || !strings.Contains(out.String(), "--allow-deletes") {
		t.Errorf("output must report the pending deletes and name --allow-deletes, got:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(remote, "bulk00.txt")); err != nil {
		t.Error("plain sync must leave the remote files in place")
	}

	cmd = newSyncCmdWithRunner(&cfgPath, lifecycle.Runner{ToolVersion: "test"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"work", "--allow-deletes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("sync --allow-deletes: %v", err)
	}
	if _, err := os.Stat(filepath.Join(remote, "bulk00.txt")); !os.IsNotExist(err) {
		t.Error("remote bulk files must be deleted")
	}
}

func TestSyncRegisteredOnRoot(t *testing.T) {
	for _, c := range newRootCommand().Commands() {
		if c.Name() == "sync" {
			return
		}
	}
	t.Fatal("sync not registered")
}
