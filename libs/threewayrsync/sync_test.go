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

// listRouter returns a runner that answers each list call with canned output chosen by the
// source path (args[len-2] is "<src>/", args[len-1] is the empty dest).
func listRouter(t *testing.T, bySrc map[string]string) runner {
	t.Helper()
	return func(_ context.Context, _ string, args []string, _ io.Writer) (runResult, error) {
		src := args[len(args)-2]
		for prefix, out := range bySrc {
			if strings.HasPrefix(src, prefix) {
				return runResult{stdout: out}, nil
			}
		}
		t.Fatalf("unexpected list source %q", src)
		return runResult{}, nil
	}
}

func TestDiffClassifiesFromListings(t *testing.T) {
	local, remote := Endpoint{Path: t.TempDir()}, Endpoint{Path: t.TempDir()}
	// base empty => a.txt is a local-only add (Push); b.txt is a remote-only add (Pull).
	run := listRouter(t, map[string]string{
		local.Path + "/":  ">f+++++++++ 1 2026/07/14-09:15:00 a.txt\n",
		remote.Path + "/": ">f+++++++++ 2 2026/07/14-09:15:00 b.txt\n",
	})
	s := &Syncer{Store: &memStore{}, run: run}
	plan, err := s.Diff(context.Background(), local, remote, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Push) != 1 || plan.Push[0] != "a.txt" {
		t.Errorf("Push = %v", plan.Push)
	}
	if len(plan.Pull) != 1 || plan.Pull[0] != "b.txt" {
		t.Errorf("Pull = %v", plan.Pull)
	}
}

func TestDiffValidatesEndpoints(t *testing.T) {
	s := &Syncer{Store: &memStore{}, run: listRouter(t, nil)}
	_, err := s.Diff(context.Background(), Endpoint{Path: ""}, Endpoint{Path: "/r"}, Options{})
	if err == nil {
		t.Fatal("want validation error for empty local path")
	}
}

// A missing local directory with an empty in-scope base is just a working copy
// that hasn't been created yet: Diff (read-only) must produce the plan as if the
// directory were empty, without requiring the caller to create it.
func TestDiffMissingLocalDirEmptyBaseListsAsEmpty(t *testing.T) {
	local := Endpoint{Path: filepath.Join(t.TempDir(), "not-created-yet")}
	remote := Endpoint{Path: t.TempDir()}
	run := listRouter(t, map[string]string{
		remote.Path + "/": ">f+++++++++ 2 2026/07/14-09:15:00 b.txt\n",
	})
	s := &Syncer{Store: &memStore{}, run: run}
	plan, err := s.Diff(context.Background(), local, remote, Options{})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(plan.Pull) != 1 || plan.Pull[0] != "b.txt" {
		t.Errorf("Pull = %v", plan.Pull)
	}
	if _, err := os.Stat(local.Path); !os.IsNotExist(err) {
		t.Error("Diff must not create the local dir")
	}
}

// With a non-empty in-scope base, a missing local dir means an unmounted disk:
// Diff must refuse, not plan a mass deletion.
func TestDiffMissingLocalDirNonEmptyBaseErrors(t *testing.T) {
	local := Endpoint{Path: filepath.Join(t.TempDir(), "gone")}
	remote := Endpoint{Path: t.TempDir()}
	run := listRouter(t, map[string]string{
		remote.Path + "/": ">f+++++++++ 2 2026/07/14-09:15:00 b.txt\n",
	})
	base := Manifest{"b.txt": {Size: 2}}
	s := &Syncer{Store: &memStore{base: base, ok: true}, run: run}
	if _, err := s.Diff(context.Background(), local, remote, Options{}); err == nil {
		t.Fatal("diff must refuse a missing local dir when the base records files")
	}
}

func TestListWrapsRunnerError(t *testing.T) {
	boom := func(_ context.Context, _ string, _ []string, _ io.Writer) (runResult, error) {
		return runResult{stderr: "nope", exitCode: 12}, io.ErrUnexpectedEOF
	}
	s := &Syncer{Store: &memStore{}, run: boom}
	_, err := s.Diff(context.Background(), Endpoint{Path: t.TempDir()}, Endpoint{Path: t.TempDir()}, Options{})
	var e *Error
	if !errors.As(err, &e) || e.Op != "list" {
		t.Fatalf("want *Error op=list, got %v", err)
	}
}
