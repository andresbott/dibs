package marker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sampleMarker() *Marker {
	return &Marker{
		CheckedOutBy: "andres@thinkpad",
		Profile:      "photos",
		Host:         "thinkpad",
		Relpaths:     []string{"."},
		CheckedOutAt: time.Date(2026, 7, 2, 10, 48, 0, 0, time.UTC),
		LastSyncAt:   time.Date(2026, 7, 5, 9, 12, 0, 0, time.UTC),
		ToolVersion:  "0.1.0",
	}
}

func TestPathJoinsRemoteRoot(t *testing.T) {
	if got := Path("/mnt/nas/work"); got != filepath.Join("/mnt/nas/work", FileName) {
		t.Errorf("Path = %q", got)
	}
}

func TestReadMissingReturnsNotExists(t *testing.T) {
	m, ok, err := Read(t.TempDir())
	if err != nil || ok || m != nil {
		t.Fatalf("Read(no marker) = %v, %v, %v; want nil,false,nil", m, ok, err)
	}
}

func TestWriteThenReadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, sampleMarker()); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Read(dir)
	if err != nil || !ok {
		t.Fatalf("Read after Write = ok %v err %v", ok, err)
	}
	if got.CheckedOutBy != "andres@thinkpad" || got.Host != "thinkpad" || got.ToolVersion != "0.1.0" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestWriteIsAtomicAndReadable(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, sampleMarker()); err != nil {
		t.Fatal(err)
	}
	// No leftover temp files beside the marker.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != FileName {
			t.Errorf("unexpected leftover file %q", e.Name())
		}
	}
}

func TestRemoveThenReadGone(t *testing.T) {
	dir := t.TempDir()
	_ = Write(dir, sampleMarker())
	if err := Remove(dir); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := Read(dir); ok {
		t.Error("marker still present after Remove")
	}
	// Remove is idempotent.
	if err := Remove(dir); err != nil {
		t.Errorf("second Remove errored: %v", err)
	}
}

func TestOwnedBy(t *testing.T) {
	m := sampleMarker()
	if !m.OwnedBy("andres@thinkpad", "thinkpad") {
		t.Error("should own its own marker")
	}
	if m.OwnedBy("alice@nas", "nas") {
		t.Error("should not own another identity")
	}
	if m.OwnedBy("andres@thinkpad", "laptop") {
		t.Error("same identity on a different host must not own it")
	}
}

func TestWriteMarkerIsGroupReadable(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, sampleMarker()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("marker mode = %o, want 644 (shared lock must be readable by others)", perm)
	}
}
