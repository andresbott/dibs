package threewayrsync

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// urlRunner returns a runner that dispatches on the trailing URL argument: the
// module-less root URL ("rsync://h/") yields the module listing, and a module
// URL ("rsync://h/mod/…") yields byURL[<url>] (a canned per-module result). A
// module URL with no entry in byURL succeeds with empty output.
func urlRunner(t *testing.T, moduleList string, byURL map[string]struct {
	res runResult
	err error
}) runner {
	t.Helper()
	return func(_ context.Context, _ string, args []string, _ io.Writer) (runResult, error) {
		url := args[len(args)-1]
		// "rsync://[user@]host[:port]/" (nothing after the authority slash) is the
		// module listing; "rsync://…/module/…" is a per-module dir listing.
		_, after, _ := strings.Cut(strings.TrimPrefix(url, "rsync://"), "/")
		if after == "" {
			return runResult{stdout: moduleList}, nil
		}
		if r, ok := byURL[url]; ok {
			return r.res, r.err
		}
		return runResult{}, nil
	}
}

const authFailStderr = "@ERROR: auth failed on module mainData"

func TestCheckConnectionUnreachable(t *testing.T) {
	// The module listing itself fails: the host is unreachable / refused. That is
	// the connection error and is returned verbatim.
	s := &Syncer{run: cannedRunner(new([]string), runResult{stderr: "connection refused", exitCode: 10}, errors.New("exit status 10"))}
	err := s.CheckConnection(context.Background(), Daemon{Host: "h", User: "u", PasswordFile: "/pw"})
	var e *Error
	if !errors.As(err, &e) || e.Op != "list-modules" {
		t.Fatalf("err = %v, want a list-modules *Error", err)
	}
}

func TestCheckConnectionAuthFailure(t *testing.T) {
	// The daemon lists its module, but the authenticated listing is rejected with
	// "auth failed": the credentials are wrong, so the check fails with that error.
	run := urlRunner(t, "mainData       \tMain Data\n", map[string]struct {
		res runResult
		err error
	}{
		"rsync://u@h/mainData/": {runResult{stderr: authFailStderr, exitCode: 5}, errors.New("exit status 5")},
	})
	s := &Syncer{run: run}
	err := s.CheckConnection(context.Background(), Daemon{Host: "h", User: "u", PasswordFile: "/pw"})
	if err == nil || !strings.Contains(err.Error(), "auth failed") {
		t.Fatalf("err = %v, want an auth-failed error", err)
	}
}

func TestCheckConnectionAuthFailureOnLaterModule(t *testing.T) {
	// The first module authenticates fine, a later one rejects the password. A
	// per-module auth failure anywhere fails the check (all modules are probed).
	run := urlRunner(t, "open           \tno auth\nlocked         \tauth\n", map[string]struct {
		res runResult
		err error
	}{
		"rsync://u@h/locked/": {runResult{stderr: authFailStderr, exitCode: 5}, errors.New("exit status 5")},
	})
	s := &Syncer{run: run}
	err := s.CheckConnection(context.Background(), Daemon{Host: "h", User: "u", PasswordFile: "/pw"})
	if err == nil || !strings.Contains(err.Error(), "auth failed") {
		t.Fatalf("err = %v, want an auth-failed error", err)
	}
}

func TestCheckConnectionOK(t *testing.T) {
	// Reachable daemon whose module authenticates: no error.
	run := urlRunner(t, "mainData       \tMain Data\n", nil)
	s := &Syncer{run: run}
	if err := s.CheckConnection(context.Background(), Daemon{Host: "h", User: "u", PasswordFile: "/pw"}); err != nil {
		t.Fatalf("CheckConnection = %v, want nil", err)
	}
}

func TestCheckConnectionIgnoresNonAuthListError(t *testing.T) {
	// A module that fails to list for a non-auth reason (e.g. a chroot/path error)
	// does not fail the check: the daemon answered and the credentials were not
	// rejected, so the connection is considered valid.
	run := urlRunner(t, "mainData       \tMain Data\n", map[string]struct {
		res runResult
		err error
	}{
		"rsync://u@h/mainData/": {runResult{stderr: "rsync: change_dir failed", exitCode: 23}, errors.New("exit status 23")},
	})
	s := &Syncer{run: run}
	if err := s.CheckConnection(context.Background(), Daemon{Host: "h", User: "u", PasswordFile: "/pw"}); err != nil {
		t.Fatalf("CheckConnection = %v, want nil (non-auth list error ignored)", err)
	}
}

func TestCheckConnectionCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &Syncer{run: cannedRunner(new([]string), runResult{}, errors.New("killed"))}
	if err := s.CheckConnection(ctx, Daemon{Host: "h"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
