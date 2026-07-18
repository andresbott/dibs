package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andresbott/netcheckout/internal/config"
)

func runInit(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(append([]string{"init"}, args...))
	err := root.Execute()
	return buf.String(), err
}

func TestInitCreatesConfigWithExplicitIdentity(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	out, err := runInit(t, "--config", p, "--identity", "alice@laptop")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, p) {
		t.Errorf("output should mention the created path:\n%s", out)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Identity != "alice@laptop" {
		t.Errorf("identity: got %q", cfg.Identity)
	}
	if len(cfg.Profiles) != 0 {
		t.Errorf("profiles should be empty, got %v", cfg.Profiles)
	}
}

func TestInitDefaultsIdentityToUserAtHostname(t *testing.T) {
	t.Setenv("USER", "bob")
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := runInit(t, "--config", p); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := "bob@" + host; cfg.Identity != want {
		t.Errorf("identity: got %q, want %q", cfg.Identity, want)
	}
}

func TestInitWhitespaceIdentityFallsBackToDefault(t *testing.T) {
	t.Setenv("USER", "bob")
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := runInit(t, "--config", p, "--identity", "   "); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := "bob@" + host; cfg.Identity != want {
		t.Errorf("identity: got %q, want %q", cfg.Identity, want)
	}
}

func TestInitRefusesWhenConfigExists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(p, &config.Config{Identity: "keep@me"}); err != nil {
		t.Fatal(err)
	}
	_, err := runInit(t, "--config", p, "--identity", "clobber@er")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("want 'already exists' error, got %v", err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Identity != "keep@me" {
		t.Errorf("existing config was modified: identity %q", cfg.Identity)
	}
}

func TestInitCreatesParentDirectories(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nested", "dir", "config.yaml")
	if _, err := runInit(t, "--config", p, "--identity", "alice@laptop"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("config not created: %v", err)
	}
}
