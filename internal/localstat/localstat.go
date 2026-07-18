// Package localstat computes a lightweight summary of a profile's local tree:
// how many folders and regular files it holds and their total size on disk. It
// walks only the local targets (never the remote) and, like internal/baseline,
// excludes the marker file and counts regular files only.
package localstat

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/andresbott/netcheckout/internal/config"
	"github.com/andresbott/netcheckout/internal/marker"
)

// Stats is the summary of a profile's local tree.
type Stats struct {
	Dirs  int   // directories under the target(s), excluding each target root itself
	Files int   // regular files (symlinks/others skipped, marker excluded)
	Bytes int64 // sum of regular-file sizes
}

// Scan walks each local target of the profile counting directories, regular
// files, and total bytes. It honors subpaths (via p.Targets()); a missing local
// subtree is skipped rather than erroring. The marker file is excluded and only
// regular files are counted, mirroring baseline.Scan's rules.
func Scan(p config.Profile) (Stats, error) {
	targets, err := p.Targets()
	if err != nil {
		return Stats{}, err
	}
	var s Stats
	for _, t := range targets {
		base := t.Local
		if _, err := os.Stat(base); errors.Is(err, os.ErrNotExist) {
			continue
		}
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// Count nested folders only; the target root itself is not a "folder
				// inside" the tree.
				if path != base {
					s.Dirs++
				}
				return nil
			}
			if d.Name() == marker.FileName || !d.Type().IsRegular() {
				return nil // skip the marker and any symlink/non-regular entry
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			s.Files++
			s.Bytes += info.Size()
			return nil
		})
		if err != nil {
			return Stats{}, err
		}
	}
	return s, nil
}
