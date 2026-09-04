package threewayrsync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MakeDir creates the directory at path (a slash-relative path inside d.Module)
// on the rsync daemon. rsync has no mkdir verb, so it transfers a local empty
// directory named after path's last segment into path's parent: with
// --recursive rsync creates that (empty) directory on the remote. path's parent
// must already exist — the browser only creates a folder inside a directory it
// is already listing — and creating an already-present directory is a harmless
// no-op. A read-only module rejects the transfer; that surfaces as the returned
// *Error.
func (s *Syncer) MakeDir(ctx context.Context, d Daemon, path string) error {
	if err := validateDaemonFields(&d, true); err != nil {
		return err
	}
	if !safeRelPath(path) {
		return fmt.Errorf("make-dir: unsafe path %q", path)
	}
	parent, leaf := "", path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		parent, leaf = path[:i], path[i+1:]
	}

	// Build a local "<tmp>/<leaf>" empty directory; transferring it (source
	// without a trailing slash) into the parent creates <parent>/<leaf> remotely.
	tmp, err := os.MkdirTemp("", "threewayrsync-mkdir-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	src := filepath.Join(tmp, leaf)
	if err := os.Mkdir(src, 0o755); err != nil {
		return err
	}

	args := buildMakeDirArgs(src, Endpoint{Path: parent, Daemon: &d})
	res, err := s.runner()(ctx, s.bin(), args, nil)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &Error{Op: "make-dir", Args: args, Stderr: res.stderr, ExitCode: res.exitCode, Err: err}
	}
	return nil
}
