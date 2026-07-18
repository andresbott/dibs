package localstat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andresbott/netcheckout/internal/config"
	"github.com/andresbott/netcheckout/internal/marker"
)

// writeFile writes content to root/rel, creating parent dirs.
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanCountsDirsFilesBytesAndSkipsMarkerAndSymlinks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "hello")         // 5 bytes
	writeFile(t, root, "sub/b.txt", "world!")    // 6 bytes
	writeFile(t, root, "sub/deep/c.txt", "abcd") // 4 bytes
	// Marker at the root must be excluded from the file count and bytes.
	writeFile(t, root, marker.FileName, "ignored")
	// A symlink must be skipped (non-regular).
	if err := os.Symlink(filepath.Join(root, "a.txt"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	got, err := Scan(config.Profile{LocalRoot: root, RemoteRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	// Folders under root: sub, sub/deep -> 2. Root itself is not counted.
	if got.Dirs != 2 {
		t.Errorf("Dirs = %d; want 2", got.Dirs)
	}
	if got.Files != 3 {
		t.Errorf("Files = %d; want 3", got.Files)
	}
	if got.Bytes != 15 {
		t.Errorf("Bytes = %d; want 15", got.Bytes)
	}
}

func TestScanHonorsSubpaths(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/a.txt", "aa")       // 2 bytes, in scope
	writeFile(t, root, "docs/nested/b.txt", "b") // 1 byte, in scope
	writeFile(t, root, "other/c.txt", "cccc")    // out of scope, must not count

	got, err := Scan(config.Profile{LocalRoot: root, RemoteRoot: root, Subpaths: []string{"docs"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Files != 2 || got.Bytes != 3 {
		t.Errorf("scoped scan = %+v; want Files 2 Bytes 3", got)
	}
	if got.Dirs != 1 { // docs/nested; the docs base itself is not counted
		t.Errorf("Dirs = %d; want 1", got.Dirs)
	}
}

func TestScanMissingLocalRootIsEmptyNotError(t *testing.T) {
	got, err := Scan(config.Profile{LocalRoot: filepath.Join(t.TempDir(), "nope"), RemoteRoot: "/x"})
	if err != nil {
		t.Fatalf("Scan(missing) err = %v; want nil", err)
	}
	if (got != Stats{}) {
		t.Errorf("Scan(missing) = %+v; want zero", got)
	}
}
