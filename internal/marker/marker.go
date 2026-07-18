// Package marker reads, writes, and removes the per-profile checkout marker: a
// small JSON lock file at the remote root (GOALS.md §5) recording who holds a
// profile, from which host, and which relpaths are pulled. It is the source of
// truth for the cooperative lock.
package marker

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// FileName is the marker's filename, placed at a profile's remote root.
const FileName = ".netcheckout.json"

// Exclude is the engine exclude list that keeps the marker out of every sync
// and diff: the cooperative lock is metadata, not content — it must never be
// pulled, pushed, or counted as a discrepancy. Shared by lifecycle and status
// so the two can't drift.
func Exclude() []string { return []string{"/" + FileName} }

// Marker is the on-disk lock record (GOALS.md §5).
type Marker struct {
	CheckedOutBy string    `json:"checked_out_by"`
	Profile      string    `json:"profile"`
	Host         string    `json:"host"`
	Relpaths     []string  `json:"relpaths"`
	CheckedOutAt time.Time `json:"checked_out_at"`
	LastSyncAt   time.Time `json:"last_sync_at"`
	ToolVersion  string    `json:"tool_version"`
}

// Path returns the marker location for a remote root.
func Path(remoteRoot string) string { return filepath.Join(remoteRoot, FileName) }

// Read loads the marker at remoteRoot. A missing marker is (nil, false, nil).
func Read(remoteRoot string) (*Marker, bool, error) {
	data, err := os.ReadFile(Path(remoteRoot))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var m Marker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, false, err
	}
	return &m, true, nil
}

// Write persists m at remoteRoot atomically (temp file + rename).
// The marker is written mode 0644 because it is a shared, cross-user lock file
// that must be readable by other users and machines for the cooperative lock to work.
func Write(remoteRoot string, m *Marker) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(remoteRoot, ".netcheckout-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil { //nolint:gosec // G302: the marker is a shared cross-user cooperative lock and must be world-readable (GOALS.md §5/§8), so 0644 is intentional, not overly permissive.
		return err
	}
	return os.Rename(tmpName, Path(remoteRoot))
}

// Remove deletes the marker at remoteRoot. A missing marker is not an error.
func Remove(remoteRoot string) error {
	err := os.Remove(Path(remoteRoot))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// OwnedBy reports whether this marker belongs to the given identity on the
// given host: both must match (GOALS.md §3/§10 — this-machine ownership).
func (m *Marker) OwnedBy(by, host string) bool {
	return m.CheckedOutBy == by && m.Host == host
}
