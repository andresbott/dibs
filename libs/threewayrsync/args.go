package threewayrsync

import (
	"os"
	"strings"
)

// partialDir holds rsync's interrupted partial transfers; it is excluded from enumeration
// and transfer so leftover partials from a canceled run are never mistaken for real files.
const partialDir = ".rsync-partial"

// endpointArgs returns the transport flags an endpoint needs on the rsync command line:
// --rsh for non-default ssh settings, --password-file for daemon auth. Local endpoints
// contribute nothing.
func endpointArgs(e Endpoint) []string {
	switch {
	case e.SSH != nil:
		if rsh := sshRsh(e.SSH); rsh != "" {
			return []string{"--rsh=" + rsh}
		}
	case e.Daemon != nil:
		if strings.TrimSpace(e.Daemon.PasswordFile) != "" {
			return []string{"--password-file=" + e.Daemon.PasswordFile}
		}
	}
	return nil
}

// buildListArgs assembles the rsync argument list for enumerating src via a dry-run
// itemize against an empty destination. --out-format prints "%i %l %M %n" (itemize flags,
// size, mtime, path) per entry. emptyDest must be an existing empty directory. scope must
// already be normalized.
func buildListArgs(src Endpoint, emptyDest string, exclude, scope []string) []string {
	args := []string{"--recursive", "--links", "--dry-run", "--itemize-changes", "--out-format=%i %l %M %n", "--exclude=" + partialDir}
	for _, ex := range exclude {
		args = append(args, "--exclude="+ex)
	}
	args = append(args, scopeFilterArgs(scope)...)
	args = append(args, endpointArgs(src)...)
	args = append(args, withTrailingSlash(src.render()), withTrailingSlash(emptyDest))
	return args
}

// buildTransferArgs assembles the rsync argument list for a real transfer from src to dst.
// --partial lets a canceled transfer resume; --times equalizes mtime so a re-listing sees
// the two sides as equal. No --recursive: every transfer is driven by an explicit
// --files-from list, whose implied --dirs already creates listed directories (and the
// parents of listed files) — with --recursive a directory entry would recurse and drag
// along children the plan never approved. At most one of src/dst is remote (validated
// earlier). scope must already be normalized; the transferred paths come from scoped
// listings, so the filter chain is defense in depth against anything out of scope
// slipping into a transfer.
func buildTransferArgs(src, dst Endpoint, checksum bool, exclude, scope []string) []string {
	args := []string{"--links", "--times", "--itemize-changes", "--partial-dir=" + partialDir, "--exclude=" + partialDir}
	if checksum {
		args = append(args, "--checksum")
	}
	for _, ex := range exclude {
		args = append(args, "--exclude="+ex)
	}
	args = append(args, scopeFilterArgs(scope)...)
	args = append(args, endpointArgs(src)...)
	args = append(args, endpointArgs(dst)...)
	args = append(args, withTrailingSlash(src.render()), dst.render())
	return args
}

// withFilesFrom splices "--files-from=<path>" (with --from0: the list is NUL-separated,
// so filenames containing newlines cannot split into extra entries) in front of the two
// trailing positional paths so rsync parses them as options, not a source.
func withFilesFrom(args []string, listPath string) []string {
	n := len(args)
	out := make([]string, 0, n+2)
	out = append(out, args[:n-2]...)
	out = append(out, "--files-from="+listPath, "--from0")
	out = append(out, args[n-2:]...)
	return out
}

// writeFileList writes one NUL-terminated relative path per entry (rsync --from0 format)
// to a temp file and returns its name; the caller removes it.
func writeFileList(paths []string) (string, error) {
	f, err := os.CreateTemp("", "threewayrsync-files-*.txt")
	if err != nil {
		return "", err
	}
	name := f.Name()
	if _, err := f.WriteString(strings.Join(paths, "\x00") + "\x00"); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// buildDeleteArgs assembles the rsync argument list for deleting specific paths on a
// remote endpoint: syncing from an empty source directory with --delete-missing-args
// turns every --files-from entry (all missing from the empty source) into a deletion
// request on the destination. This works uniformly over ssh and the daemon protocol —
// no remote shell needed — and is idempotent: an already-gone destination file is a
// no-op. The caller splices the file list in via withFilesFrom. Requires rsync >= 3.1 on
// both ends.
func buildDeleteArgs(emptySrc string, dst Endpoint) []string {
	args := []string{"--recursive", "--delete-missing-args"}
	args = append(args, endpointArgs(dst)...)
	args = append(args, withTrailingSlash(emptySrc), withTrailingSlash(dst.render()))
	return args
}
