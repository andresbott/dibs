package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/internal/lifecycle"
)

func TestSyncResolvesServerReference(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{
		Identity: "tester",
		Servers:  map[string]config.Server{"nas": {Host: "nas.local", User: "bob"}},
		Profiles: map[string]config.Profile{"docs": {ID: "x", Server: "nas", RemoteModule: "share/docs", LocalRoot: dir}},
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	// Verify config.ResolveProfile works as expected
	resolved, err := cfg.ResolveProfile("docs")
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	want := "rsync://bob@nas.local/share/docs"
	if resolved.RemoteRoot != want {
		t.Fatalf("ResolveProfile returned RemoteRoot %q, want %q", resolved.RemoteRoot, want)
	}

	cmd := newSyncCmdWithRunner(&path, lifecycle.Runner{ToolVersion: "test"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"docs"})

	err = cmd.Execute()
	// Command will fail (profile not checked out), but the error should NOT be about server resolution
	if err != nil && strings.Contains(err.Error(), "still references server") {
		t.Fatalf("command failed with unresolved server reference: %v", err)
	}
}

func TestSyncUnknownServerErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{
		Identity: "tester",
		Profiles: map[string]config.Profile{"docs": {Server: "ghost", RemoteModule: "m", LocalRoot: dir}},
	}
	_ = config.Save(path, cfg)

	cmd := newSyncCmdWithRunner(&path, lifecycle.Runner{ToolVersion: "test"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"docs"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown server, got nil")
	}
	if !strings.Contains(err.Error(), "unknown server") || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error = %q, want to mention unknown server \"ghost\"", err)
	}
}

func TestCheckoutResolvesServerReference(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{
		Identity: "tester",
		Servers:  map[string]config.Server{"nas": {Host: "nas.local", User: "bob"}},
		Profiles: map[string]config.Profile{"docs": {ID: "x", Server: "nas", RemoteModule: "share/docs", LocalRoot: dir}},
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	// Verify resolution works
	resolved, err := cfg.ResolveProfile("docs")
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if want := "rsync://bob@nas.local/share/docs"; resolved.RemoteRoot != want {
		t.Fatalf("ResolveProfile returned RemoteRoot %q, want %q", resolved.RemoteRoot, want)
	}

	cmd := newCheckoutCmdWithRunner(&path, lifecycle.Runner{ToolVersion: "test"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"docs"})

	err = cmd.Execute()
	if err != nil && strings.Contains(err.Error(), "still references server") {
		t.Fatalf("command failed with unresolved server reference: %v", err)
	}
}

func TestCheckinResolvesServerReference(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{
		Identity: "tester",
		Servers:  map[string]config.Server{"nas": {Host: "nas.local", User: "bob"}},
		Profiles: map[string]config.Profile{"docs": {ID: "x", Server: "nas", RemoteModule: "share/docs", LocalRoot: dir}},
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	// Verify resolution works
	resolved, err := cfg.ResolveProfile("docs")
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if want := "rsync://bob@nas.local/share/docs"; resolved.RemoteRoot != want {
		t.Fatalf("ResolveProfile returned RemoteRoot %q, want %q", resolved.RemoteRoot, want)
	}

	cmd := newCheckinCmdWithRunner(&path, lifecycle.Runner{ToolVersion: "test"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"docs"})

	err = cmd.Execute()
	if err != nil && strings.Contains(err.Error(), "still references server") {
		t.Fatalf("command failed with unresolved server reference: %v", err)
	}
}
