package threewayrsync

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fileRunner records single-file rsync calls and returns a canned result.
type fileRunner struct {
	calls [][]string
	res   runResult
	err   error
}

func (f *fileRunner) run(_ context.Context, _ string, args []string, _ io.Writer) (runResult, error) {
	f.calls = append(f.calls, args)
	return f.res, f.err
}

func TestFetchFileBuildsArgs(t *testing.T) {
	fr := &fileRunner{}
	s := &Syncer{run: fr.run}
	e := Endpoint{Path: "/remote/root", SSH: &SSH{User: "u", Host: "h"}}
	found, err := s.FetchFile(context.Background(), e, ".netcheckout.json", "/tmp/dst")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	args := fr.calls[0]
	src := args[len(args)-2]
	if src != "u@h:/remote/root/.netcheckout.json" {
		t.Errorf("src = %q", src)
	}
	if args[len(args)-1] != "/tmp/dst" {
		t.Errorf("dst = %q", args[len(args)-1])
	}
}

func TestFetchFileMissingIsNotFound(t *testing.T) {
	for _, code := range []int{23, 24} {
		fr := &fileRunner{res: runResult{exitCode: code, stderr: "No such file or directory"}, err: errors.New("exit status")}
		s := &Syncer{run: fr.run}
		found, err := s.FetchFile(context.Background(), Endpoint{Path: "/r", SSH: &SSH{Host: "h"}}, "m.json", "/tmp/dst")
		if err != nil {
			t.Fatalf("exit %d: err = %v", code, err)
		}
		if found {
			t.Errorf("exit %d: should report not found", code)
		}
	}
}

func TestFetchFileOtherFailureIsError(t *testing.T) {
	fr := &fileRunner{res: runResult{exitCode: 12, stderr: "connection closed"}, err: errors.New("exit status 12")}
	s := &Syncer{run: fr.run}
	_, err := s.FetchFile(context.Background(), Endpoint{Path: "/r", SSH: &SSH{Host: "h"}}, "m.json", "/tmp/dst")
	var terr *Error
	if !errors.As(err, &terr) || terr.ExitCode != 12 {
		t.Fatalf("err = %v", err)
	}
}

func TestPutFileBuildsArgsAndRequiresSource(t *testing.T) {
	src := filepath.Join(t.TempDir(), "marker.json")
	if err := os.WriteFile(src, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	fr := &fileRunner{}
	s := &Syncer{run: fr.run}
	e := Endpoint{Daemon: &Daemon{Host: "h", Module: "mod"}}
	if err := s.PutFile(context.Background(), e, ".netcheckout.json", src); err != nil {
		t.Fatal(err)
	}
	args := fr.calls[0]
	if dst := args[len(args)-1]; dst != "rsync://h/mod/.netcheckout.json" {
		t.Errorf("dst = %q", dst)
	}
	if err := s.PutFile(context.Background(), e, "m.json", filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("missing source must error before any rsync call")
	}
}

func TestDeleteFileLocalRemovesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "m.json")
	if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Syncer{run: (&fileRunner{}).run}
	if err := s.DeleteFile(context.Background(), Endpoint{Path: dir}, "m.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("file should be gone, stat err = %v", err)
	}
	if err := s.DeleteFile(context.Background(), Endpoint{Path: dir}, "m.json"); err != nil {
		t.Errorf("second delete must be a no-op, got %v", err)
	}
}

func TestDeleteFileRemoteUsesDeleteMissingArgs(t *testing.T) {
	fr := &fileRunner{}
	s := &Syncer{run: fr.run}
	e := Endpoint{Path: "/remote/root", SSH: &SSH{Host: "h"}}
	if err := s.DeleteFile(context.Background(), e, "sub/m.json"); err != nil {
		t.Fatal(err)
	}
	var hasFlag bool
	for _, a := range fr.calls[0] {
		if a == "--delete-missing-args" {
			hasFlag = true
		}
	}
	if !hasFlag {
		t.Errorf("remote delete args = %v", fr.calls[0])
	}
}

func TestFileHelpersRejectUnsafePaths(t *testing.T) {
	s := &Syncer{run: (&fileRunner{}).run}
	e := Endpoint{Path: "/r", SSH: &SSH{Host: "h"}}
	for _, rel := range []string{"", "/abs", "../up", "a/../b"} {
		if _, err := s.FetchFile(context.Background(), e, rel, "/tmp/x"); err == nil || !strings.Contains(err.Error(), "safe relative path") {
			t.Errorf("FetchFile(%q) err = %v", rel, err)
		}
		if err := s.DeleteFile(context.Background(), e, rel); err == nil {
			t.Errorf("DeleteFile(%q) must error", rel)
		}
	}
}
