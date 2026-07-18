package threewayrsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
)

// validateOne checks a single endpoint (the pair rule of validate does not apply when only
// one side is an Endpoint and the other is a plain local file).
func validateOne(e Endpoint) error {
	if e.SSH != nil && e.Daemon != nil {
		return errors.New("endpoint sets both ssh and daemon")
	}
	if e.Daemon == nil && strings.TrimSpace(e.Path) == "" {
		return errors.New("endpoint path is required")
	}
	if err := validateSSH(e.SSH); err != nil {
		return err
	}
	return validateDaemon(e.Daemon)
}

// fileLoc renders the rsync-syntax location of one file inside an endpoint: the endpoint's
// rendered root plus the slash-relative path.
func fileLoc(e Endpoint, rel string) string {
	return strings.TrimRight(e.render(), "/") + "/" + rel
}

// checkFileArgs validates the endpoint/rel pair shared by the single-file helpers.
func checkFileArgs(e Endpoint, rel string) error {
	if err := validateOne(e); err != nil {
		return err
	}
	if !safeRelPath(rel) {
		return fmt.Errorf("relative path %q is not a safe relative path", rel)
	}
	return nil
}

// FetchFile copies the single file rel (slash-relative to the endpoint root) to the local
// path dstPath, over whatever transport the endpoint uses. A missing source file returns
// (false, nil) — rsync reports it as a partial-transfer failure (exit 23, or 24 when it
// vanishes mid-run), which is indistinguishable from e.g. an unreadable source, so any
// exit-23 failure is treated as "not found"; a genuinely broken endpoint keeps failing
// loudly on the operations that follow. Callers use this to probe for a marker file on a
// remote endpoint.
func (s *Syncer) FetchFile(ctx context.Context, e Endpoint, rel, dstPath string) (bool, error) {
	if err := checkFileArgs(e, rel); err != nil {
		return false, err
	}
	args := []string{"--times"}
	args = append(args, endpointArgs(e)...)
	args = append(args, fileLoc(e, rel), dstPath)
	res, err := s.runner()(ctx, s.bin(), args, nil)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if res.exitCode == 23 || res.exitCode == 24 {
			return false, nil
		}
		return false, &Error{Op: "fetch", Args: args, Stderr: res.stderr, ExitCode: res.exitCode, Err: err}
	}
	return true, nil
}

// PutFile copies the local file srcPath to rel (slash-relative to the endpoint root) on the
// endpoint. The destination's parent directory must already exist. The new remote file's
// permissions derive from srcPath's mode, so callers chmod the source first when the
// destination must be e.g. world-readable. rsync's default temp-file-plus-rename delivery
// makes the write effectively atomic on the destination.
func (s *Syncer) PutFile(ctx context.Context, e Endpoint, rel, srcPath string) error {
	if err := checkFileArgs(e, rel); err != nil {
		return err
	}
	if _, err := os.Stat(srcPath); err != nil {
		return err
	}
	args := []string{"--times", "--perms"}
	args = append(args, endpointArgs(e)...)
	args = append(args, srcPath, fileLoc(e, rel))
	res, err := s.runner()(ctx, s.bin(), args, nil)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &Error{Op: "put", Args: args, Stderr: res.stderr, ExitCode: res.exitCode, Err: err}
	}
	return nil
}

// DeleteFile removes the single file rel (slash-relative to the endpoint root) from the
// endpoint. A file already gone is not an error. Filesystem endpoints use os.Remove;
// remote endpoints (ssh or daemon) reuse the rsync --delete-missing-args mechanism.
func (s *Syncer) DeleteFile(ctx context.Context, e Endpoint, rel string) error {
	if err := checkFileArgs(e, rel); err != nil {
		return err
	}
	if !e.remote() {
		abs := path.Join(e.Path, rel)
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	_, err := s.deleteRemote(ctx, e, []string{rel})
	return err
}
