package threewayrsync

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// listMtimeLayout matches rsync's %M escape: "YYYY/MM/DD-HH:MM:SS" in local time. If the
// Task 1 fixture shows a different form, update this and parseListLine together.
const listMtimeLayout = "2006/01/02-15:04:05"

// parseListOutput turns "%i %l %M %n" list output into a Manifest of regular files and
// directories. A line that is itemize-shaped but fails to parse is an error, not a skip:
// silently dropping entries would make every dropped file look deleted and turn format
// drift into data loss.
func parseListOutput(out string) (Manifest, error) {
	m := Manifest{}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		fs, path, ok, err := parseListLine(sc.Text())
		if err != nil {
			return nil, fmt.Errorf("parsing rsync list output: %w", err)
		}
		if ok {
			m[path] = fs
		}
	}
	return m, sc.Err()
}

// parseListLine parses one "%i %l %M %n" line, keeping regular files and directories.
// Non-itemize chatter (blank lines, the "sending incremental file list" banner) and
// other entry types (symlinks, the "./" root) are skipped with ok=false. A directory
// line's path carries a trailing slash (rsync %n) which is stripped, and its size — a
// meaningless inode size — is dropped: directory entries are presence-only. An
// itemize-shaped line that cannot be fully parsed, or whose path is not a safe relative
// path, returns an error.
func parseListLine(line string) (FileState, string, bool, error) {
	parts := strings.SplitN(line, " ", 4)
	flags := parts[0]
	if len(flags) != 11 || !strings.ContainsRune("<>ch.*", rune(flags[0])) {
		return FileState{}, "", false, nil // not an itemize line: chatter
	}
	isDir := flags[1] == 'd'
	if flags[1] != 'f' && !isDir { // regular files and directories only (2nd flag char)
		return FileState{}, "", false, nil
	}
	if len(parts) < 4 {
		return FileState{}, "", false, fmt.Errorf("malformed itemize line %q", line)
	}
	sizeStr, mtimeStr, path := parts[1], parts[2], parts[3]
	if path == "" || path == "./" {
		return FileState{}, "", false, nil
	}
	size, err := strconv.ParseInt(strings.ReplaceAll(sizeStr, ",", ""), 10, 64)
	if err != nil {
		return FileState{}, "", false, fmt.Errorf("bad size in itemize line %q: %w", line, err)
	}
	mtime, err := time.ParseInLocation(listMtimeLayout, mtimeStr, time.Local)
	if err != nil {
		return FileState{}, "", false, fmt.Errorf("bad mtime in itemize line %q: %w", line, err)
	}
	path = decodeListPath(path)
	if isDir {
		path = strings.TrimSuffix(path, "/")
		size = 0
	}
	if !safeRelPath(path) {
		return FileState{}, "", false, fmt.Errorf("unsafe path in itemize line %q", line)
	}
	return FileState{Size: size, ModTime: mtime, IsDir: isDir}, path, true, nil
}

// decodeListPath undoes rsync's escaping of unprintable bytes in %n output: "\#ooo" with
// three octal digits. Without decoding, such a path exists in the manifest but not on
// disk, so deletes silently miss it and the file resurrects on the next sync.
func decodeListPath(s string) string {
	if !strings.Contains(s, `\#`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+4 < len(s) && s[i+1] == '#' &&
			isOctal(s[i+2]) && isOctal(s[i+3]) && isOctal(s[i+4]) {
			b.WriteByte((s[i+2]-'0')<<6 | (s[i+3]-'0')<<3 | (s[i+4] - '0'))
			i += 5
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }

// safeRelPath reports whether p is safe to join under an endpoint root and to write into
// a NUL-separated --files-from list: relative, no "." / ".." components (a hostile or
// confused listing must not reach outside the root), no NUL (the file-list separator).
// Other control characters are legal filename bytes and pass through.
func safeRelPath(p string) bool {
	if p == "" || p[0] == '/' || strings.ContainsRune(p, 0) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// itemizeWriter extracts the path from each rsync --itemize-changes line as it streams and
// invokes onPath. Writes may split a line, so partial input is buffered until its newline.
type itemizeWriter struct {
	onPath func(string)
	buf    []byte
}

func (w *itemizeWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(w.buf[:i]), "\r")
		w.buf = w.buf[i+1:]
		if path, ok := itemizePath(line); ok {
			w.onPath(path)
		}
	}
	return len(p), nil
}

// itemizePath returns the path from a valid --itemize-changes line (11-char flag field
// then the path), skipping the "./" root, non-itemize chatter, and other entry types
// (symlinks) — progress events, like the manifest, cover regular files and directories.
// A transfer only itemizes a directory it actually creates, so a directory event means
// "dir created"; its trailing slash is stripped to match manifest paths.
func itemizePath(line string) (string, bool) {
	const flagLen = 11
	if len(line) < flagLen+1 {
		return "", false
	}
	flags := line[:flagLen]
	if !strings.ContainsRune("<>ch.*", rune(flags[0])) {
		return "", false
	}
	if flags[1] != 'f' && flags[1] != 'd' {
		return "", false
	}
	path := strings.TrimSpace(line[flagLen:])
	if path == "" || path == "./" {
		return "", false
	}
	if flags[1] == 'd' {
		path = strings.TrimSuffix(path, "/")
	}
	return decodeListPath(path), true
}
