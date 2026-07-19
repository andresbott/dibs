package threewayrsync

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

// cannedRunner returns a runner that records the args it was called with into *got and
// returns the given result and error.
func cannedRunner(got *[]string, res runResult, err error) runner {
	return func(_ context.Context, _ string, args []string, _ io.Writer) (runResult, error) {
		*got = args
		return res, err
	}
}

func TestParseModuleList(t *testing.T) {
	// Real format (pinned by TestIntegrationListModules): "%-15s\t%s" per module,
	// possibly preceded by MOTD chatter and blank lines.
	out := "Welcome to the server\n\n" +
		"data           \tfirst module\n" +
		"backup         \t\n" +
		"a much longer module name\tstill skipped: name has spaces\n"
	got := parseModuleList(out)
	want := []Module{{Name: "data", Comment: "first module"}, {Name: "backup"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseModuleList = %+v, want %+v", got, want)
	}
}

func TestParseModuleListEmpty(t *testing.T) {
	if got := parseModuleList(""); len(got) != 0 {
		t.Errorf("parseModuleList(\"\") = %+v, want empty", got)
	}
	// MOTD-only output (all modules hidden with "list = false") yields no modules.
	if got := parseModuleList("MOTD line without modules\n"); len(got) != 0 {
		t.Errorf("parseModuleList(MOTD only) = %+v, want empty", got)
	}
}

func TestParseDirList(t *testing.T) {
	// Real format (pinned by TestIntegrationListDirs): mode, size (comma-grouped),
	// date, time, name; the "." self entry leads; names may contain spaces.
	out := "drwxr-xr-x          4,096 2026/07/19 15:48:06 .\n" +
		"-rw-r--r--              3 2026/07/19 15:48:06 file.txt\n" +
		"drwxr-xr-x             40 2026/07/19 15:48:06 dir with space\n" +
		"lrwxrwxrwx              6 2026/07/19 15:48:06 alink -> target\n" +
		"drwxr-xr-x             60 2026/07/19 15:48:06 subdir1\n"
	got, err := parseDirList(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"dir with space", "subdir1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseDirList = %v, want %v", got, want)
	}
}

func TestParseDirListSkipsChatter(t *testing.T) {
	// A "d"-leading line that is not a mode string (chatter) is skipped, not an error.
	got, err := parseDirList("downloading file list ...\ndone\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("parseDirList(chatter) = %v, want empty", got)
	}
}

func TestParseDirListMalformedDirLineErrors(t *testing.T) {
	// Directory-shaped (valid mode string) but missing fields: silently dropping it
	// would make an existing folder unbrowsable, so it must error.
	if _, err := parseDirList("drwxr-xr-x 4,096\n"); err == nil {
		t.Fatal("want error for malformed dir line")
	}
}

func TestBuildModuleListArgs(t *testing.T) {
	got := buildModuleListArgs(Daemon{Host: "h", Port: 874, User: "u", PasswordFile: "/pw"})
	// No password flag: the module listing is unauthenticated (auth is per-module).
	want := []string{"--contimeout=10", "rsync://u@h:874/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildModuleListArgs = %v, want %v", got, want)
	}
}

func TestBuildModuleListArgsDefaults(t *testing.T) {
	got := buildModuleListArgs(Daemon{Host: "h"})
	want := []string{"--contimeout=10", "rsync://h/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildModuleListArgs = %v, want %v", got, want)
	}
}

func TestBuildDirListArgs(t *testing.T) {
	got := buildDirListArgs(Daemon{Host: "h", Module: "mod", PasswordFile: "/pw"}, "a/b")
	want := []string{"--list-only", "--contimeout=10", "--password-file=/pw", "rsync://h/mod/a/b/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildDirListArgs = %v, want %v", got, want)
	}
}

func TestBuildDirListArgsModuleRoot(t *testing.T) {
	got := buildDirListArgs(Daemon{Host: "h", Module: "mod"}, "")
	want := []string{"--list-only", "--contimeout=10", "rsync://h/mod/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildDirListArgs = %v, want %v", got, want)
	}
}

func TestListModules(t *testing.T) {
	var args []string
	out := "data           \tfirst module\nbackup         \t\n"
	s := &Syncer{run: cannedRunner(&args, runResult{stdout: out}, nil)}
	got, err := s.ListModules(context.Background(), Daemon{Host: "h", Module: "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Module{{Name: "data", Comment: "first module"}, {Name: "backup"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListModules = %+v, want %+v", got, want)
	}
	if args[len(args)-1] != "rsync://h/" {
		t.Errorf("last arg = %q, want the module-less root URL", args[len(args)-1])
	}
}

func TestListModulesRunError(t *testing.T) {
	var args []string
	s := &Syncer{run: cannedRunner(&args, runResult{stderr: "boom", exitCode: 10}, errors.New("exit status 10"))}
	_, err := s.ListModules(context.Background(), Daemon{Host: "h"})
	var e *Error
	if !errors.As(err, &e) || e.Op != "list-modules" || e.ExitCode != 10 || !strings.Contains(e.Stderr, "boom") {
		t.Fatalf("err = %v, want *Error{Op: list-modules, exit 10, stderr boom}", err)
	}
}

func TestListModulesValidatesHost(t *testing.T) {
	s := &Syncer{run: cannedRunner(new([]string), runResult{}, nil)}
	for _, host := range []string{"", "-oEvil", "h h"} {
		if _, err := s.ListModules(context.Background(), Daemon{Host: host}); err == nil {
			t.Errorf("host %q: want validation error", host)
		}
	}
}

func TestListModulesCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &Syncer{run: cannedRunner(new([]string), runResult{}, errors.New("killed"))}
	_, err := s.ListModules(ctx, Daemon{Host: "h"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestListDirs(t *testing.T) {
	var args []string
	out := "drwxr-xr-x          4,096 2026/07/19 15:48:06 .\n" +
		"drwxr-xr-x             60 2026/07/19 15:48:06 sub\n"
	s := &Syncer{run: cannedRunner(&args, runResult{stdout: out}, nil)}
	got, err := s.ListDirs(context.Background(), Daemon{Host: "h", Module: "mod"}, "a")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"sub"}) {
		t.Errorf("ListDirs = %v, want [sub]", got)
	}
	if args[len(args)-1] != "rsync://h/mod/a/" {
		t.Errorf("last arg = %q, want the trailing-slash module path", args[len(args)-1])
	}
}

func TestListDirsRunError(t *testing.T) {
	s := &Syncer{run: cannedRunner(new([]string), runResult{stderr: "no such dir", exitCode: 23}, errors.New("exit status 23"))}
	_, err := s.ListDirs(context.Background(), Daemon{Host: "h", Module: "mod"}, "nope")
	var e *Error
	if !errors.As(err, &e) || e.Op != "list-dirs" || e.ExitCode != 23 {
		t.Fatalf("err = %v, want *Error{Op: list-dirs, exit 23}", err)
	}
}

func TestListDirsRequiresModule(t *testing.T) {
	s := &Syncer{run: cannedRunner(new([]string), runResult{}, nil)}
	if _, err := s.ListDirs(context.Background(), Daemon{Host: "h"}, ""); err == nil {
		t.Fatal("want validation error for missing module")
	}
}

func TestListDirsRejectsUnsafePath(t *testing.T) {
	s := &Syncer{run: cannedRunner(new([]string), runResult{}, nil)}
	for _, p := range []string{"../evil", "/abs", "a/../b"} {
		if _, err := s.ListDirs(context.Background(), Daemon{Host: "h", Module: "mod"}, p); err == nil {
			t.Errorf("path %q: want unsafe-path error", p)
		}
	}
}

func TestListDirsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &Syncer{run: cannedRunner(new([]string), runResult{}, errors.New("killed"))}
	_, err := s.ListDirs(ctx, Daemon{Host: "h", Module: "mod"}, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
