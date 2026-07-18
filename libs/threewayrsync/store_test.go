package threewayrsync

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// memStore is an in-memory Store for tests. Reused by sync_test.go.
type memStore struct {
	base  Manifest
	ok    bool
	saved int
}

func (m *memStore) LoadBase() (Manifest, bool, error) { return m.base, m.ok, nil }
func (m *memStore) SaveBase(b Manifest) error         { m.base = b; m.ok = true; m.saved++; return nil }

func TestFileStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "base.json")
	fs := FileStore{Path: path}

	if _, ok, err := fs.LoadBase(); ok || err != nil {
		t.Fatalf("missing file should be (nil,false,nil); ok=%v err=%v", ok, err)
	}

	want := Manifest{"a.txt": {Size: 3, ModTime: time.Unix(100, 0)}}
	if err := fs.SaveBase(want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := fs.LoadBase()
	if err != nil || !ok {
		t.Fatalf("LoadBase = ok %v err %v", ok, err)
	}
	if got["a.txt"].Size != 3 || !got["a.txt"].ModTime.Equal(time.Unix(100, 0)) {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestFileStoreCorruptStateIsTyped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "base.json")
	if err := os.WriteFile(path, []byte("{truncated"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := FileStore{Path: path}.LoadBase()
	if !errors.Is(err, ErrCorruptState) {
		t.Fatalf("want ErrCorruptState, got %v", err)
	}
}

func TestFileStoreTryLockExcludes(t *testing.T) {
	fs := FileStore{Path: filepath.Join(t.TempDir(), "base.json")}
	release, err := fs.TryLock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fs.TryLock(); !errors.Is(err, ErrLocked) {
		t.Fatalf("second TryLock must fail with ErrLocked, got %v", err)
	}
	release()
	release2, err := fs.TryLock()
	if err != nil {
		t.Fatalf("lock must be reacquirable after release: %v", err)
	}
	release2()
}
