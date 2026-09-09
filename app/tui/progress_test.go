package tui

import (
	"strings"
	"testing"

	"github.com/candy-tools/dibs/internal/lifecycle"
	"github.com/candy-tools/dibs/internal/status"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

// TestRenderProgressLineDeterminate: with totals the counters read done/total,
// the esc hint is present, and the bar fill is proportional to overall done.
func TestRenderProgressLineDeterminate(t *testing.T) {
	p := &syncProgress{totals: &kindCounts{Add: 2, Modify: 1, Delete: 1}, cylonDir: 1}
	p.done = kindCounts{Add: 1, Modify: 1}
	plain := ansi.Strip(renderProgressLine(80, p))
	for _, want := range []string{"add 1/2", "mod 1/1", "del 0/1", "esc: Cancel"} {
		if !strings.Contains(plain, want) {
			t.Errorf("progress line missing %q, got: %s", want, plain)
		}
	}
	barW := progressBarW(80, p)
	if got, want := strings.Count(plain, "█"), barW*2/4; got != want {
		t.Errorf("2 of 4 done: want %d filled cells of %d, got %d", want, barW, got)
	}
}

// TestRenderProgressLineIndeterminate: without totals the counters count up
// (no "/"), the track is capped at the short scanner width, the line still
// spans the full width (padding keeps the hint on the right edge), and the
// eye is a three-cell scanner mid-track that narrows to one cell at the ends.
func TestRenderProgressLineIndeterminate(t *testing.T) {
	p := newSyncProgress(nil, false)
	p.done = kindCounts{Add: 4, Modify: 1}
	p.cylonPos = 3
	line := renderProgressLine(80, p)
	plain := ansi.Strip(line)
	for _, want := range []string{"add 4", "mod 1", "del 0", "esc: Cancel"} {
		if !strings.Contains(plain, want) {
			t.Errorf("progress line missing %q, got: %s", want, plain)
		}
	}
	if strings.Contains(plain, "/") {
		t.Errorf("count-up counters must not show totals, got: %s", plain)
	}
	if got := lipgloss.Width(line); got != 80 {
		t.Errorf("padded line should span the full width, got %d cells", got)
	}
	if got := progressBarW(80, p); got != cylonBarW {
		t.Errorf("indeterminate track should cap at %d cells, got %d", cylonBarW, got)
	}
	if got := strings.Count(plain, "█"); got != 5 {
		t.Errorf("mid-track eye should be core + two wings per side (5 cells), got %d in: %s", got, plain)
	}
	p.cylonPos = 1
	if got := strings.Count(ansi.Strip(renderProgressLine(80, p)), "█"); got != 3 {
		t.Errorf("one cell from the border the eye squashes to 3 cells, got %d", got)
	}
	p.cylonPos = 0
	if got := strings.Count(ansi.Strip(renderProgressLine(80, p)), "█"); got != 1 {
		t.Errorf("at the track border the eye squashes to its core, got %d cells", got)
	}
}

// TestRenderProgressLineZeroTotals: a plan with nothing to do renders a full
// bar instead of dividing by zero.
func TestRenderProgressLineZeroTotals(t *testing.T) {
	p := &syncProgress{totals: &kindCounts{}, cylonDir: 1}
	plain := ansi.Strip(renderProgressLine(80, p))
	if strings.Contains(plain, "░") || !strings.Contains(plain, "█") {
		t.Errorf("zero-total plan should render a full bar, got: %s", plain)
	}
}

// TestRenderProgressLineNarrow: a width too small for a bar degrades to the
// truncated counters + hint without panicking.
func TestRenderProgressLineNarrow(t *testing.T) {
	p := newSyncProgress(nil, false)
	line := renderProgressLine(20, p)
	if w := lipgloss.Width(line); w > 20 {
		t.Errorf("narrow line must fit its width, got %d cells", w)
	}
}

// TestCylonAdvanceBounces: the eye walks to the track's last cell, dwells
// there for cylonEdgeHold ticks, reverses, walks back to 0, dwells, and
// reverses again.
func TestCylonAdvanceBounces(t *testing.T) {
	p := &syncProgress{cylonDir: 1}
	for i := 0; i < 4; i++ {
		p.advance(5)
	}
	if p.cylonPos != 4 || p.cylonDir != 1 {
		t.Fatalf("after 4 steps on a 5-track: pos=%d dir=%d, want pos=4 dir=1", p.cylonPos, p.cylonDir)
	}
	for i := 0; i < cylonEdgeHold; i++ {
		p.advance(5)
		if p.cylonPos != 4 {
			t.Fatalf("dwell tick %d: eye should sit on the end, pos=%d", i, p.cylonPos)
		}
	}
	p.advance(5)
	if p.cylonPos != 3 || p.cylonDir != -1 {
		t.Fatalf("bounce at the right end: pos=%d dir=%d, want pos=3 dir=-1", p.cylonPos, p.cylonDir)
	}
	for i := 0; i < 3; i++ {
		p.advance(5)
	}
	if p.cylonPos != 0 {
		t.Fatalf("walk back: pos=%d, want 0", p.cylonPos)
	}
	for i := 0; i < cylonEdgeHold; i++ {
		p.advance(5)
	}
	p.advance(5)
	if p.cylonPos != 1 || p.cylonDir != 1 {
		t.Fatalf("bounce at the left end: pos=%d dir=%d, want pos=1 dir=1", p.cylonPos, p.cylonDir)
	}
	// A shrunk track clamps the position instead of stranding the eye outside.
	p.cylonPos = 10
	p.advance(5)
	if p.cylonPos > 4 {
		t.Fatalf("resize clamp: pos=%d, want <= 4", p.cylonPos)
	}
}

// launchSync opens the sync dialog for the open profile and confirms it,
// returning the model with the run launched.
func launchSync(t *testing.T, m model) model {
	t.Helper()
	tm, _ := m.runSelectedAction("Sync")
	m = tm.(model)
	return update(t, m, keyMsg("y"))
}

// TestSyncLaunchSnapshotsTotals: a sync launched after a Status run carries
// that result's per-verb totals (determinate bar).
func TestSyncLaunchSnapshotsTotals(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.result = &status.ProfileStatus{CheckedOut: true, HasBaseline: true,
		Targets: []status.TargetStatus{{
			Push: []status.Change{{Path: "a"}, {Path: "b", Modify: true}},
			Pull: []status.Change{{Path: "c"}},
		}}}
	m = launchSync(t, m)
	p := m.profile.progress
	if p == nil || p.totals == nil {
		t.Fatal("sync after a Status run should carry determinate totals")
	}
	if p.totals.Add != 2 || p.totals.Modify != 1 {
		t.Errorf("totals add=%d mod=%d, want add=2 mod=1", p.totals.Add, p.totals.Modify)
	}
}

// TestSyncLaunchWithoutStatusIsIndeterminate: no Status run in memory means
// no totals — the bar starts in cylon mode.
func TestSyncLaunchWithoutStatusIsIndeterminate(t *testing.T) {
	m := openActions(t, testConfig())
	m = launchSync(t, m)
	if m.profile.progress == nil {
		t.Fatal("a launched sync should always track progress")
	}
	if m.profile.progress.totals != nil {
		t.Error("no Status result: totals must be nil (indeterminate)")
	}
}

// TestSyncEventIncrementsProgress: each streamed applied change bumps its
// verb's done counter.
func TestSyncEventIncrementsProgress(t *testing.T) {
	m := openActions(t, testConfig()) // opens "alpha"
	m.profile.acting = true
	m.profile.progress = newSyncProgress(nil, false)
	m.actionSeq = 2
	ch := make(chan tea.Msg, 1)
	m = update(t, m, syncEventMsg{name: "alpha", seq: 2, ch: ch,
		event: lifecycle.Event{Kind: lifecycle.EventAdd, Side: lifecycle.SideRemote, Path: "a"}})
	m = update(t, m, syncEventMsg{name: "alpha", seq: 2, ch: ch,
		event: lifecycle.Event{Kind: lifecycle.EventDelete, Side: lifecycle.SideLocal, Path: "b"}})
	if d := m.profile.progress.done; d.Add != 1 || d.Delete != 1 || d.Modify != 0 {
		t.Errorf("done counts add=%d mod=%d del=%d, want add=1 mod=0 del=1", d.Add, d.Modify, d.Delete)
	}
	// A stale straggler (bumped seq) must not count.
	m = update(t, m, syncEventMsg{name: "alpha", seq: 1, ch: ch,
		event: lifecycle.Event{Kind: lifecycle.EventAdd, Path: "c"}})
	if m.profile.progress.done.Add != 1 {
		t.Error("a stale event must not increment progress")
	}
}

// TestSyncResultClearsProgressAndTotals: when the sync's terminal result
// lands, the progress state AND the stored Status result are cleared, so a
// later sync without a fresh Status falls back to cylon mode.
func TestSyncResultClearsProgressAndTotals(t *testing.T) {
	m := openActions(t, testConfig()) // opens "alpha"
	m.profile.acting = true
	m.profile.progress = newSyncProgress(nil, false)
	m.profile.result = &status.ProfileStatus{CheckedOut: true, HasBaseline: true}
	m.actionSeq = 2
	m = update(t, m, actionResultMsg{name: "alpha", seq: 2, report: lifecycle.Report{Action: "sync"}})
	if m.profile.progress != nil {
		t.Error("a finished sync should clear its progress state")
	}
	if m.profile.result != nil {
		t.Error("a finished sync invalidates the stored Status totals")
	}
}

// TestCancelClearsProgress: confirming the cancel dialog mid-sync clears the
// progress state and the (now possibly stale) Status result.
func TestCancelClearsProgress(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.acting = true
	m.profile.progress = newSyncProgress(nil, false)
	m.profile.result = &status.ProfileStatus{CheckedOut: true, HasBaseline: true}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = update(t, m, keyMsg("y")) // confirm the stop
	if m.profile.progress != nil {
		t.Error("a canceled sync should clear its progress state")
	}
	if m.profile.result != nil {
		t.Error("a canceled sync invalidates the stored Status totals")
	}
}

// TestCylonTickAdvancesAndRearms: while an indeterminate sync runs, each tick
// moves the eye and schedules the next tick; a stale or determinate tick does
// neither.
func TestCylonTickAdvancesAndRearms(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.acting = true
	m.profile.progress = newSyncProgress(nil, false)
	m.actionSeq = 3
	next, cmd := m.Update(cylonTickMsg{seq: 3})
	m = next.(model)
	if m.profile.progress.cylonPos != 1 {
		t.Errorf("tick should advance the eye, pos=%d want 1", m.profile.progress.cylonPos)
	}
	if cmd == nil {
		t.Error("tick should re-arm while the indeterminate sync runs")
	}

	// Stale seq (canceled/superseded run): dropped, no re-arm.
	next, cmd = m.Update(cylonTickMsg{seq: 2})
	m = next.(model)
	if m.profile.progress.cylonPos != 1 || cmd != nil {
		t.Error("a stale tick must neither advance nor re-arm")
	}

	// Determinate run: the bar is proportional, no animation needed.
	m.profile.progress = &syncProgress{totals: &kindCounts{Add: 1}, cylonDir: 1}
	if _, cmd := m.Update(cylonTickMsg{seq: 3}); cmd != nil {
		t.Error("a determinate sync must not re-arm the cylon tick")
	}
}

// TestSyncLaunchStartsCylonTick: launching a sync without Status totals
// returns a batch that includes the first tick (smoke: the launch cmd is
// non-nil; the tick behavior itself is covered above).
func TestSyncLaunchStartsCylonTick(t *testing.T) {
	m := openActions(t, testConfig())
	tm, _ := m.runSelectedAction("Sync")
	m = tm.(model)
	next, cmd := m.Update(keyMsg("y"))
	m = next.(model)
	if cmd == nil {
		t.Fatal("confirming a sync should return the launch command batch")
	}
	if m.profile.progress == nil || m.profile.progress.totals != nil {
		t.Fatal("this launch should be indeterminate")
	}
}

// TestRunningSyncShowsProgressBar: while a sync with progress runs, the
// bottom row is the progress line; other running actions (progress nil) keep
// the plain running footer.
func TestRunningSyncShowsProgressBar(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.acting = true
	m.pane = paneActivity
	m.profile.progress = newSyncProgress(nil, false)
	m.resize(tea.WindowSizeMsg{Width: 100, Height: 30})
	plain := ansi.Strip(m.View())
	for _, want := range []string{"add 0", "mod 0", "del 0", "esc: Cancel"} {
		if !strings.Contains(plain, want) {
			t.Errorf("running sync view missing %q", want)
		}
	}
	if !strings.Contains(plain, "▕") {
		t.Error("running sync view should draw the progress bar")
	}

	m.profile.progress = nil // e.g. a running checkout
	plain = ansi.Strip(m.View())
	if strings.Contains(plain, "▕") {
		t.Error("a run without progress keeps the plain footer")
	}
	if !strings.Contains(plain, ": Cancel") {
		t.Error("the plain running footer should still hint esc: Cancel")
	}
}
