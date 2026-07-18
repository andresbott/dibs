package threewayrsync

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestBuildListArgsLocal(t *testing.T) {
	got := buildListArgs(Endpoint{Path: "/data"}, "/tmp/empty", nil, nil)
	for _, want := range []string{"--recursive", "--dry-run", "--itemize-changes", "--out-format=%i %l %M %n", "--exclude=.rsync-partial"} {
		if !slices.Contains(got, want) {
			t.Errorf("list args missing %q: %v", want, got)
		}
	}
	if got[len(got)-2] != "/data/" || got[len(got)-1] != "/tmp/empty/" {
		t.Errorf("src/dst = %q %q", got[len(got)-2], got[len(got)-1])
	}
}

func TestBuildListArgsSSHAddsRsh(t *testing.T) {
	got := buildListArgs(Endpoint{Path: "/data", SSH: &SSH{Host: "nas", Port: 2222}}, "/tmp/empty", nil, nil)
	if !slices.Contains(got, "--rsh=ssh -p 2222") {
		t.Errorf("missing rsh: %v", got)
	}
	if got[len(got)-2] != "nas:/data/" {
		t.Errorf("src = %q", got[len(got)-2])
	}
}

func TestBuildListArgsDaemonAddsPasswordFile(t *testing.T) {
	got := buildListArgs(Endpoint{Path: "p", Daemon: &Daemon{Host: "nas", Module: "m", PasswordFile: "/pw"}}, "/tmp/empty", nil, nil)
	if !slices.Contains(got, "--password-file=/pw") {
		t.Errorf("missing password-file: %v", got)
	}
	if got[len(got)-2] != "rsync://nas/m/p/" {
		t.Errorf("src = %q", got[len(got)-2])
	}
}

func TestBuildTransferArgsDaemon(t *testing.T) {
	got := buildTransferArgs(Endpoint{Path: "/src"}, Endpoint{Daemon: &Daemon{Host: "nas", Module: "m", PasswordFile: "/pw"}}, false, nil, nil)
	if !slices.Contains(got, "--password-file=/pw") {
		t.Errorf("missing password-file: %v", got)
	}
	if got[len(got)-1] != "rsync://nas/m" {
		t.Errorf("dst = %q", got[len(got)-1])
	}
	// No password file => no auth flag.
	got = buildTransferArgs(Endpoint{Path: "/src"}, Endpoint{Daemon: &Daemon{Host: "nas", Module: "m"}}, false, nil, nil)
	for _, a := range got {
		if strings.HasPrefix(a, "--password-file") {
			t.Errorf("unexpected password-file flag: %v", got)
		}
	}
}

func TestBuildTransferArgsHasPartialAndFiles(t *testing.T) {
	got := buildTransferArgs(Endpoint{Path: "/src"}, Endpoint{Path: "/dst"}, false, nil, nil)
	for _, want := range []string{"--links", "--times", "--itemize-changes", "--partial-dir=.rsync-partial", "--exclude=.rsync-partial"} {
		if !slices.Contains(got, want) {
			t.Errorf("transfer args missing %q: %v", want, got)
		}
	}
	if slices.Contains(got, "--checksum") {
		t.Errorf("checksum should be off by default: %v", got)
	}
	// No --recursive: transfers are files-from driven, and a directory entry under
	// --recursive would drag along children the plan never approved.
	if slices.Contains(got, "--recursive") {
		t.Errorf("transfer args must not contain --recursive: %v", got)
	}
	if got[len(got)-2] != "/src/" || got[len(got)-1] != "/dst" {
		t.Errorf("src/dst = %q %q", got[len(got)-2], got[len(got)-1])
	}
}

func TestBuildTransferArgsChecksumAndExclude(t *testing.T) {
	got := buildTransferArgs(Endpoint{Path: "/src"}, Endpoint{Path: "/dst", SSH: &SSH{Host: "h", Port: 2222}}, true, []string{".git"}, nil)
	for _, want := range []string{"--checksum", "--exclude=.git", "--rsh=ssh -p 2222"} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %q: %v", want, got)
		}
	}
	if got[len(got)-1] != "h:/dst" {
		t.Errorf("dst = %q", got[len(got)-1])
	}
}

func TestWithFilesFromInsertsBeforePaths(t *testing.T) {
	args := []string{"--recursive", "--partial", "/src/", "/dst"}
	got := withFilesFrom(args, "/tmp/list")
	if got[len(got)-4] != "--files-from=/tmp/list" || got[len(got)-3] != "--from0" {
		t.Fatalf("files-from/from0 not spliced before paths: %v", got)
	}
	if got[len(got)-2] != "/src/" || got[len(got)-1] != "/dst" {
		t.Fatalf("positional paths disturbed: %v", got)
	}
}

func TestWriteFileListRoundTrip(t *testing.T) {
	// NUL-separated (--from0) so a filename containing a newline stays one entry.
	name, err := writeFileList([]string{"a.txt", "sub/b.txt", "odd\nname.txt"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(name) }()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "a.txt\x00sub/b.txt\x00odd\nname.txt\x00" {
		t.Errorf("file list = %q", string(data))
	}
}

func TestBuildDeleteArgs(t *testing.T) {
	got := buildDeleteArgs("/tmp/empty", Endpoint{Path: "/data", SSH: &SSH{Host: "nas", Port: 2222}})
	for _, want := range []string{"--recursive", "--delete-missing-args", "--rsh=ssh -p 2222"} {
		if !slices.Contains(got, want) {
			t.Errorf("delete args missing %q: %v", want, got)
		}
	}
	if got[len(got)-2] != "/tmp/empty/" || got[len(got)-1] != "nas:/data/" {
		t.Errorf("src/dst = %q %q", got[len(got)-2], got[len(got)-1])
	}

	got = buildDeleteArgs("/tmp/empty", Endpoint{Path: "p", Daemon: &Daemon{Host: "nas", Module: "m", PasswordFile: "/pw"}})
	if !slices.Contains(got, "--password-file=/pw") {
		t.Errorf("missing password-file: %v", got)
	}
	if got[len(got)-1] != "rsync://nas/m/p/" {
		t.Errorf("dst = %q", got[len(got)-1])
	}
}
