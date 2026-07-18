package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/internal/marker"
	"github.com/andresbott/dibs/internal/sanity"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func testConfig() *config.Config {
	return &config.Config{Profiles: map[string]config.Profile{
		"alpha": {LocalRoot: "/l/a", RemoteRoot: "/r/a"},
		"beta":  {LocalRoot: "/l/b", RemoteRoot: "/r/b"},
	}}
}

// ownLock returns a sanity result for a profile checked out by m's own
// identity, so the own-lock actions (Status, Sync, Check-in) are offered.
func ownLock(m model) *sanity.Result {
	return &sanity.Result{CheckedOut: true, Marker: &marker.Marker{CheckedOutBy: m.id.By, Host: m.id.Host}}
}

// foreignLock returns a sanity result for a profile checked out by someone
// else, so only Checkout (with the steal option) is offered.
func foreignLock() *sanity.Result {
	return &sanity.Result{CheckedOut: true, Marker: &marker.Marker{CheckedOutBy: "other@elsewhere", Host: "elsewhere"}}
}

func update(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	nm, _ := m.Update(msg)
	got, ok := nm.(model)
	if !ok {
		t.Fatalf("Update returned %T, want model", nm)
	}
	return got
}

func TestListNavigation(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if name, _ := m.list.selected(); name != "beta" {
		t.Fatalf("want beta selected after down, got %q", name)
	}
}

// TestEditOpensPrefilledForm covers the selected-row -> profile lookup path:
// "alpha" sorts first, so the initial cursor selects it, and pressing "e" must
// open the form pre-filled with that profile.
func TestEditOpensPrefilledForm(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if m.mode != modeForm {
		t.Fatalf("want modeForm after e, got %d", m.mode)
	}
	name, p := m.form.values()
	if name != "alpha" {
		t.Fatalf("want form name alpha, got %q", name)
	}
	if p.LocalRoot != "/l/a" || p.RemoteRoot != "/r/a" {
		t.Fatalf("want roots /l/a,/r/a, got %q,%q", p.LocalRoot, p.RemoteRoot)
	}
}

// TestEnterEntersActions: enter now reveals the Actions box in place of the
// profile list (modeMain throughout), rather than switching to a separate mode.
func TestEnterEntersActions(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMain {
		t.Fatalf("want modeMain after enter, got %d", m.mode)
	}
	if m.sub != subActions {
		t.Fatalf("want subActions after enter, got %d", m.sub)
	}
	if m.profile.name != "alpha" {
		t.Fatalf("want profile alpha, got %q", m.profile.name)
	}
}

// TestActionsEscReturnsToList: esc from the Actions box returns to the list,
// without leaving modeMain.
func TestActionsEscReturnsToList(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.sub != subActions {
		t.Fatalf("want subActions after enter, got %d", m.sub)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeMain {
		t.Fatalf("want modeMain after esc, got %d", m.mode)
	}
	if m.sub != subList {
		t.Fatalf("want subList after esc, got %d", m.sub)
	}
}

// TestActionsEscPreservesListCursor: leaving Actions back to the list keeps the
// list's cursor where it was, rather than resetting to the top.
func TestActionsEscPreservesListCursor(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // select "beta"
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.profile.name != "beta" {
		t.Fatalf("want profile beta, got %q", m.profile.name)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if name, _ := m.list.selected(); name != "beta" {
		t.Fatalf("want beta still selected after esc, got %q", name)
	}
}

// TestActionsQIsInert: q is intentionally unbound while Actions is showing — it
// only means "quit" on the plain list, so pressing it here does nothing.
func TestActionsQIsInert(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.sub != subActions {
		t.Fatalf("want subActions after enter, got %d", m.sub)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if m.sub != subActions {
		t.Fatalf("want subActions after q (unbound), got %d", m.sub)
	}
}

// TestListWSNavigation: w/s move the main list cursor, same as the arrows.
func TestListWSNavigation(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if name, _ := m.list.selected(); name != "beta" {
		t.Fatalf("want beta selected after s, got %q", name)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	if name, _ := m.list.selected(); name != "alpha" {
		t.Fatalf("want alpha selected after w, got %q", name)
	}
}

// TestActionsWSNavigation: w/s move the Actions box's cursor, same as the
// arrows.
func TestActionsWSNavigation(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	m.checks["alpha"] = ownLock(m) // 3 actions to navigate
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})     // reveal Actions for "alpha"
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if m.profile.cursor != 1 {
		t.Fatalf("want cursor 1 after s, got %d", m.profile.cursor)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	if m.profile.cursor != 0 {
		t.Fatalf("want cursor 0 after w, got %d", m.profile.cursor)
	}
}

func TestListQuit(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected a command from ctrl+c")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected QuitMsg")
	}
}

func TestListEscQuits(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected a command from esc in list mode")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected QuitMsg")
	}
}

func TestFormCtrlCQuits(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}) // open add form
	if m.mode != modeForm {
		t.Fatalf("want modeForm, got %d", m.mode)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected a command from ctrl+c in form mode")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected QuitMsg")
	}
}

func TestListViewNotEmpty(t *testing.T) {
	m := newModel(filepath.Join(t.TempDir(), "x.yaml"), testConfig())
	if m.View() == "" {
		t.Fatal("list view should not be empty")
	}
}

// TestViewFitsWindowWidth guards against the two-pane main view overflowing the
// terminal: after a resize, the rendered view (both panel borders included) must
// fit within the reported width, otherwise the right border is pushed off-screen.
func TestViewFitsWindowWidth(t *testing.T) {
	for _, w := range []int{80, 100, 120} {
		m := newModel("/tmp/x.yaml", testConfig())
		m = update(t, m, tea.WindowSizeMsg{Width: w, Height: 30})
		if got := lipgloss.Width(m.View()); got > w {
			t.Errorf("width=%d: view renders %d cols, overflow %d", w, got, got-w)
		}
	}
}

// TestActionsViewFitsWindowWidth: same width guard, once Actions is showing
// instead of the plain list.
func TestActionsViewFitsWindowWidth(t *testing.T) {
	for _, w := range []int{80, 100, 120} {
		m := newModel("/tmp/x.yaml", testConfig())
		m = update(t, m, tea.WindowSizeMsg{Width: w, Height: 30})
		m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if got := lipgloss.Width(m.View()); got > w {
			t.Errorf("width=%d: view renders %d cols, overflow %d", w, got, got-w)
		}
	}
}

// TestModalFitsAndIsCentered: the form modal view fills the terminal (it is
// composited over a full-size canvas) but the modal box itself is narrower than
// the terminal, i.e. centered rather than full-width.
func TestModalFitsAndIsCentered(t *testing.T) {
	for _, w := range []int{60, 100, 120} {
		m := newModel("/tmp/x.yaml", testConfig())
		m = update(t, m, tea.WindowSizeMsg{Width: w, Height: 30})
		m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
		if m.mode != modeForm {
			t.Fatalf("width=%d: want modeForm, got %d", w, m.mode)
		}
		if got := lipgloss.Width(m.View()); got > w {
			t.Errorf("width=%d: view renders %d cols, overflow %d", w, got, got-w)
		}
		if got := lipgloss.Width(m.form.View()); got >= w {
			t.Errorf("width=%d: modal box is %d cols, expected narrower (centered)", w, got)
		}
	}
}

// TestModalFloatsOverMainView: with a modal open, the dimmed main view is still
// behind it — the other profile's name and the panel titles remain visible.
func TestModalFloatsOverMainView(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")}) // delete-confirm on "alpha"
	if m.mode != modeConfirm {
		t.Fatalf("want modeConfirm, got %d", m.mode)
	}
	view := m.View()
	if !strings.Contains(view, "Profiles") || !strings.Contains(view, "Details") {
		t.Errorf("panel titles should remain visible behind the modal:\n%s", view)
	}
	if !strings.Contains(view, "beta") {
		t.Errorf("non-selected profile should remain visible behind the modal:\n%s", view)
	}
	if !strings.Contains(view, "Confirm delete") {
		t.Errorf("modal title missing:\n%s", view)
	}
	if got := lipgloss.Width(view); got > 100 {
		t.Errorf("view width %d > 100", got)
	}
}
