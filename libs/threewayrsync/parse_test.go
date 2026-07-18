package threewayrsync

import (
	"strings"
	"testing"
	"time"
)

func TestParseListOutput(t *testing.T) {
	out := "sending incremental file list\n" +
		">f+++++++++ 1 2026/07/14-09:15:00 a.txt\n" +
		">f+++++++++ 2 2026/07/14-09:15:01 sub/b.txt\n" +
		"cd+++++++++ 4096 2026/07/14-09:15:00 sub/\n" +
		".d..t...... 4096 2026/07/14-09:15:00 ./\n" +
		"\n"
	m, err := parseListOutput(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 3 {
		t.Fatalf("want 2 files + 1 dir, got %d: %+v", len(m), m)
	}
	if m["a.txt"].Size != 1 {
		t.Errorf("a.txt size = %d, want 1", m["a.txt"].Size)
	}
	want := time.Date(2026, 7, 14, 9, 15, 1, 0, time.Local)
	if !m["sub/b.txt"].ModTime.Equal(want) {
		t.Errorf("sub/b.txt mtime = %v, want %v", m["sub/b.txt"].ModTime, want)
	}
	// The dir enters keyed without the trailing slash, presence-only (size dropped).
	sub, ok := m["sub"]
	if !ok || !sub.IsDir {
		t.Fatalf("directory must enter the manifest as IsDir: %+v", m)
	}
	if sub.Size != 0 {
		t.Errorf("dir size must be dropped (inode size is meaningless), got %d", sub.Size)
	}
}

func TestParseListLineSkipsChatterAndNonEntries(t *testing.T) {
	for _, line := range []string{
		"sending incremental file list",
		"",
		".d..t...... 4096 2026/07/14-09:15:00 ./", // root
		"cL+++++++++ 7 2026/07/14-09:15:00 link",  // symlink
	} {
		_, _, ok, err := parseListLine(line)
		if ok || err != nil {
			t.Errorf("line should be silently skipped: %q (ok=%v err=%v)", line, ok, err)
		}
	}
}

func TestParseListLineDir(t *testing.T) {
	fs, path, ok, err := parseListLine("cd+++++++++ 80 2026/07/14-09:15:00 a/b/")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if path != "a/b" {
		t.Errorf("path = %q, want %q (trailing slash stripped)", path, "a/b")
	}
	if !fs.IsDir || fs.Size != 0 {
		t.Errorf("state = %+v, want IsDir with size 0", fs)
	}
}

// A line that is itemize-shaped but malformed must be an error, not a silent skip:
// dropping entries makes the corresponding files look deleted.
func TestParseListLineErrorsOnMalformedItemize(t *testing.T) {
	for _, line := range []string{
		">f+++++++++ notasize 2026/07/14-09:15:00 a.txt", // bad size
		">f+++++++++ 1 14-07-2026 a.txt",                 // bad mtime format
		">f+++++++++ 1",                                  // truncated
		">f+++++++++ 1 2026/07/14-09:15:00 /etc/passwd",  // absolute path
		">f+++++++++ 1 2026/07/14-09:15:00 ../escape",    // traversal
	} {
		if _, _, _, err := parseListLine(line); err == nil {
			t.Errorf("want parse error for %q", line)
		}
	}
}

func TestParseListOutputPropagatesError(t *testing.T) {
	if _, err := parseListOutput(">f+++++++++ bad 2026/07/14-09:15:00 x\n"); err == nil {
		t.Fatal("want error for malformed itemize line")
	}
}

// rsync escapes unprintable filename bytes as \#ooo; the manifest must carry the real
// on-disk name or deletes miss it and the file resurrects next sync.
func TestParseListLineDecodesEscapes(t *testing.T) {
	fs, path, ok, err := parseListLine(`>f+++++++++ 1 2026/07/14-09:15:00 a\#012b.txt`)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if path != "a\nb.txt" {
		t.Errorf("path = %q, want %q", path, "a\nb.txt")
	}
	if fs.Size != 1 {
		t.Errorf("size = %d", fs.Size)
	}
}

func TestDecodeListPath(t *testing.T) {
	cases := map[string]string{
		"plain.txt":     "plain.txt",
		`tab\#011x`:     "tab\tx",
		`nl\#012x`:      "nl\nx",
		`not\#9x`:       `not\#9x`, // not three octal digits: literal
		`trail\#`:       `trail\#`,
		`back\\slash.t`: `back\\slash.t`, // plain backslash untouched
	}
	for in, want := range cases {
		if got := decodeListPath(in); got != want {
			t.Errorf("decodeListPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSafeRelPath(t *testing.T) {
	for p, want := range map[string]bool{
		"a.txt":     true,
		"sub/b.txt": true,
		"a\nb.txt":  true, // newline is a legal filename byte; the file list is NUL-separated
		"/abs":      false,
		"../up":     false,
		"a/../b":    false,
		"a/./b":     false,
		"a//b":      false,
		"":          false,
		"a\x00b":    false, // NUL is the file-list separator
	} {
		if got := safeRelPath(p); got != want {
			t.Errorf("safeRelPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestItemizeWriterStreamsPaths(t *testing.T) {
	var got []string
	w := &itemizeWriter{onPath: func(p string) { got = append(got, p) }}
	// Split one line across two writes to prove partial input is buffered. The
	// directory row (cd) surfaces too — a transfer only itemizes a dir it creates —
	// with its trailing slash stripped; the symlink row (cL) is skipped.
	chunks := []string{
		"sending incremental file list\ncd+++++++++ sub/\ncL+++++++++ link\n>f+++++++++ new.txt\n>f.st",
		"...... changed.txt\n",
	}
	for _, c := range chunks {
		if _, err := w.Write([]byte(c)); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"sub", "new.txt", "changed.txt"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("streamed paths = %v, want %v", got, want)
	}
}

func TestParseListOutputLongLine(t *testing.T) {
	long := strings.Repeat("d/", 500) + "x.txt"
	m, err := parseListOutput(">f+++++++++ 1 2026/07/14-09:15:00 " + long + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m[long]; !ok {
		t.Error("long path missing")
	}
}
