package lifecycle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/internal/ident"
	"github.com/andresbott/dibs/internal/marker"
)

func TestSyncRefusesUnlistedLocalContent(t *testing.T) {
	local, remote := t.TempDir(), t.TempDir()
	// A marker so the guard is reached only if it runs BEFORE the lock check would
	// otherwise pass; the guard must fire regardless.
	id := ident.Ident{By: "me@host", Host: "host"}
	m := &marker.Marker{CheckedOutBy: id.By, Host: id.Host, Profile: "p"}
	if err := marker.Write(remote, m); err != nil {
		t.Fatal(err)
	}
	// Unlisted local content: subpaths=[a] but a file lives at the root.
	if err := os.WriteFile(filepath.Join(local, "top.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := config.Profile{LocalRoot: local, RemoteRoot: remote, Subpaths: []string{"a"}}
	r := Runner{ToolVersion: "test"}

	_, err := r.Sync(context.Background(), "p", p, id, "", Options{})
	if err == nil || !strings.Contains(err.Error(), "top.txt") {
		t.Fatalf("expected error naming top.txt, got %v", err)
	}
}

func TestSyncForceDoesNotBypassGuard(t *testing.T) {
	local, remote := t.TempDir(), t.TempDir()
	// Same setup as TestSyncRefusesUnlistedLocalContent
	id := ident.Ident{By: "me@host", Host: "host"}
	m := &marker.Marker{CheckedOutBy: id.By, Host: id.Host, Profile: "p"}
	if err := marker.Write(remote, m); err != nil {
		t.Fatal(err)
	}
	// Unlisted local content: subpaths=[a] but a file lives at the root.
	if err := os.WriteFile(filepath.Join(local, "top.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := config.Profile{LocalRoot: local, RemoteRoot: remote, Subpaths: []string{"a"}}
	r := Runner{ToolVersion: "test"}

	// Even with Force: true, the guard must still refuse to proceed
	_, err := r.Sync(context.Background(), "p", p, id, "", Options{Force: true})
	if err == nil || !strings.Contains(err.Error(), "top.txt") {
		t.Fatalf("expected error naming top.txt even with --force, got %v", err)
	}
}

// A checkout scoped NARROWER than the declared subpaths must still protect
// local work under the other declared subpaths: it is inside the declaration
// (so the config-level guard passes) but outside the checkout envelope, so no
// scoped sync would ever push it and checkin would report in-sync over it.
func TestSyncRefusesLocalContentOutsideCheckoutEnvelope(t *testing.T) {
	t.Setenv("DIBS_STATE", t.TempDir())
	local, remote := t.TempDir(), t.TempDir()
	for _, dir := range []string{filepath.Join(remote, "a"), filepath.Join(remote, "b")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	id := ident.Ident{By: "me@host", Host: "host"}
	p := config.Profile{LocalRoot: local, RemoteRoot: remote, Subpaths: []string{"a", "b"}}
	r := Runner{ToolVersion: "test"}
	// Envelope = [a] only (explicit relpath narrower than the declaration).
	if _, err := r.Checkout(context.Background(), "p", p, id, "a", Options{}); err != nil {
		t.Fatal(err)
	}
	// Local work under the DECLARED-but-not-checked-out subpath b.
	if err := os.MkdirAll(filepath.Join(local, "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "b", "work.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The walk reports the shallowest uncovered path: the directory b itself.
	if _, err := r.Sync(context.Background(), "p", p, id, "", Options{}); err == nil || !strings.Contains(err.Error(), "\n  b") {
		t.Fatalf("sync must refuse and name the out-of-envelope content, got %v", err)
	}
	if _, err := r.Checkin(context.Background(), "p", p, id, Options{}); err == nil || !strings.Contains(err.Error(), "\n  b") {
		t.Fatalf("checkin must refuse and name the out-of-envelope content, got %v", err)
	}
	// Widening the checkout to b clears the refusal... but b/work.txt is now a
	// local copy the vacancy guard protects, so the widen itself refuses too —
	// the user must move the content aside first. Verify the message says so.
	if _, err := r.Checkout(context.Background(), "p", p, id, "b", Options{}); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("widening onto existing local content must hit the vacancy guard, got %v", err)
	}
}

func TestCheckinRefusesUnlistedLocalContent(t *testing.T) {
	local, remote := t.TempDir(), t.TempDir()
	// Same setup as TestSyncRefusesUnlistedLocalContent
	id := ident.Ident{By: "me@host", Host: "host"}
	m := &marker.Marker{CheckedOutBy: id.By, Host: id.Host, Profile: "p"}
	if err := marker.Write(remote, m); err != nil {
		t.Fatal(err)
	}
	// Unlisted local content: subpaths=[a] but a file lives at the root.
	if err := os.WriteFile(filepath.Join(local, "top.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := config.Profile{LocalRoot: local, RemoteRoot: remote, Subpaths: []string{"a"}}
	r := Runner{ToolVersion: "test"}

	// Checkin must also refuse when there is unlisted local content
	_, err := r.Checkin(context.Background(), "p", p, id, Options{})
	if err == nil || !strings.Contains(err.Error(), "top.txt") {
		t.Fatalf("expected error naming top.txt, got %v", err)
	}
}
