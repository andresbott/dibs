package sanity

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/internal/marker"
)

func TestCheckRootsExistence(t *testing.T) {
	local, remote := t.TempDir(), t.TempDir()
	r := Check(config.Profile{LocalRoot: local, RemoteRoot: remote}, "")
	if !r.LocalRoot || !r.RemoteRoot {
		t.Fatalf("both roots exist, got %+v", r)
	}
	r = Check(config.Profile{
		LocalRoot:  filepath.Join(local, "nope"),
		RemoteRoot: filepath.Join(remote, "nope"),
	}, "")
	if r.LocalRoot || r.RemoteRoot {
		t.Fatalf("missing roots should be false, got %+v", r)
	}
}

func TestCheckRemoteRootMustBeDir(t *testing.T) {
	remote := t.TempDir()
	file := filepath.Join(remote, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Check(config.Profile{LocalRoot: t.TempDir(), RemoteRoot: file}, "").RemoteRoot {
		t.Error("RemoteRoot should be false for a regular file")
	}
}

func TestCheckMarkerAtRemoteRoot(t *testing.T) {
	local, remote := t.TempDir(), t.TempDir()
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	if Check(p, "").CheckedOut {
		t.Error("CheckedOut should be false with no marker")
	}
	if err := os.WriteFile(filepath.Join(remote, marker.FileName), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Check(p, "").CheckedOut {
		t.Error("CheckedOut should be true with a root marker")
	}
}

// TestCheckMarkerHolder: a readable marker is carried in the result so callers
// can tell an own-machine lock from a foreign one; an unparseable marker keeps
// CheckedOut true with a nil Marker (read as foreign, the safe direction).
func TestCheckMarkerHolder(t *testing.T) {
	local, remote := t.TempDir(), t.TempDir()
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	if err := marker.Write(remote, &marker.Marker{CheckedOutBy: "alice@a", Host: "hostA", Profile: "work"}); err != nil {
		t.Fatal(err)
	}
	r := Check(p, "")
	if r.Marker == nil || r.Marker.CheckedOutBy != "alice@a" || r.Marker.Host != "hostA" {
		t.Fatalf("result should carry the marker holder, got %+v", r.Marker)
	}

	if err := os.WriteFile(filepath.Join(remote, marker.FileName), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	r = Check(p, "")
	if !r.CheckedOut {
		t.Error("an unparseable marker should still read as checked out")
	}
	if r.Marker != nil {
		t.Errorf("an unparseable marker should carry no holder, got %+v", r.Marker)
	}
}

func TestCheckSubpathsExistOnRemote(t *testing.T) {
	local, remote := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(remote, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	// "b" is intentionally absent.
	r := Check(config.Profile{LocalRoot: local, RemoteRoot: remote, Subpaths: []string{"a", "b"}}, "")
	want := []Subpath{{Path: "a", Exists: true}, {Path: "b", Exists: false}}
	if len(r.Subpaths) != len(want) {
		t.Fatalf("Subpaths = %+v, want %+v", r.Subpaths, want)
	}
	for i := range want {
		if r.Subpaths[i] != want[i] {
			t.Errorf("Subpaths[%d] = %+v, want %+v", i, r.Subpaths[i], want[i])
		}
	}
}

func TestCheckWholeRootHasNoSubpaths(t *testing.T) {
	if r := Check(config.Profile{LocalRoot: t.TempDir(), RemoteRoot: t.TempDir()}, ""); len(r.Subpaths) != 0 {
		t.Errorf("whole-root profile should have no Subpaths, got %+v", r.Subpaths)
	}
}

func TestCheckInvalidSubpathReturnsRootsOnly(t *testing.T) {
	r := Check(config.Profile{LocalRoot: t.TempDir(), RemoteRoot: t.TempDir(), Subpaths: []string{"../escape"}}, "")
	if !r.LocalRoot || !r.RemoteRoot {
		t.Errorf("roots should still be reported on an invalid subpath: %+v", r)
	}
	if len(r.Subpaths) != 0 {
		t.Errorf("Subpaths should be empty when Targets() errors, got %+v", r.Subpaths)
	}
}

func TestUnlistedLocal(t *testing.T) {
	tests := []struct {
		name     string
		subpaths []string
		// files to create under local_root, relative slash paths
		files []string
		// dirs to create under local_root (for empty-dir cases)
		dirs []string
		// symlinks to create: path is where the symlink goes, target is what it points to
		symlinks []struct{ path, target string }
		want     []string
	}{
		{
			name:     "no subpaths means whole root, nothing flagged",
			subpaths: nil,
			files:    []string{"a/f", "b/g", "top.txt"},
			want:     nil,
		},
		{
			name:     "flat uncovered dir with a file is flagged",
			subpaths: []string{"a", "b"},
			files:    []string{"a/f", "b/g", "c/x.txt"},
			want:     []string{"c"},
		},
		{
			name:     "nested sibling of a covered subpath is flagged, ancestor is not",
			subpaths: []string{"a/2024"},
			files:    []string{"a/2024/f", "a/2025/g"},
			want:     []string{"a/2025"},
		},
		{
			name:     "loose file at root is flagged individually",
			subpaths: []string{"a"},
			files:    []string{"a/f", "top.txt"},
			want:     []string{"top.txt"},
		},
		{
			name:     "empty uncovered dir is not flagged",
			subpaths: []string{"a"},
			files:    []string{"a/f"},
			dirs:     []string{"b"},
			want:     nil,
		},
		{
			name:     "only covered content yields nothing",
			subpaths: []string{"a", "b"},
			files:    []string{"a/f", "b/deep/g"},
			want:     nil,
		},
		{
			name:     "shallowest dir is reported, not the nested file",
			subpaths: []string{"a"},
			files:    []string{"a/f", "c/deep/nested/f"},
			want:     []string{"c"},
		},
		{
			name:     "prefix collision: ab is not covered by subpath a",
			subpaths: []string{"a"},
			files:    []string{"a/f", "ab/x"},
			want:     []string{"ab"},
		},
		{
			name:     "symlink does not make a dir count as containing files",
			subpaths: []string{"a"},
			files:    []string{"a/f"},
			// a symlink is created in setup below (see Item 1b); "b" holds only that symlink
			symlinks: []struct{ path, target string }{{path: "b/link", target: "a/f"}},
			want:     nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			local := t.TempDir()
			for _, d := range tc.dirs {
				if err := os.MkdirAll(filepath.Join(local, filepath.FromSlash(d)), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for _, f := range tc.files {
				full := filepath.Join(local, filepath.FromSlash(f))
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for _, sl := range tc.symlinks {
				full := filepath.Join(local, filepath.FromSlash(sl.path))
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(sl.target, full); err != nil {
					if os.IsExist(err) || os.IsPermission(err) {
						// Skip on platforms without symlink support or permission issues
						t.Skip("symlink creation not supported on this platform")
					}
					t.Fatal(err)
				}
			}
			got, err := UnlistedLocal(config.Profile{LocalRoot: local, Subpaths: tc.subpaths})
			if err != nil {
				t.Fatalf("UnlistedLocal error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("UnlistedLocal = %v, want %v", got, tc.want)
			}
		})
	}
}

// A .DS_Store dropped by the file manager — loose at the root or as the sole
// content of an uncovered dir — must not trip the unlisted-local guard when the
// profile ignores it: sync will never touch it, so it can't strand work.
func TestUnlistedLocalSkipsIgnored(t *testing.T) {
	local := t.TempDir()
	for _, f := range []string{"a/f", ".DS_Store", "junk/.DS_Store"} {
		full := filepath.Join(local, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p := config.Profile{LocalRoot: local, Subpaths: []string{"a"}, Ignore: []string{".DS_Store"}}
	got, err := UnlistedLocal(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("UnlistedLocal = %v, want nil (ignored files must not be flagged)", got)
	}
	// Without the ignore pattern the same tree IS flagged — the ignore list is
	// what makes the difference.
	p.Ignore = nil
	got, err = UnlistedLocal(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{".DS_Store", "junk"}; !reflect.DeepEqual(got, want) {
		t.Errorf("UnlistedLocal = %v, want %v", got, want)
	}
}

func TestUnlistedLocalMissingRoot(t *testing.T) {
	got, err := UnlistedLocal(config.Profile{
		LocalRoot: filepath.Join(t.TempDir(), "nope"),
		Subpaths:  []string{"a"},
	})
	if err != nil || got != nil {
		t.Fatalf("missing local root should be (nil, nil), got (%v, %v)", got, err)
	}
}

func TestCheckPopulatesUnlistedLocal(t *testing.T) {
	local, remote := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "top.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Check(config.Profile{LocalRoot: local, RemoteRoot: remote, Subpaths: []string{"a"}}, "")
	if !reflect.DeepEqual(r.UnlistedLocal, []string{"top.txt"}) {
		t.Errorf("Check.UnlistedLocal = %v, want [top.txt]", r.UnlistedLocal)
	}
}
