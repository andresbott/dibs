package threewayrsync

import (
	"context"
	"errors"
	"strings"
)

// CheckConnection verifies a daemon endpoint is reachable and, where a module
// requires it, that the configured credentials are accepted. It first lists the
// daemon's modules (unauthenticated — this proves the host is reachable and
// speaking the rsync daemon protocol); a failure here is the connection error
// and is returned as-is. It then attempts an authenticated listing of each
// advertised module: if any module rejects the credentials with "auth failed",
// that error is returned. A module that fails to list for some other reason (a
// path/chroot error, a permission denial) is not a connection failure — the
// daemon answered and the password was not rejected — so it is ignored.
//
// Because rsync daemon auth is per-module and a server carries no module, this
// probes every advertised module rather than one specific target. It cannot
// validate a password against a module that requires no auth (rsync only sends
// the password when the daemon challenges for it).
func (s *Syncer) CheckConnection(ctx context.Context, d Daemon) error {
	d.Module = ""
	mods, err := s.ListModules(ctx, d)
	if err != nil {
		return err
	}
	for _, m := range mods {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		dm := d
		dm.Module = m.Name
		if _, err := s.ListDirs(ctx, dm, ""); err != nil {
			if isAuthFailure(err) {
				return err
			}
		}
	}
	return nil
}

// isAuthFailure reports whether err is an rsync daemon authentication rejection
// ("@ERROR: auth failed on module …").
func isAuthFailure(err error) bool {
	var e *Error
	if errors.As(err, &e) {
		return strings.Contains(strings.ToLower(e.Stderr), "auth failed")
	}
	return false
}
