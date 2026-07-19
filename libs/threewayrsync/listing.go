package threewayrsync

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Module is one entry of an rsync daemon's module listing: the module name and the
// optional comment configured next to it in rsyncd.conf.
type Module struct {
	Name    string
	Comment string
}

// ListModules connects to the daemon described by d and returns the modules it lists.
// d.Module is ignored (module listing addresses the daemon root); d.PasswordFile is
// unused too — daemon auth is per-module, the listing itself is unauthenticated. Modules
// configured with "list = false" are hidden by the daemon and will not appear.
func (s *Syncer) ListModules(ctx context.Context, d Daemon) ([]Module, error) {
	d.Module = ""
	if err := validateDaemonFields(&d, false); err != nil {
		return nil, err
	}
	args := buildModuleListArgs(d)
	res, err := s.runner()(ctx, s.bin(), args, nil)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &Error{Op: "list-modules", Args: args, Stderr: res.stderr, ExitCode: res.exitCode, Err: err}
	}
	return parseModuleList(res.stdout), nil
}

// ListDirs returns the names of the directories directly under path — a slash-relative
// path inside d.Module, "" for the module root — sorted. Files and the "." self entry
// are skipped: the listing feeds a directory browser, which only descends.
func (s *Syncer) ListDirs(ctx context.Context, d Daemon, path string) ([]string, error) {
	if err := validateDaemonFields(&d, true); err != nil {
		return nil, err
	}
	if !safeRelOrEmpty(path) {
		return nil, fmt.Errorf("list-dirs: unsafe path %q", path)
	}
	args := buildDirListArgs(d, path)
	res, err := s.runner()(ctx, s.bin(), args, nil)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &Error{Op: "list-dirs", Args: args, Stderr: res.stderr, ExitCode: res.exitCode, Err: err}
	}
	return parseDirList(res.stdout)
}

// safeRelOrEmpty is safeRelPath extended to accept "" (the module root).
func safeRelOrEmpty(p string) bool { return p == "" || safeRelPath(p) }

// isModeString reports whether s looks like a 10-character ls-style mode string
// ("drwxr-xr-x"): every permission position is one of the characters ls uses there.
// This is what separates a real --list-only entry from chatter starting with "d".
func isModeString(s string) bool {
	for _, c := range s[1:] {
		if !strings.ContainsRune("rwxsStT-", c) {
			return false
		}
	}
	return true
}

// parseModuleList parses the daemon's module listing ("rsync rsync://host/"): rsync
// prints one "%-15s\t%s" line per module — the name space-padded to 15 columns, a tab,
// then the comment. Lines without a tab, or whose pre-tab token is not a single
// whitespace-free word (MOTD chatter, blanks), are skipped.
func parseModuleList(out string) []Module {
	var mods []Module
	for _, line := range strings.Split(out, "\n") {
		name, comment, found := strings.Cut(line, "\t")
		if !found {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, " \t") {
			continue
		}
		mods = append(mods, Module{Name: name, Comment: strings.TrimSpace(comment)})
	}
	return mods
}

// parseDirList parses "rsync --list-only" output into the directory names it lists,
// sorted. Each entry line is "modestring size date time name" — the name starts after
// the fourth space-delimited field and may itself contain spaces. Only "d" entries are
// kept; the "." self entry, files, symlinks, and non-entry chatter are skipped. A
// directory-shaped line that cannot be split into five fields is an error: silently
// dropping it would make an existing folder unbrowsable with no signal.
func parseDirList(out string) ([]string, error) {
	var dirs []string
	for _, line := range strings.Split(out, "\n") {
		if line == "" || line[0] != 'd' {
			continue // files ("-"), links ("l"), and chatter — only dirs browse
		}
		if len(line) < 11 || !isModeString(line[:10]) {
			continue // "d" first byte but not a mode string: chatter
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return nil, fmt.Errorf("malformed --list-only line %q", line)
		}
		// The name is everything after the "date time" pair; locate the pair
		// positionally so names containing runs of spaces survive strings.Fields'
		// collapsing (a name cannot precede the timestamp, so the first match is it).
		stamp := fields[2] + " " + fields[3]
		idx := strings.Index(line, stamp)
		if idx < 0 {
			return nil, fmt.Errorf("malformed --list-only line %q", line)
		}
		name := strings.TrimPrefix(line[idx+len(stamp):], " ")
		if name == "" {
			return nil, fmt.Errorf("malformed --list-only line %q", line)
		}
		if name == "." {
			continue // the listed directory itself
		}
		dirs = append(dirs, name)
	}
	sort.Strings(dirs)
	return dirs, nil
}
