package tui

import (
	"testing"

	"github.com/andresbott/dibs/internal/lifecycle"
	"github.com/andresbott/dibs/internal/status"
)

// TestPlannedCounts: a Status result classifies into per-verb totals — pushes
// and pulls split by their Modify flag, deletes summed across both sides and
// both targets, and zeroed when allow-deletes is off (the engine skips them).
func TestPlannedCounts(t *testing.T) {
	st := &status.ProfileStatus{CheckedOut: true, HasBaseline: true, Targets: []status.TargetStatus{
		{
			Push:          []status.Change{{Path: "a", Modify: false}, {Path: "b", Modify: true}},
			Pull:          []status.Change{{Path: "c", Modify: false}},
			LocalDeletes:  []string{"d"},
			RemoteDeletes: []string{"e", "f"},
		},
		{Push: []status.Change{{Path: "g", Modify: true}}},
	}}
	c := plannedCounts(st, true)
	if c == nil {
		t.Fatal("expected counts, got nil")
	}
	if c.Add != 2 || c.Modify != 2 || c.Delete != 3 {
		t.Errorf("got add=%d mod=%d del=%d, want add=2 mod=2 del=3", c.Add, c.Modify, c.Delete)
	}
	if c := plannedCounts(st, false); c.Delete != 0 {
		t.Errorf("allow-deletes off: delete total should be 0, got %d", c.Delete)
	}
	if plannedCounts(nil, true) != nil {
		t.Error("no status result must yield nil (indeterminate) totals")
	}
}

// TestKindCountsInc: inc buckets an event kind onto the right counter.
func TestKindCountsInc(t *testing.T) {
	var c kindCounts
	c.inc(lifecycle.EventAdd)
	c.inc(lifecycle.EventAdd)
	c.inc(lifecycle.EventModify)
	c.inc(lifecycle.EventDelete)
	if c.Add != 2 || c.Modify != 1 || c.Delete != 1 || c.total() != 4 {
		t.Errorf("got %+v total=%d, want add=2 mod=1 del=1 total=4", c, c.total())
	}
}

// TestNewSyncProgress: the constructor snapshots totals (or nil) and starts
// the cylon eye moving right.
func TestNewSyncProgress(t *testing.T) {
	p := newSyncProgress(nil, false)
	if p.totals != nil {
		t.Error("no status result: totals must be nil (indeterminate)")
	}
	if p.cylonDir != 1 {
		t.Errorf("cylon should start moving right, got dir=%d", p.cylonDir)
	}
	st := &status.ProfileStatus{Targets: []status.TargetStatus{{Push: []status.Change{{Path: "a"}}}}}
	if p := newSyncProgress(st, false); p.totals == nil || p.totals.Add != 1 {
		t.Errorf("totals should snapshot the status result, got %+v", p.totals)
	}
}
