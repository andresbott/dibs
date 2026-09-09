package cmd

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/candy-tools/dibs/internal/config"
	"github.com/candy-tools/dibs/internal/lifecycle"
	"github.com/candy-tools/dibs/internal/marker"
	"github.com/candy-tools/dibs/libs/threewayrsync"
	"github.com/spf13/cobra"
)

// capturingAccessor records the remote endpoint passed to NewAccessor during
// preflight checks, allowing tests to assert on the resolved remote root before
// any network calls are made.
type capturingAccessor struct {
	capturedRemoteRoot *string
}

func (a *capturingAccessor) Read(ctx context.Context) (*marker.Marker, bool, error) {
	// Marker doesn't exist - this is expected for profiles not checked out
	return nil, false, nil
}

func (a *capturingAccessor) Write(ctx context.Context, m *marker.Marker) error {
	return fmt.Errorf("test accessor: write not implemented")
}

func (a *capturingAccessor) Remove(ctx context.Context) error {
	return fmt.Errorf("test accessor: remove not implemented")
}

// endpointToRemoteRoot reconstructs the remote root URL from an endpoint, reversing
// what config.Profile.RemoteEndpoint() does. For rsync daemon endpoints, it builds
// "rsync://[user@]host[:port]/module[/path]". For SSH and local paths, it returns
// the path or reconstructed ssh:// URL.
func endpointToRemoteRoot(e threewayrsync.Endpoint) string {
	if e.Daemon != nil {
		host := e.Daemon.Host
		if e.Daemon.User != "" {
			host = e.Daemon.User + "@" + host
		}
		if e.Daemon.Port != 0 {
			host += ":" + strconv.Itoa(e.Daemon.Port)
		}
		modulePath := e.Daemon.Module
		if e.Path != "" {
			modulePath += "/" + e.Path
		}
		return "rsync://" + host + "/" + modulePath
	}
	if e.SSH != nil {
		host := e.SSH.Host
		if e.SSH.User != "" {
			host = e.SSH.User + "@" + host
		}
		if e.SSH.Port != 0 {
			host += ":" + strconv.Itoa(e.SSH.Port)
		}
		return "ssh://" + host + e.Path
	}
	return e.Path
}

// runWithCapturingSyncer runs a command with an injected capturing accessor and returns
// the remote root URL the command resolved. The accessor captures the endpoint during
// preflight checks, before any network calls.
func runWithCapturingSyncer(t *testing.T, cfgPath string, newCmd func(*string, lifecycle.Runner) *cobra.Command, args []string) string {
	t.Helper()
	var capturedRemoteRoot string
	r := lifecycle.Runner{
		ToolVersion: "test",
		NewAccessor: func(e threewayrsync.Endpoint) marker.Accessor {
			capturedRemoteRoot = endpointToRemoteRoot(e)
			return &capturingAccessor{capturedRemoteRoot: &capturedRemoteRoot}
		},
	}
	cmd := newCmd(&cfgPath, r)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	_ = cmd.Execute() // Will error (profile not checked out), but we only care about the captured root
	return capturedRemoteRoot
}

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

	got := runWithCapturingSyncer(t, path, newSyncCmdWithRunner, []string{"docs"})
	want := "rsync://bob@nas.local/share/docs"
	if got != want {
		t.Errorf("sync passed RemoteRoot %q to runner, want %q", got, want)
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

	got := runWithCapturingSyncer(t, path, newCheckoutCmdWithRunner, []string{"docs"})
	want := "rsync://bob@nas.local/share/docs"
	if got != want {
		t.Errorf("checkout passed RemoteRoot %q to runner, want %q", got, want)
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

	got := runWithCapturingSyncer(t, path, newCheckinCmdWithRunner, []string{"docs"})
	want := "rsync://bob@nas.local/share/docs"
	if got != want {
		t.Errorf("checkin passed RemoteRoot %q to runner, want %q", got, want)
	}
}
