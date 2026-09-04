package threewayrsync

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestMakeDir(t *testing.T) {
	var args []string
	s := &Syncer{run: cannedRunner(&args, runResult{}, nil)}
	if err := s.MakeDir(context.Background(), Daemon{Host: "h", Module: "mod", PasswordFile: "/pw"}, "sub/new"); err != nil {
		t.Fatal(err)
	}
	// Dest is the parent directory (trailing slash); the leaf is created inside it.
	if args[len(args)-1] != "rsync://h/mod/sub/" {
		t.Errorf("dest arg = %q, want the parent path with a trailing slash", args[len(args)-1])
	}
	if !contains(args, "--recursive") {
		t.Errorf("args %v missing --recursive", args)
	}
	if !contains(args, "--password-file=/pw") {
		t.Errorf("args %v missing --password-file (auth)", args)
	}
	// Source is a local empty directory named after the leaf, transferred as-is
	// (no trailing slash) so rsync creates it under the parent.
	if src := args[len(args)-2]; filepath.Base(src) != "new" || strings.HasSuffix(src, "/") {
		t.Errorf("source arg = %q, want a no-slash .../new empty dir", src)
	}
}

func TestMakeDirRunError(t *testing.T) {
	s := &Syncer{run: cannedRunner(new([]string), runResult{stderr: "@ERROR: chdir failed", exitCode: 5}, errors.New("exit status 5"))}
	err := s.MakeDir(context.Background(), Daemon{Host: "h", Module: "mod"}, "new")
	var e *Error
	if !errors.As(err, &e) || e.Op != "make-dir" || e.ExitCode != 5 {
		t.Fatalf("err = %v, want *Error{Op: make-dir, exit 5}", err)
	}
}

func TestMakeDirRequiresModule(t *testing.T) {
	s := &Syncer{run: cannedRunner(new([]string), runResult{}, nil)}
	if err := s.MakeDir(context.Background(), Daemon{Host: "h"}, "new"); err == nil {
		t.Fatal("want validation error for a missing module")
	}
}

func TestMakeDirRejectsUnsafePath(t *testing.T) {
	s := &Syncer{run: cannedRunner(new([]string), runResult{}, nil)}
	for _, p := range []string{"", "../evil", "/abs", "a/../b"} {
		if err := s.MakeDir(context.Background(), Daemon{Host: "h", Module: "mod"}, p); err == nil {
			t.Errorf("path %q: want unsafe-path error", p)
		}
	}
}

func TestMakeDirCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &Syncer{run: cannedRunner(new([]string), runResult{}, errors.New("killed"))}
	if err := s.MakeDir(ctx, Daemon{Host: "h", Module: "mod"}, "new"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
