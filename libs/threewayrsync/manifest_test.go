package threewayrsync

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFileStateEqual(t *testing.T) {
	t0 := time.Unix(100, 0)
	a := FileState{Size: 5, ModTime: t0}
	dir := FileState{IsDir: true, ModTime: t0}
	cases := []struct {
		name string
		a, b FileState
		want bool
	}{
		{"identical", a, FileState{Size: 5, ModTime: t0}, true},
		{"size differs", a, FileState{Size: 6, ModTime: t0}, false},
		{"mtime differs", a, FileState{Size: 5, ModTime: time.Unix(101, 0)}, false},
		// Dirs are presence-only: mtime churn must never look like an edit.
		{"dir/dir mtime differs", dir, FileState{IsDir: true, ModTime: time.Unix(999, 0)}, true},
		{"file/dir type change", a, FileState{IsDir: true, Size: 5, ModTime: t0}, false},
		{"dir/file type change", dir, FileState{ModTime: t0}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.a.Equal(c.b); got != c.want {
				t.Errorf("Equal = %v, want %v", got, c.want)
			}
		})
	}
}

// A base written before directory tracking carries no isDir keys; its entries must
// unmarshal as files so the upgrade needs no migration.
func TestFileStateOldJSONUnmarshalsAsFile(t *testing.T) {
	var fs FileState
	if err := json.Unmarshal([]byte(`{"size":5,"mtime":"2026-07-14T09:15:00Z"}`), &fs); err != nil {
		t.Fatal(err)
	}
	if fs.IsDir {
		t.Error("old-format entry must not be a dir")
	}
	// And a dir entry round-trips.
	data, err := json.Marshal(FileState{IsDir: true, ModTime: time.Unix(100, 0)})
	if err != nil {
		t.Fatal(err)
	}
	var back FileState
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if !back.IsDir {
		t.Errorf("dir flag lost in round-trip: %s", data)
	}
}

func TestCountFiles(t *testing.T) {
	m := Manifest{
		"a.txt":   {Size: 1},
		"d":       {IsDir: true},
		"d/b.txt": {Size: 2},
	}
	if got := countFiles(m); got != 2 {
		t.Errorf("countFiles = %d, want 2", got)
	}
}
