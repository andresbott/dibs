package tui

import (
	"strings"
	"testing"

	"github.com/andresbott/dibs/internal/ident"
	"github.com/andresbott/dibs/internal/marker"
	"github.com/andresbott/dibs/internal/sanity"
	tea "github.com/charmbracelet/bubbletea"
)

// ownID and ownResult build a checked-out sanity result held by ownID, for
// tests that need the full own-lock action list.
var ownID = ident.Ident{By: "me@host", Host: "host"}

func ownResult() *sanity.Result {
	return &sanity.Result{CheckedOut: true, Marker: &marker.Marker{CheckedOutBy: ownID.By, Host: ownID.Host}}
}

func TestActionCursorMoves(t *testing.T) {
	actions := visibleActions(ownResult(), ownID, "") // Status, Sync, Check-in
	n := len(actions)
	p := newProfileView("alpha")
	if p.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", p.cursor)
	}
	p.moveUp() // clamps at the top
	if p.cursor != 0 {
		t.Fatalf("cursor after up at top = %d, want 0", p.cursor)
	}
	for i := 0; i < n+2; i++ {
		p.moveDown(n) // clamps at the bottom
	}
	if p.cursor != n-1 {
		t.Fatalf("cursor after many downs = %d, want %d", p.cursor, n-1)
	}
	p.moveUp()
	if p.cursor != n-2 {
		t.Fatalf("cursor after up = %d, want %d", p.cursor, n-2)
	}
}

// TestClampCursorOnShrink: when the visible list shrinks (e.g. a check-in swaps
// [Status, Sync, Check-in] for [Checkout]), clampCursor keeps the cursor in
// range.
func TestClampCursorOnShrink(t *testing.T) {
	p := newProfileView("alpha")
	p.cursor = 2 // Check-in, valid while checked out
	p.clampCursor(len(visibleActions(&sanity.Result{CheckedOut: false}, ownID, "")))
	if p.cursor != 0 {
		t.Fatalf("cursor after shrink to [Checkout] = %d, want 0", p.cursor)
	}
}

// TestActionsViewShowsPanels: once Actions is revealed, the unified view shows
// both boxes' content plus the idle Activity toggle help.
func TestActionsViewShowsPanels(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	m.checks["alpha"] = &sanity.Result{CheckedOut: false} // not checked out -> Checkout offered
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	view := m.View()
	// "alpha:" is the Actions-box header; Checkout is the sole action for a
	// not-checked-out profile.
	for _, want := range []string{"Details", "Actions", "Activity", "alpha", "Checkout", "allow deletes"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

// TestCheckedOutActionsOrder: a profile checked out by this machine offers
// Status, Sync, then Check-in (in that order), with the cursor landing on Status.
func TestCheckedOutActionsOrder(t *testing.T) {
	got := visibleActions(ownResult(), ownID, "")
	want := []string{"Status", "Sync", "Check-in"}
	if !equalStrings(got, want) {
		t.Fatalf("checked-out actions = %v, want %v", got, want)
	}
	if got[newProfileView("alpha").cursor] != "Status" {
		t.Fatalf("default cursor should select Status, got %q", got[0])
	}
}
