// Package baseline stores the per-profile checkout state: the base ("last-synced")
// manifest that powers threewayrsync's three-way merge, plus the relpaths
// covered and the last sync time. It lives in a local state file, never on the remote,
// and exposes a threewayrsync.Store so the sync engine loads and commits the base itself.
package baseline

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andresbott/dibs/libs/threewayrsync"
)

// State is a profile's checkout state. Files is the base manifest — size and mtime per
// path, rsync's own quick-check fingerprint. Older state files written by the previous
// engine carry a content hash per file and nanosecond mtimes; the hash is ignored on load
// and mtimes are truncated to rsync's one-second resolution (see Load).
type State struct {
	Profile  string   `json:"profile"`
	Relpaths []string `json:"relpaths"`
	// LocalRoot and RemoteRoot record the resolved roots the baseline was taken
	// against, binding the state file to the profile as configured at checkout.
	// A profile whose roots were edited (or recreated under the same name) must
	// not merge against this manifest. Empty in state files written before this
	// field existed; callers treat empty as unbound (no check possible).
	LocalRoot  string                 `json:"local_root,omitempty"`
	RemoteRoot string                 `json:"remote_root,omitempty"`
	Files      threewayrsync.Manifest `json:"files"`
	LastSyncAt time.Time              `json:"last_sync_at"`
}

// Scope translates the recorded relpaths into a threewayrsync scope: a
// whole-root entry ("." or "", however recorded) anywhere means the whole tree
// (nil scope); otherwise the cleaned, slash-separated relpaths. Both sync and
// status derive their engine scope through here, so they can never diverge.
func (s *State) Scope() []string {
	var scope []string
	for _, rp := range s.Relpaths {
		rp = strings.TrimSpace(rp)
		if rp == "" || rp == "." {
			return nil
		}
		scope = append(scope, filepath.ToSlash(filepath.Clean(rp)))
	}
	return scope
}

// Dir returns the state directory: $DIBS_STATE, else
// $XDG_STATE_HOME/dibs, else ~/.local/state/dibs.
func Dir() (string, error) {
	if p := os.Getenv("DIBS_STATE"); p != "" {
		return p, nil
	}
	if p := os.Getenv("XDG_STATE_HOME"); p != "" {
		return filepath.Join(p, "dibs"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "dibs"), nil
}

func statePath(profile string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, profile+".json"), nil
}

// Load reads the state for profile. A missing file is (nil, false, nil). Every manifest
// mtime is truncated to whole seconds: rsync's listings carry one-second mtimes, and a
// nanosecond-precision entry (from a pre-threewayrsync state file) would otherwise make an
// untouched file look changed against the base on both sides — a false conflict.
func Load(profile string) (*State, bool, error) {
	path, err := statePath(profile)
	if err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: state file path is derived from the app's own state dir (Dir()) plus the profile name, not attacker-controlled input.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, false, err
	}
	for p, fs := range s.Files {
		fs.ModTime = fs.ModTime.Truncate(time.Second)
		s.Files[p] = fs
	}
	return &s, true, nil
}

// Save writes s to the state dir atomically (temp file + rename).
func Save(s *State) error {
	path, err := statePath(s.Profile)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".dibs-state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Remove deletes the state for profile. A missing file is not an error.
func Remove(profile string) error {
	path, err := statePath(profile)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Best-effort cleanup of the sync lock file living next to the state.
	_ = os.Remove(path + ".lock")
	return nil
}

// ProfileStore adapts a profile's state file to threewayrsync.Store: the engine loads the
// base manifest at the start of a sync and commits the merged base at the end, while the
// envelope (Relpaths) is preserved and LastSyncAt is stamped on every save. It also
// implements threewayrsync.Locker via a flock next to the state file, so two concurrent
// syncs of the same profile cannot interleave.
type ProfileStore struct {
	Profile string
	// Now stamps LastSyncAt on SaveBase; nil means time.Now().UTC.
	Now func() time.Time
}

// Store returns the threewayrsync store for a profile.
func Store(profile string) *ProfileStore { return &ProfileStore{Profile: profile} }

// LoadBase implements threewayrsync.Store: (nil, false, nil) when no state exists.
func (ps *ProfileStore) LoadBase() (threewayrsync.Manifest, bool, error) {
	s, ok, err := Load(ps.Profile)
	if err != nil || !ok {
		return nil, false, err
	}
	return s.Files, true, nil
}

// SaveBase implements threewayrsync.Store: the merged base replaces Files, the rest of the
// envelope is preserved (or freshly created when a sync runs without a prior checkout
// state — the lifecycle layer guards against that, so it is defensive here).
func (ps *ProfileStore) SaveBase(m threewayrsync.Manifest) error {
	s, ok, err := Load(ps.Profile)
	if err != nil {
		return err
	}
	if !ok {
		s = &State{Profile: ps.Profile}
	}
	s.Files = m
	s.LastSyncAt = ps.now()
	return Save(s)
}

// TryLock implements threewayrsync.Locker by delegating to the flock-based lock
// threewayrsync.FileStore provides, placed next to the profile's state file.
func (ps *ProfileStore) TryLock() (func(), error) {
	path, err := statePath(ps.Profile)
	if err != nil {
		return nil, err
	}
	return threewayrsync.FileStore{Path: path}.TryLock()
}

func (ps *ProfileStore) now() time.Time {
	if ps.Now != nil {
		return ps.Now()
	}
	return time.Now().UTC()
}
