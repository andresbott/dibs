package tui

import (
	"fmt"
	"strings"

	"github.com/andresbott/dibs/internal/lifecycle"
	"github.com/andresbott/dibs/internal/status"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

// renderProgressLine is the bottom row while a sync runs: per-verb counters on
// the left, a progress bar filling the middle, and the cancel hint on the
// right. With planned totals (a prior Status run) the counters read done/total
// and the bar fills proportionally; without them the counters count up and the
// bar shows a bouncing eye. A width too narrow for a useful bar degrades to
// the truncated counters + hint.
func renderProgressLine(width int, p *syncProgress) string {
	if width <= 0 {
		width = 80 // matches mainView's pre-resize fallback
	}
	left, right := progressEnds(p)
	barW := progressBarW(width, p)
	if barW < 5 {
		return ansi.Truncate(left+right, width, "")
	}
	return left + "▕" + progressTrack(barW, p) + "▏" + right
}

// progressEnds renders the line's fixed ends: the counters (left) and the
// cancel hint (right), shared by the renderer and the width math.
func progressEnds(p *syncProgress) (left, right string) {
	var counters string
	if p.totals != nil {
		counters = fmt.Sprintf("add %d/%d  mod %d/%d  del %d/%d",
			p.done.Add, p.totals.Add, p.done.Modify, p.totals.Modify, p.done.Delete, p.totals.Delete)
	} else {
		counters = fmt.Sprintf("add %d  mod %d  del %d", p.done.Add, p.done.Modify, p.done.Delete)
	}
	return " " + counters + "  ", "  " + hint("esc", "Cancel")
}

// progressBarW is the bar's inner cell count for a terminal width: what is
// left between the counters, the hint, and the bar's two end caps. The tick
// handler uses it too, so the eye advances over exactly the track that is
// drawn.
func progressBarW(width int, p *syncProgress) int {
	left, right := progressEnds(p)
	return width - lipgloss.Width(left) - lipgloss.Width(right) - 2
}

// progressTrack renders the barW inner cells: a proportional fill when totals
// are known (a zero-total plan is already complete; conflicts resolved
// local-wins can push done past the plan, so the fill clamps), the bouncing
// eye otherwise.
func progressTrack(barW int, p *syncProgress) string {
	if p.totals == nil {
		pos := p.cylonPos
		if pos > barW-1 {
			pos = barW - 1
		}
		if pos < 0 {
			pos = 0
		}
		return strings.Repeat("░", pos) + "█" + strings.Repeat("░", barW-1-pos)
	}
	fill := barW
	if t := p.totals.total(); t > 0 {
		fill = p.done.total() * barW / t
		if fill > barW {
			fill = barW
		}
	}
	return strings.Repeat("█", fill) + strings.Repeat("░", barW-fill)
}

// advance moves the cylon eye one cell along a barW-wide track, bouncing at
// the ends. The position is clamped into the track first so a terminal resize
// can never strand the eye outside the bar.
func (p *syncProgress) advance(barW int) {
	if barW < 2 {
		p.cylonPos = 0
		return
	}
	if p.cylonDir == 0 {
		p.cylonDir = 1
	}
	if p.cylonPos > barW-1 {
		p.cylonPos = barW - 1
	}
	if next := p.cylonPos + p.cylonDir; next < 0 || next > barW-1 {
		p.cylonDir = -p.cylonDir
	}
	p.cylonPos += p.cylonDir
}
