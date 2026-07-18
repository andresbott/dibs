package threewayrsync

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrCorruptState marks a base manifest that exists but cannot be decoded. Callers can
// errors.Is on it to offer recovery (delete the state file and re-sync; the converged
// rule makes the re-sync cheap) instead of being wedged.
var ErrCorruptState = errors.New("base state is corrupt")

// ErrLocked is returned by TryLock when another process holds the sync lock.
var ErrLocked = errors.New("another sync is in progress")

// Store persists the base ("last-synced") manifest between syncs. Implementations decide
// where it lives; the library loads once at the start of a sync and saves once at the end.
// LoadBase returns (nil, false, nil) when there is no prior sync.
type Store interface {
	LoadBase() (Manifest, bool, error)
	SaveBase(Manifest) error
}

// Locker is an optional Store capability: when implemented, Sync holds the lock for its
// whole load→apply→save critical section, so two concurrent syncs over the same state
// cannot interleave (the loser's base write would silently undo the winner's deletions).
type Locker interface {
	// TryLock acquires the lock without blocking, returning the release func, or
	// ErrLocked when the lock is held elsewhere.
	TryLock() (func(), error)
}

// FileStore persists the base manifest to a single JSON file, written atomically, and
// provides a flock-based cross-process sync lock next to it.
type FileStore struct {
	Path string
}

// LoadBase reads the manifest. A missing file is (nil, false, nil); an undecodable file
// is an error wrapping ErrCorruptState.
func (f FileStore) LoadBase() (Manifest, bool, error) {
	data, err := os.ReadFile(f.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, false, fmt.Errorf("%w: %s: %v", ErrCorruptState, f.Path, err)
	}
	return m, true, nil
}

// SaveBase writes the manifest atomically and durably (temp file + fsync + rename),
// creating the parent dir. Without the fsync a crash right after the rename can leave an
// empty file — a corrupt base — on journaled filesystems.
func (f FileStore) SaveBase(m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(f.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".threewayrsync-state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, f.Path); err != nil {
		return err
	}
	// Best-effort fsync of the directory so the rename itself is durable.
	if d, err := os.Open(dir); err == nil { //nolint:gosec // G304: dir is derived from the caller-configured store path, not untrusted input.
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// TryLock takes a non-blocking flock on "<Path>.lock". flock (unlike a create-exclusive
// lock file) releases automatically when the process dies, so a crash never leaves a
// stale lock behind.
func (f FileStore) TryLock() (func(), error) {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return nil, err
	}
	lf, err := os.OpenFile(f.Path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lf.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
		_ = lf.Close()
	}, nil
}
