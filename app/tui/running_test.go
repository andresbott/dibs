package tui

import (
	"strings"
	"testing"

	"github.com/andresbott/dibs/internal/lifecycle"
	"github.com/andresbott/dibs/internal/status"
	tea "github.com/charmbracelet/bubbletea"
)

// TestTabIsInertWhileRunning: while an action runs the view is locked to the
// Activity panel, so Tab (and Shift+Tab) must not move focus back to Actions.
func TestTabIsInertWhileRunning(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.acting = true
	m.pane = paneActivity
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.pane != paneActivity {
		t.Errorf("Tab while running must not move focus, got pane=%d", m.pane)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.pane != paneActivity {
		t.Errorf("Shift+Tab while running must not move focus, got pane=%d", m.pane)
	}
}

// TestEnterCannotLaunchWhileRunning: even if focus somehow sat on the Actions
// pane, Enter must not open another action's confirm dialog mid-run.
func TestEnterCannotLaunchWhileRunning(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.acting = true
	m.pane = paneActions // defensive: should not happen, but must still be safe
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMain {
		t.Errorf("Enter while running must not open a dialog, got mode=%d", m.mode)
	}
	if m.profile.checking {
		t.Error("Enter while running must not start a Status compute")
	}
}

// TestQWhileRunningOpensCancelConfirm: q joins Esc as a cancel path during a
// run (while staying unbound when the actions view is idle).
func TestQWhileRunningOpensCancelConfirm(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.acting = true
	m = update(t, m, keyMsg("q"))
	if m.mode != modeConfirm || m.confirmKind != confirmCancel {
		t.Fatalf("q while running should open the cancel confirm; mode=%d kind=%d", m.mode, m.confirmKind)
	}
}

// TestCtrlCWhileRunningOpensCancelConfirm: a hard quit mid-run would orphan the
// live rsync, so Ctrl+C routes through the cancel confirm instead of quitting.
// While idle it keeps quitting immediately.
func TestCtrlCWhileRunningOpensCancelConfirm(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.acting = true
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(model)
	if cmd != nil {
		t.Error("Ctrl+C while running must not quit")
	}
	if m.mode != modeConfirm || m.confirmKind != confirmCancel {
		t.Fatalf("Ctrl+C while running should open the cancel confirm; mode=%d kind=%d", m.mode, m.confirmKind)
	}

	// With the cancel confirm already open, another Ctrl+C is a no-op.
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(model)
	if cmd != nil || m.mode != modeConfirm {
		t.Error("Ctrl+C on the open cancel confirm should do nothing")
	}

	// Idle: Ctrl+C still quits immediately.
	idle := openActions(t, testConfig())
	if _, cmd := idle.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Error("Ctrl+C while idle should quit")
	}
}

// TestLaunchFocusesActivity: confirming a sync (and running Status) hands
// focus to the Activity panel so the run opens in monitoring position.
func TestLaunchFocusesActivity(t *testing.T) {
	m := openActions(t, testConfig())
	m.pane = paneActions
	tm, _ := m.runSelectedAction("Sync")
	m = tm.(model)
	m = update(t, m, keyMsg("y")) // confirm the sync dialog
	if m.pane != paneActivity {
		t.Errorf("a launched sync should focus the Activity panel, got pane=%d", m.pane)
	}

	m2 := openActions(t, testConfig())
	m2.pane = paneActions
	tm, _ = m2.runSelectedAction("Status")
	m2 = tm.(model)
	if m2.pane != paneActivity {
		t.Errorf("a running Status should focus the Activity panel, got pane=%d", m2.pane)
	}
}

// TestCompletionReturnsFocusToActions: when the run's terminal result lands,
// focus moves back to the Actions panel — for a mutating action and for Status.
func TestCompletionReturnsFocusToActions(t *testing.T) {
	m := openActions(t, testConfig()) // opens "alpha"
	m.profile.acting = true
	m.pane = paneActivity
	m.actionSeq = 2
	m = update(t, m, actionResultMsg{name: "alpha", seq: 2, report: lifecycle.Report{Action: "sync"}})
	if m.pane != paneActions {
		t.Errorf("a finished action should return focus to Actions, got pane=%d", m.pane)
	}

	m2 := openActions(t, testConfig())
	m2.profile.checking = true
	m2.pane = paneActivity
	m2.actionSeq = 2
	m2 = update(t, m2, statusResultMsg{name: "alpha", seq: 2, st: status.ProfileStatus{}})
	if m2.pane != paneActions {
		t.Errorf("a finished Status should return focus to Actions, got pane=%d", m2.pane)
	}
}

// TestCancelReturnsFocusToActions: confirming the cancel dialog lands back on
// the profile view with the Actions panel focused.
func TestCancelReturnsFocusToActions(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.acting = true
	m.pane = paneActivity
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = update(t, m, keyMsg("y")) // confirm the stop
	if m.pane != paneActions {
		t.Errorf("a confirmed cancel should return focus to Actions, got pane=%d", m.pane)
	}
}

// TestRunningViewDisablesActions: while a run is in flight the footer drops the
// Run/Select/Tab hints (only Scroll + esc: Cancel remain) and the Actions box
// renders with no cursor marker.
func TestRunningViewDisablesActions(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.acting = true
	m.pane = paneActivity
	m.resize(tea.WindowSizeMsg{Width: 100, Height: 30})
	view := m.View()
	for _, banned := range []string{": Run", ": Select", "tab"} {
		if strings.Contains(view, banned) {
			t.Errorf("running view must not offer %q, got:\n%s", banned, view)
		}
	}
	if !strings.Contains(view, ": Cancel") {
		t.Errorf("running view should hint esc: Cancel, got:\n%s", view)
	}
	if strings.Contains(view, "▸") {
		t.Errorf("Actions box must not show a cursor while running, got:\n%s", view)
	}
}
