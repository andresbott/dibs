package threewayrsync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func requireRsync(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not on PATH")
	}
}

// List must enumerate an endpoint exactly as a sync listing does: regular files
// and directories with size+mtime, scope honored, ignored segments partitioned
// out, and a missing local dir treated as an empty tree.
func TestListEnumeratesEndpoint(t *testing.T) {
	requireRsync(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("AAAA"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("BB"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New(FileStore{Path: filepath.Join(t.TempDir(), "base.json")})
	m, err := s.List(context.Background(), Endpoint{Path: dir}, Options{Ignore: []string{".DS_Store"}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if st, ok := m["a.txt"]; !ok || st.IsDir || st.Size != 4 {
		t.Errorf("a.txt = %+v ok=%v, want a 4-byte file entry", st, ok)
	}
	if st, ok := m["sub/b.txt"]; !ok || st.Size != 2 {
		t.Errorf("sub/b.txt = %+v ok=%v, want a 2-byte file entry", st, ok)
	}
	if st, ok := m["sub"]; !ok || !st.IsDir {
		t.Errorf("sub = %+v ok=%v, want a directory entry", st, ok)
	}
	if _, ok := m[".DS_Store"]; ok {
		t.Error("ignored entries must be partitioned out of the listing")
	}
}

func TestListMissingLocalDirIsEmpty(t *testing.T) {
	requireRsync(t)
	s := New(FileStore{Path: filepath.Join(t.TempDir(), "base.json")})
	m, err := s.List(context.Background(), Endpoint{Path: filepath.Join(t.TempDir(), "nope")}, Options{})
	if err != nil {
		t.Fatalf("List over a missing dir must not error, got %v", err)
	}
	if len(m) != 0 {
		t.Errorf("want an empty manifest, got %d entries", len(m))
	}
}

func TestListScopesTheListing(t *testing.T) {
	requireRsync(t)
	dir := t.TempDir()
	for _, d := range []string{"in", "out"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, d, "f.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := New(FileStore{Path: filepath.Join(t.TempDir(), "base.json")})
	m, err := s.List(context.Background(), Endpoint{Path: dir}, Options{Scope: []string{"in"}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if _, ok := m["in/f.txt"]; !ok {
		t.Error("in-scope file missing from the listing")
	}
	if _, ok := m["out/f.txt"]; ok {
		t.Error("out-of-scope file must not be listed")
	}
}
