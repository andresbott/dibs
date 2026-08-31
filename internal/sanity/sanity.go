// Package sanity computes lightweight, stat-only state for a profile: whether
// its roots exist, whether it is checked out, and whether its declared subpaths
// exist on the remote. It performs no rsync and no content comparison -- that is
// internal/status. UnlistedLocal additionally performs a bounded local-only
// directory walk.
package sanity

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/internal/marker"
)

// Result is the lightweight state of a profile.
type Result struct {
	// ConfigErr is set when the profile could not be resolved (unknown server,
	// or the old embedded rsync shape). When non-empty the other fields are
	// zero: the profile is misconfigured, so no stat-based check was run.
	ConfigErr string

	LocalRoot  bool // local_root exists on disk
	RemoteRoot bool // remote_root exists and is a directory (mounted)
	CheckedOut bool // a marker is present at the remote root
	// Marker is the checkout marker when CheckedOut, so callers can tell an
	// own-machine lock from a foreign one. nil when not checked out — and also
	// when a marker file exists but cannot be parsed (CheckedOut stays true:
	// an unreadable lock is treated as foreign, the safe direction).
	Marker        *marker.Marker
	Subpaths      []Subpath // one per declared subpath, in config order; empty for whole-root profiles
	UnlistedLocal []string  // local paths (relative to local_root) holding content outside all declared subpaths
}

// Subpath is the existence of one declared subpath under the remote root.
type Subpath struct {
	Path   string
	Exists bool
}

// Check runs the lightweight checks for a profile. It returns no error: a
// missing path or unreachable remote is data (a false field), not a failure.
// A mounted-path remote is stat-only; a URL remote (ssh:// or rsync://) probes
// reachability and checkout state with a single marker fetch over rsync, and
// omits the per-subpath existence rows (no cheap remote stat exists). rsyncBin
// overrides the rsync binary for that fetch ("" means "rsync" from PATH).
func Check(p config.Profile, rsyncBin string) Result {
	var r Result
	if _, err := os.Stat(config.ExpandRoot(p.LocalRoot)); err == nil {
		r.LocalRoot = true
	}

	if p.RemoteIsLocalPath() {
		checkMountedRemote(p, &r)
	} else if e, err := p.RemoteEndpoint(); err == nil {
		if m, found, err := marker.ForEndpoint(e, rsyncBin).Read(context.Background()); err == nil {
			// The fetch reached the endpoint (found or cleanly absent).
			r.RemoteRoot = true
			r.CheckedOut = found
			r.Marker = m
		}
	}

	// Best-effort: a walk error is swallowed (Check reports data, not failures).
	if unlisted, err := UnlistedLocal(p); err == nil {
		r.UnlistedLocal = unlisted
	}
	return r
}

// checkMountedRemote fills the remote-side fields for a mounted-path remote:
// root presence, marker presence, and per-subpath existence — all stat-only.
func checkMountedRemote(p config.Profile, r *Result) {
	remoteRoot := config.ExpandRoot(p.RemoteRoot)
	if info, err := os.Stat(remoteRoot); err == nil && info.IsDir() {
		r.RemoteRoot = true
	}
	// The marker is per-profile at the remote root. Presence is
	// stat-based so a corrupt marker still reads as checked out (with a nil
	// Marker — treated as a foreign lock, the safe direction).
	if _, err := os.Stat(marker.Path(remoteRoot)); err == nil {
		r.CheckedOut = true
		if m, found, err := marker.Read(remoteRoot); err == nil && found {
			r.Marker = m
		}
	}
	targets, err := p.Targets()
	if err != nil {
		return
	}
	for _, t := range targets {
		if t.Subpath != "" {
			_, err := os.Stat(t.Remote)
			r.Subpaths = append(r.Subpaths, Subpath{Path: t.Subpath, Exists: err == nil})
		}
	}
}

// UnlistedLocal walks local_root and returns the shallowest paths (relative to
// local_root, slash-separated) that hold at least one regular file but fall outside
// every declared subpath. Loose regular files directly under local_root are reported
// individually. It returns nil when the profile declares no subpaths (whole root => all
// content is in scope) or when local_root does not exist. Symlinks are not regular files
// and never, by themselves, make a directory "contain files". Entries matching the
// profile's ignore patterns are invisible to the walk — sync never touches them, so
// they can't strand work (a .DS_Store dropped by Finder must not wedge every sync).
func UnlistedLocal(p config.Profile) ([]string, error) {
	if len(p.Subpaths) == 0 {
		return nil, nil
	}
	return UnlistedOutside(p, p.Subpaths)
}

// UnlistedOutside is UnlistedLocal against an arbitrary cover set instead of the
// declared subpaths: it flags local content outside every entry of cover. A "."
// (or empty) entry covers the whole root, so nothing can be flagged. Lifecycle
// uses this with a checkout's recorded relpaths, which may be narrower than the
// declared subpaths — content invisible to that envelope would be skipped by
// every scoped sync and by checkin's in-sync verification.
func UnlistedOutside(p config.Profile, cover []string) ([]string, error) {
	subs := make([]string, 0, len(cover))
	for _, s := range cover {
		if strings.TrimSpace(s) == "" || s == "." {
			return nil, nil // whole root covered
		}
		if err := config.ValidateSubpath(s); err != nil {
			return nil, fmt.Errorf("subpath %q: %w", s, err)
		}
		subs = append(subs, filepath.ToSlash(filepath.Clean(s)))
	}
	localRoot := config.ExpandRoot(p.LocalRoot)
	if info, err := os.Stat(localRoot); err != nil || !info.IsDir() {
		return nil, nil
	}
	var flagged []string
	if err := walkUnlisted(localRoot, ".", subs, p.Ignore, &flagged); err != nil {
		return nil, err
	}
	sort.Strings(flagged)
	if len(flagged) == 0 {
		return nil, nil
	}
	return flagged, nil
}

// walkUnlisted recurses under localRoot at relative rel, appending uncovered entries.
// Entries whose name matches an ignore pattern are skipped entirely.
func walkUnlisted(localRoot, rel string, subs, ignore []string, out *[]string) error {
	dir := localRoot
	if rel != "." {
		dir = filepath.Join(localRoot, filepath.FromSlash(rel))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if config.MatchesIgnoreName(e.Name(), ignore) {
			continue
		}
		childRel := e.Name()
		if rel != "." {
			childRel = rel + "/" + e.Name()
		}
		if e.IsDir() {
			switch {
			case isCovered(childRel, subs):
				// fully in scope; skip.
			case isAncestorOfSubpath(childRel, subs):
				if err := walkUnlisted(localRoot, childRel, subs, ignore, out); err != nil {
					return err
				}
			default:
				hasFile, err := containsRegularFile(filepath.Join(localRoot, filepath.FromSlash(childRel)), ignore)
				if err != nil {
					return err
				}
				if hasFile {
					*out = append(*out, childRel)
				}
			}
			continue
		}
		// regular file (skip symlinks and other non-regular entries)
		if e.Type().IsRegular() && !isCovered(childRel, subs) {
			*out = append(*out, childRel)
		}
	}
	return nil
}

// isCovered reports whether rel equals or lives under any declared subpath.
func isCovered(rel string, subs []string) bool {
	for _, s := range subs {
		if rel == s || strings.HasPrefix(rel, s+"/") {
			return true
		}
	}
	return false
}

// isAncestorOfSubpath reports whether any declared subpath lives under rel.
func isAncestorOfSubpath(rel string, subs []string) bool {
	for _, s := range subs {
		if strings.HasPrefix(s, rel+"/") {
			return true
		}
	}
	return false
}

// containsRegularFile reports whether dir holds at least one regular file at any
// depth, not counting ignored names: a directory holding nothing but .DS_Store
// files does not "contain files" as far as the sync is concerned.
func containsRegularFile(dir string, ignore []string) (bool, error) {
	found := false
	err := filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if config.MatchesIgnoreName(d.Name(), ignore) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found, err
}
