package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andresbott/dibs/internal/baseline"
	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/internal/lifecycle"
	"github.com/andresbott/dibs/internal/status"
	"github.com/andresbott/dibs/libs/threewayrsync"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// openActions builds a model at a usable size and reveals the first profile's
// action menu (cursor on Status).
func openActions(t *testing.T, cfg *config.Config) model {
	t.Helper()
	m := newModel("/tmp/x.yaml", cfg)
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	return m
}

// runStatusResult runs cmd — the Status action returns a tea.Batch of the status
// compute and the local scan — and returns the statusResultMsg it produces,
// unwrapping the batch's sub-commands.
func runStatusResult(t *testing.T, cmd tea.Cmd) statusResultMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("want a command from Enter on Status")
	}
	if res, ok := cmd().(statusResultMsg); ok {
		return res
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			if res, ok := c().(statusResultMsg); ok {
				return res
			}
		}
	}
	t.Fatal("command should produce a statusResultMsg")
	return statusResultMsg{}
}

// TestActivityIdlePlaceholder: before any action has run, the Activity box
// points at where each action's options live rather than sitting empty.
func TestActivityIdlePlaceholder(t *testing.T) {
	m := openActions(t, testConfig())
	view := m.View()
	for _, want := range []string{"allow deletes", "local wins", "steal the lock"} {
		if !strings.Contains(view, want) {
			t.Errorf("idle Activity should show the options help (missing %q):\n%s", want, view)
		}
	}
}

func TestActivityShowsChecking(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.checking = true
	if !strings.Contains(m.View(), "Checking…") {
		t.Errorf("Activity should show Checking… while a compute is in flight:\n%s", m.View())
	}
}

func TestActivityShowsError(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.err = errors.New("remote root /r/a is not mounted")
	if !strings.Contains(m.View(), "not mounted") {
		t.Errorf("Activity should show the error:\n%s", m.View())
	}
}

func TestActivityShowsStatusResult(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.result = &status.ProfileStatus{CheckedOut: true, HasBaseline: true, Targets: []status.TargetStatus{{
		Push: []status.Change{{Path: "notes.txt", Modify: true}},
	}}}
	view := m.View()
	for _, want := range []string{"(root)", "modify", "→ remote", "notes.txt"} {
		if !strings.Contains(view, want) {
			t.Errorf("Activity result missing %q:\n%s", want, view)
		}
	}
}

// TestActivityStatusNoProfileName: the Status body does not repeat the profile
// name (the Details box already shows it).
func TestActivityStatusNoProfileName(t *testing.T) {
	body := statusBody(status.ProfileStatus{CheckedOut: true, HasBaseline: true, Targets: []status.TargetStatus{{
		Push: []status.Change{{Path: "notes.txt", Modify: true}},
	}}}, 40, "")
	if strings.Contains(body, "alpha") {
		t.Errorf("status body should not contain the profile name:\n%s", body)
	}
}

// TestActivityStatusVerbAndSide: each change reads as a verb and the side it
// lands on — pushes go to remote, pulls to local, deletes name their side, and
// the verb reflects add/modify/delete.
func TestActivityStatusVerbAndSide(t *testing.T) {
	body := statusBody(status.ProfileStatus{CheckedOut: true, HasBaseline: true, Targets: []status.TargetStatus{{
		Push:          []status.Change{{Path: "new.png", Modify: false}},
		Pull:          []status.Change{{Path: "cover.jpg", Modify: true}},
		RemoteDeletes: []string{"gone.png"},
	}}}, 40, "")
	for _, want := range []string{
		"add", "→ remote", "new.png",
		"delete", "gone.png",
		"modify", "→ local", "cover.jpg",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("status body missing %q:\n%s", want, body)
		}
	}
}

// TestActivityStatusLocalDeleteAndConflict: a remote-side deletion renders as a
// local delete, and a two-sided change renders as a conflict on "both".
func TestActivityStatusLocalDeleteAndConflict(t *testing.T) {
	body := statusBody(status.ProfileStatus{CheckedOut: true, HasBaseline: true, Targets: []status.TargetStatus{{
		LocalDeletes: []string{"old.txt"},
		Conflicts:    []string{"clash.txt"},
	}}}, 40, "")
	for _, want := range []string{
		"delete", "→ local", "old.txt",
		"conflict", "both", "clash.txt",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("status body missing %q:\n%s", want, body)
		}
	}
}

// TestActivityStatusInSyncPerTarget: an all-in-sync profile lists each target
// with its own "no changes" line rather than a single global summary.
func TestActivityStatusInSyncPerTarget(t *testing.T) {
	body := statusBody(status.ProfileStatus{CheckedOut: true, HasBaseline: true, Targets: []status.TargetStatus{
		{Subpath: "albums"},
		{Subpath: "stems"},
	}}, 40, "")
	if c := strings.Count(body, "no changes"); c != 2 {
		t.Errorf("want a 'no changes' line per in-sync target, got %d:\n%s", c, body)
	}
	for _, want := range []string{"albums", "stems", "───"} {
		if !strings.Contains(body, want) {
			t.Errorf("in-sync body missing %q:\n%s", want, body)
		}
	}
}

// TestActivityStatusDivider: multiple targets are separated by a divider line,
// but no divider trails the last target.
func TestActivityStatusDivider(t *testing.T) {
	body := statusBody(status.ProfileStatus{CheckedOut: true, HasBaseline: true, Targets: []status.TargetStatus{
		{Subpath: "a", Push: []status.Change{{Path: "x"}}},
		{Subpath: "b", Push: []status.Change{{Path: "y"}}},
	}}, 40, "")
	if n := strings.Count(body, "───"); n < 1 {
		t.Errorf("two targets should be divided by a rule:\n%s", body)
	}
	if strings.HasSuffix(strings.TrimRight(body, " "), "───") {
		t.Errorf("no divider should trail the last target:\n%s", body)
	}
}

// TestActivityStatusNoBaseline: checked out without a local baseline is called
// out rather than shown as a change list.
func TestActivityStatusNoBaseline(t *testing.T) {
	body := statusBody(status.ProfileStatus{CheckedOut: true, HasBaseline: false}, 40, "")
	if !strings.Contains(body, "no local baseline") {
		t.Errorf("want a no-baseline note, got %q", body)
	}
}

// TestStatusClearsStaleActionReport: after a mutating action leaves a report on
// the profile ("sync: 0 pulled"), running Status drops that report so the fresh
// status result is shown instead of the stale action outcome.
func TestStatusClearsStaleActionReport(t *testing.T) {
	cfg := mountedConfig(t)
	saveEmptyBaseline(t, cfg) // empty trees + empty baseline: in sync
	m := openActions(t, cfg)
	m.checks["alpha"] = ownLock(m)
	m.profile.actionReport = &lifecycle.Report{Action: "sync"} // as if a sync just finished
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})        // Enter on Status
	m = nm.(model)
	if m.profile.actionReport != nil {
		t.Fatal("running Status should clear the stale action report")
	}
	m = update(t, m, runStatusResult(t, cmd))
	view := m.View()
	if strings.Contains(view, "pulled") {
		t.Errorf("Activity should show the status result, not the stale action report:\n%s", view)
	}
	if !strings.Contains(view, "no changes") {
		t.Errorf("in-sync Status should show 'no changes':\n%s", view)
	}
}

// TestActivityStatusFitsWidth: a very long change path is clipped, so the view
// never overflows the terminal width.
func TestActivityStatusFitsWidth(t *testing.T) {
	long := strings.Repeat("very-long-path-segment/", 12) + "file.txt"
	for _, w := range []int{80, 100, 120} {
		m := newModel("/tmp/x.yaml", testConfig())
		m = update(t, m, tea.WindowSizeMsg{Width: w, Height: 30})
		m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		m.profile.result = &status.ProfileStatus{CheckedOut: true, HasBaseline: true, Targets: []status.TargetStatus{{
			Push: []status.Change{{Path: long, Modify: false}},
		}}}
		if got := lipgloss.Width(m.View()); got > w {
			t.Errorf("width=%d: view renders %d cols, overflow %d", w, got, got-w)
		}
	}
}

// manyChangesResult builds a checked-out status with n push changes, enough to
// overflow the Activity pane so scrolling applies.
func manyChangesResult(n int) *status.ProfileStatus {
	changes := make([]status.Change, n)
	for i := range changes {
		changes[i] = status.Change{Path: fmt.Sprintf("file%02d.txt", i), Modify: false}
	}
	return &status.ProfileStatus{CheckedOut: true, HasBaseline: true, Targets: []status.TargetStatus{{Push: changes}}}
}

// TestActivityStatusScrolls: a change list taller than the pane hides its tail.
// Scrolling is strictly Tab-gated — the keys do nothing in the Actions pane;
// only once Tab focuses the Activity panel does PgDn reveal the tail and PgUp
// return to the top.
func TestActivityStatusScrolls(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.result = manyChangesResult(40)

	if v := m.View(); !strings.Contains(v, "file00.txt") || strings.Contains(v, "file39.txt") {
		t.Fatalf("initial view should show the head, not the tail:\n%s", v)
	}
	// Actions pane: the scroll keys are inert and the footer shows no scroll hint.
	if strings.Contains(m.View(), "Scroll") {
		t.Errorf("actions-pane footer should not show a scroll hint:\n%s", m.View())
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.profile.statusScroll != 0 {
		t.Errorf("PgDn in the actions pane must not scroll, got scroll=%d", m.profile.statusScroll)
	}

	// Tab focuses the Activity panel; now the footer offers the scroll keys.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if !strings.Contains(m.View(), "Scroll") {
		t.Errorf("activity-pane footer should offer the scroll hint:\n%s", m.View())
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	if v := m.View(); !strings.Contains(v, "file39.txt") {
		t.Errorf("after paging down the tail should be visible:\n%s", v)
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.profile.statusScroll != 0 {
		t.Errorf("paging up should return to the top, got scroll=%d", m.profile.statusScroll)
	}
	if v := m.View(); !strings.Contains(v, "file00.txt") {
		t.Errorf("back at the top the head should be visible:\n%s", v)
	}
}

// TestActivityShiftArrowsFastScroll: with the Activity panel focused,
// shift+↓/↑ jump ten lines at a time instead of one.
func TestActivityShiftArrowsFastScroll(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.result = manyChangesResult(60)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // focus Activity

	m = update(t, m, tea.KeyMsg{Type: tea.KeyShiftDown})
	if m.profile.statusScroll != 10 {
		t.Errorf("shift+Down should scroll ten lines, got %d", m.profile.statusScroll)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.profile.statusScroll != 11 {
		t.Errorf("Down after shift+Down should be at 11, got %d", m.profile.statusScroll)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyShiftUp})
	if m.profile.statusScroll != 1 {
		t.Errorf("shift+Up should scroll back ten lines, got %d", m.profile.statusScroll)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyShiftUp})
	if m.profile.statusScroll != 0 {
		t.Errorf("shift+Up should clamp at the top, got %d", m.profile.statusScroll)
	}
}

// mixedStatusResult builds a status with three operation groups: add → remote,
// modify → local, and delete → remote.
func mixedStatusResult() *status.ProfileStatus {
	return &status.ProfileStatus{CheckedOut: true, HasBaseline: true, Targets: []status.TargetStatus{{
		Push:          []status.Change{{Path: "new.png"}},
		Pull:          []status.Change{{Path: "cover.jpg", Modify: true}},
		RemoteDeletes: []string{"gone.png"},
	}}}
}

// TestActivityFilterCyclesOperations: with the Activity panel focused, → steps
// the view All → add → remote → modify → local → delete → remote → All, each
// step showing only that group's rows plus a filter header.
func TestActivityFilterCyclesOperations(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.result = mixedStatusResult()
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // focus Activity

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	v := m.View()
	if !strings.Contains(v, "filter:") || !strings.Contains(v, "new.png") {
		t.Fatalf("first → should filter to add → remote:\n%s", v)
	}
	if strings.Contains(v, "cover.jpg") || strings.Contains(v, "gone.png") {
		t.Errorf("add filter should hide the other groups:\n%s", v)
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if v := m.View(); !strings.Contains(v, "cover.jpg") || strings.Contains(v, "new.png") {
		t.Errorf("second → should filter to modify → local:\n%s", v)
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if v := m.View(); !strings.Contains(v, "gone.png") || strings.Contains(v, "cover.jpg") {
		t.Errorf("third → should filter to delete → remote:\n%s", v)
	}

	// One more wraps back to All: every group visible, no filter header.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	v = m.View()
	for _, want := range []string{"new.png", "cover.jpg", "gone.png"} {
		if !strings.Contains(v, want) {
			t.Errorf("wrapping to All should show every group (missing %q):\n%s", want, v)
		}
	}
	if strings.Contains(v, "filter:") {
		t.Errorf("All should show no filter header:\n%s", v)
	}
}

// TestActivityFilterCyclesBackward: ← cycles the other way, so from All it
// lands on the last group.
func TestActivityFilterCyclesBackward(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.result = mixedStatusResult()
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})

	m = update(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if v := m.View(); !strings.Contains(v, "gone.png") || strings.Contains(v, "new.png") {
		t.Errorf("← from All should land on the last group (delete → remote):\n%s", v)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if v := m.View(); !strings.Contains(v, "cover.jpg") || strings.Contains(v, "gone.png") {
		t.Errorf("second ← should land on modify → local:\n%s", v)
	}
}

// TestActivityFilterResetsScroll: narrowing the list resets the scroll so the
// filtered view starts at the top.
func TestActivityFilterResetsScroll(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.result = manyChangesResult(40)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.profile.statusScroll == 0 {
		t.Fatal("PgDn should have scrolled")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if m.profile.statusScroll != 0 {
		t.Errorf("changing the filter should reset the scroll, got %d", m.profile.statusScroll)
	}
}

// TestActivityFilterInertInActionsPane: like scrolling, the ←→ filter is
// Tab-gated — in the Actions pane the keys leave the view unfiltered.
func TestActivityFilterInertInActionsPane(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.result = mixedStatusResult()
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if m.profile.opFilter != "" {
		t.Errorf("→ in the actions pane must not filter, got %q", m.profile.opFilter)
	}
	if !strings.Contains(m.View(), "gone.png") {
		t.Errorf("actions-pane view should stay unfiltered:\n%s", m.View())
	}
}

// TestActivityFilterAppliesToSyncStream: the filter also narrows the applied
// change list of an in-flight sync, matching the request that navigation work
// in the sync view too.
func TestActivityFilterAppliesToSyncStream(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.acting = true
	m.pane = paneActivity
	m.profile.applied = []lifecycle.Event{
		{Kind: lifecycle.EventAdd, Side: lifecycle.SideRemote, Path: "new.png"},
		{Kind: lifecycle.EventDelete, Side: lifecycle.SideLocal, Path: "old.txt"},
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	v := m.View()
	if !strings.Contains(v, "new.png") || strings.Contains(v, "old.txt") {
		t.Errorf("→ during a sync should filter the applied list to add → remote:\n%s", v)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if v := m.View(); !strings.Contains(v, "old.txt") || strings.Contains(v, "new.png") {
		t.Errorf("second → should filter to delete → local:\n%s", v)
	}
}

// TestNewRunClearsFilter: launching a fresh Status clears a leftover filter so
// the new result always opens unfiltered.
func TestNewRunClearsFilter(t *testing.T) {
	m := openActions(t, testConfig())
	m.checks["alpha"] = ownLock(m) // cursor sits on Status
	m.profile.result = mixedStatusResult()
	m.profile.opFilter = opKey("add", "remote")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // Enter on Status
	if m.profile.opFilter != "" {
		t.Errorf("a fresh Status run should clear the filter, got %q", m.profile.opFilter)
	}
}

// TestTabTogglesActivityPane: Tab moves focus Actions → Activity and back, and
// the actions view always opens focused on the actions pane.
func TestTabTogglesActivityPane(t *testing.T) {
	m := openActions(t, testConfig())
	if m.pane != paneActions {
		t.Fatalf("actions view should open on the actions pane, got %d", m.pane)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.pane != paneActivity {
		t.Errorf("Tab should focus the activity pane, got %d", m.pane)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.pane != paneActions {
		t.Errorf("Tab again should return to the actions pane, got %d", m.pane)
	}
}

// TestActivityPaneArrowsScroll: once the Activity panel is focused, the arrow
// keys scroll it line by line and leave the action cursor untouched.
func TestActivityPaneArrowsScroll(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.result = manyChangesResult(40) // taller than the pane
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	cursor := m.profile.cursor

	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.profile.statusScroll != 1 {
		t.Errorf("Down should scroll one line, got %d", m.profile.statusScroll)
	}
	if m.profile.cursor != cursor {
		t.Errorf("scrolling must not move the action cursor: got %d, want %d", m.profile.cursor, cursor)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.profile.statusScroll != 0 {
		t.Errorf("Up should scroll back to the top, got %d", m.profile.statusScroll)
	}
}

// TestActionsPaneArrowsMoveCursor: in the actions pane the arrow keys move the
// action cursor and never scroll the Activity panel, even when it overflows.
func TestActionsPaneArrowsMoveCursor(t *testing.T) {
	m := openActions(t, testConfig())
	m.checks[m.profile.name] = ownLock(m) // Status/Sync/Check-in
	m.profile.result = manyChangesResult(40)                    // scrollable, to prove it doesn't

	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.profile.cursor != 1 {
		t.Errorf("Down should move the action cursor to 1, got %d", m.profile.cursor)
	}
	if m.profile.statusScroll != 0 {
		t.Errorf("Down in the actions pane must not scroll, got %d", m.profile.statusScroll)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.profile.cursor != 0 {
		t.Errorf("Up should move the action cursor back to 0, got %d", m.profile.cursor)
	}
}

// TestEscFromActivityReturnsToList: Esc always leaves the profile for the list,
// even when the Activity panel holds focus — there is no layered back-out.
func TestEscFromActivityReturnsToList(t *testing.T) {
	m := openActions(t, testConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // focus Activity
	if m.pane != paneActivity {
		t.Fatalf("Tab should focus the activity pane, got %d", m.pane)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.sub != subList {
		t.Errorf("Esc from the activity pane should return to the profile list, got sub=%d", m.sub)
	}
}

// TestOpenProfileResetsPane: opening a profile always focuses the actions pane,
// so a stale activity focus (e.g. left over after a check-in returned to the
// list) never leaks into the next profile.
func TestOpenProfileResetsPane(t *testing.T) {
	m := openActions(t, testConfig())
	m.sub = subList       // as a completed check-in leaves it,
	m.pane = paneActivity // still focused on the activity pane
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.sub != subActions {
		t.Fatalf("Enter should reopen a profile, got sub=%d", m.sub)
	}
	if m.pane != paneActions {
		t.Errorf("opening a profile should reset focus to the actions pane, got %d", m.pane)
	}
}

// mountedConfig builds a single-profile "alpha" config whose roots are real,
// existing temp directories with a checkout marker on the remote, and points
// DIBS_STATE at a temp state dir so status.Compute resolves its baseline
// there.
func mountedConfig(t *testing.T) *config.Config {
	t.Helper()
	requireRsync(t) // status.Compute enumerates via the real engine
	local := filepath.Join(t.TempDir(), "local")
	remote := filepath.Join(t.TempDir(), "remote")
	for _, d := range []string{local, remote} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(remote, ".dibs.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DIBS_STATE", t.TempDir())
	return &config.Config{Profiles: map[string]config.Profile{
		"alpha": {LocalRoot: local, RemoteRoot: remote},
	}}
}

// saveEmptyBaseline records an empty base manifest for the alpha profile, the
// state a fresh checkout leaves behind.
func saveEmptyBaseline(t *testing.T, cfg *config.Config) {
	t.Helper()
	if err := baseline.Save(&baseline.State{Profile: "alpha", Relpaths: []string{"."}, Files: threewayrsync.Manifest{}}); err != nil {
		t.Fatal(err)
	}
}

// TestStatusEnterRunsCompute: Enter on Status marks the profile checking and
// returns a command; running it yields a statusResultMsg whose result renders.
func TestStatusEnterRunsCompute(t *testing.T) {
	cfg := mountedConfig(t)
	saveEmptyBaseline(t, cfg)
	// A brand-new local file is a push the three-way plan surfaces.
	if err := os.WriteFile(filepath.Join(cfg.Profiles["alpha"].LocalRoot, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := openActions(t, cfg)
	m.checks["alpha"] = ownLock(m) // Status is offered only when checked out
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(model)
	if !m.profile.checking {
		t.Fatal("want profile.checking after Enter on Status")
	}
	res := runStatusResult(t, cmd)
	if res.err != nil {
		t.Fatalf("unexpected compute error: %v", res.err)
	}
	m = update(t, m, res)
	if m.profile.checking {
		t.Fatal("checking should clear once the result arrives")
	}
	if !strings.Contains(m.View(), "notes.txt") {
		t.Errorf("Activity should show the computed change:\n%s", m.View())
	}
}

// TestStatusEnterScansLocalTree: Enter on Status also kicks off a local file
// scan (marking the profile scanning) and, once the result arrives, populates
// fileStats so the Details box shows the folder/file/size summary.
func TestStatusEnterScansLocalTree(t *testing.T) {
	cfg := mountedConfig(t)
	// Give the local root some content so the scan has something to count.
	local := cfg.Profiles["alpha"].LocalRoot
	if err := os.MkdirAll(filepath.Join(local, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "sub", "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	saveEmptyBaseline(t, cfg) // baseline now matches local; status is in sync
	m := openActions(t, cfg)
	m.checks["alpha"] = ownLock(m)
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(model)
	if !m.profile.scanning {
		t.Fatal("want profile.scanning after Enter on Status")
	}
	res := runLocalStatResult(t, cmd)
	if res.err != nil {
		t.Fatalf("unexpected scan error: %v", res.err)
	}
	m = update(t, m, res)
	if m.profile.scanning {
		t.Fatal("scanning should clear once the scan result arrives")
	}
	if m.profile.fileStats == nil || m.profile.fileStats.Files != 1 {
		t.Fatalf("want fileStats with 1 file, got %+v", m.profile.fileStats)
	}
	if !strings.Contains(m.View(), "Contents") || !strings.Contains(m.View(), "Files") {
		t.Errorf("Details should show the contents summary:\n%s", m.View())
	}
}

// TestStatusEnterRefreshesSanity: Enter on Status also re-runs the sanity
// check, so the Details existence marks reflect the current on-disk state —
// e.g. a local root created after the profile was opened flips ✗ to ✓.
func TestStatusEnterRefreshesSanity(t *testing.T) {
	cfg := mountedConfig(t)
	saveEmptyBaseline(t, cfg)
	m := openActions(t, cfg)
	// Stale cache: as if the local root did not exist when the profile opened.
	stale := ownLock(m)
	stale.LocalRoot = false
	stale.RemoteRoot = true
	m.checks["alpha"] = stale
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // Enter on Status
	m = nm.(model)
	res := runSanityResult(t, cmd)
	m = update(t, m, res)
	if !m.checks["alpha"].LocalRoot {
		t.Fatal("Status should refresh the sanity check: LocalRoot exists on disk but the cached mark stayed false")
	}
}

// runSanityResult runs the Status batch command and returns the
// sanityResultMsg it produces.
func runSanityResult(t *testing.T, cmd tea.Cmd) sanityResultMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("want a command from Enter on Status")
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			if res, ok := c().(sanityResultMsg); ok {
				return res
			}
		}
	}
	t.Fatal("command should produce a sanityResultMsg")
	return sanityResultMsg{}
}

// runLocalStatResult runs the Status batch command and returns the
// localStatResultMsg it produces.
func runLocalStatResult(t *testing.T, cmd tea.Cmd) localStatResultMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("want a command from Enter on Status")
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			if res, ok := c().(localStatResultMsg); ok {
				return res
			}
		}
	}
	t.Fatal("command should produce a localStatResultMsg")
	return localStatResultMsg{}
}

// TestStatusEnterSurfacesError: when the remote root is not mounted, Compute
// errors and the Activity box shows that error.
func TestStatusEnterSurfacesError(t *testing.T) {
	m := openActions(t, testConfig())                    // testConfig roots do not exist on disk
	m.checks["alpha"] = ownLock(m) // Status is offered only when checked out
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(model)
	res := runStatusResult(t, cmd)
	if res.err == nil {
		t.Fatal("want an error for an unmounted remote root")
	}
	m = update(t, m, res)
	if m.profile.err == nil {
		t.Fatal("want profile.err set after an errored compute")
	}
	if !strings.Contains(m.View(), "not mounted") {
		t.Errorf("Activity should show the mount error:\n%s", m.View())
	}
}

// TestStaleStatusResultIgnored: a result for a profile the user has left is
// dropped rather than applied to the current profile.
func TestStaleStatusResultIgnored(t *testing.T) {
	m := openActions(t, testConfig())
	stale := statusResultMsg{name: "some-other-profile", err: errors.New("boom")}
	m = update(t, m, stale)
	if m.profile.err != nil {
		t.Fatal("a result for another profile must be ignored")
	}
}

func TestActivityShowsNotCheckedOut(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.result = &status.ProfileStatus{CheckedOut: false}
	if !strings.Contains(m.View(), "not checked out") {
		t.Errorf("Activity should show 'not checked out':\n%s", m.View())
	}
}
