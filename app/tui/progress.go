package tui

import (
	"github.com/andresbott/dibs/internal/lifecycle"
	"github.com/andresbott/dibs/internal/status"
)

// kindCounts tallies changes per verb — the same add/modify/delete vocabulary
// the Activity rows use.
type kindCounts struct{ Add, Modify, Delete int }

// inc buckets one applied change onto its verb's counter.
func (c *kindCounts) inc(k lifecycle.EventKind) {
	switch k {
	case lifecycle.EventAdd:
		c.Add++
	case lifecycle.EventModify:
		c.Modify++
	case lifecycle.EventDelete:
		c.Delete++
	}
}

func (c kindCounts) total() int { return c.Add + c.Modify + c.Delete }

// syncProgress is the live progress of the in-flight sync: the planned totals
// snapshotted from the last Status run (nil when none ran — the bar is then
// indeterminate), the done counts incremented per streamed event, and the
// bouncing-eye position/direction for the indeterminate bar.
type syncProgress struct {
	totals   *kindCounts
	done     kindCounts
	cylonPos int
	cylonDir int
}

// newSyncProgress starts progress tracking for one sync run. st is the last
// Status result held in memory for the profile (nil when Status never ran);
// allowDeletes is the run's "allow deletes" checkbox, which decides whether
// planned deletions are part of the plan at all.
func newSyncProgress(st *status.ProfileStatus, allowDeletes bool) *syncProgress {
	return &syncProgress{totals: plannedCounts(st, allowDeletes), cylonDir: 1}
}

// plannedCounts classifies a Status result into per-verb totals: pushes and
// pulls split by their add/modify flag, deletes from both sides. With
// allow-deletes off the engine skips every planned delete (no delete event
// ever streams), so they are not part of the plan either. A nil result means
// no totals are known.
func plannedCounts(st *status.ProfileStatus, allowDeletes bool) *kindCounts {
	if st == nil {
		return nil
	}
	var c kindCounts
	for _, t := range st.Targets {
		for _, ch := range t.Push {
			c.inc(transferEventKind(ch.Modify))
		}
		for _, ch := range t.Pull {
			c.inc(transferEventKind(ch.Modify))
		}
		if allowDeletes {
			c.Delete += len(t.LocalDeletes) + len(t.RemoteDeletes)
		}
	}
	return &c
}

// transferEventKind maps a planned copy's add/modify flag onto the event-kind
// vocabulary the done counters are keyed by.
func transferEventKind(modify bool) lifecycle.EventKind {
	if modify {
		return lifecycle.EventModify
	}
	return lifecycle.EventAdd
}
