package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/andresbott/dibs/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestFormViewHasBorder(t *testing.T) {
	f := newForm("photos", config.Profile{LocalRoot: "/l", RemoteRoot: "/r"}, nil)
	f.setWidth(80)
	if view := f.View(); !strings.Contains(view, "╭") || !strings.Contains(view, "╯") {
		t.Fatalf("form view missing rounded border corners:\n%s", view)
	}
}

// TestFormFieldsUnderlined asserts each input renders as a value over a single
// underline (bottom border only), with no field boxes. Complements
// TestFormViewHasBorder, which covers the modal's own rounded frame.
func TestFormFieldsUnderlined(t *testing.T) {
	f := newForm("photos", config.Profile{LocalRoot: "/l", RemoteRoot: "/r"}, nil)
	f.setWidth(80)
	u := f.underline(0)
	if h := lipgloss.Height(u); h != 2 {
		t.Fatalf("underline height = %d, want 2 (value row + line)", h)
	}
	lines := strings.Split(u, "\n")
	if want := strings.Repeat("─", f.fieldWidth(0)); lines[1] != want {
		t.Fatalf("underline line = %q, want %q", lines[1], want)
	}
	if strings.Contains(f.View(), "┌") {
		t.Fatalf("form should render no field boxes (found ┌):\n%s", f.View())
	}
}

func TestFormHasBrowseButtons(t *testing.T) {
	f := newForm("photos", config.Profile{LocalRoot: "/l", RemoteRoot: "/r"}, nil)
	f.setWidth(80)
	if n := strings.Count(f.View(), "Browse"); n != 2 {
		t.Fatalf("want 2 Browse buttons (path fields only), got %d:\n%s", n, f.View())
	}
}

func TestFormDownUpColumn0IncludesSave(t *testing.T) {
	m := openAddForm(t) // Name input (slot 0)
	// Down descends the left column: Name → Local input → remote-type selector →
	// Remote input → Add subpath → the two default ignore inputs → Add ignore →
	// Save → wrap. (A fresh add form seeds the default ignore rows.)
	for i, want := range []int{1, 3, 4, 6, 7, 9, 11, 12, 0} {
		m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
		if m.form.focus != want {
			t.Fatalf("down #%d: focus = %d, want %d", i+1, m.form.focus, want)
		}
	}
	// Up from Name wraps to the bottom of the left column (Save).
	m = update(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.form.focus != 12 {
		t.Fatalf("up from Name: focus = %d, want 12 (Save)", m.form.focus)
	}
}

func TestFormDownFromRemoteBrowseFallsToColumn0(t *testing.T) {
	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Local input (empty ⇒ cursor at end)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // Local Browse
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // selector row has no right cell → falls to it
	if m.form.focusKind() != slotTypeSel {
		t.Fatalf("setup: down from Local Browse should fall to the type selector, got slot %d", m.form.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // Remote input row (col 0)
	if m.form.focusKind() != slotInput || m.form.focusField() != idxRemotePath {
		t.Fatalf("setup: want the Remote input, got slot %d", m.form.focus)
	}
	// Down then descends column 0: Add subpath, the default ignore rows' inputs,
	// Add ignore, and lands on Save.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.form.focusKind() != slotAdd {
		t.Fatalf("down from the Remote input should reach Add subpath, got slot %d", m.form.focus)
	}
	for range 4 { // two ignore inputs, Add ignore, then the action row
		m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.form.focusKind() != slotSave {
		t.Fatalf("down through the ignore section should reach Save, got slot %d", m.form.focus)
	}
}

func TestFormUpFromFirstBrowseGoesToName(t *testing.T) {
	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Local input (empty ⇒ cursor at end)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // Local Browse (the first Browse)
	if m.form.focusKind() != slotButton || m.form.focusField() != 1 {
		t.Fatalf("setup: want Local Browse, got slot %d", m.form.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyUp}) // → Name (the row above has no right cell)
	if m.form.focus != 0 {
		t.Fatalf("up from Local Browse should focus Name (slot 0), got slot %d", m.form.focus)
	}
}

func TestFormRightLeftReachButton(t *testing.T) {
	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Local input (empty ⇒ cursor at end)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // step onto the Browse button
	if !m.form.currentIsButton() || m.form.focusField() != 1 {
		t.Fatalf("right at end of Local input should focus its Browse button, got focus %d", m.form.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyLeft}) // back to the input
	if m.form.currentIsButton() || m.form.focus != m.form.inputSlot(1) {
		t.Fatalf("left from the button should return to the Local input, got focus %d", m.form.focus)
	}
}

func TestFormRightMovesCursorUntilEnd(t *testing.T) {
	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // Local input
	m = typeRunes(t, m, "abc")                      // cursor at end
	m = update(t, m, tea.KeyMsg{Type: tea.KeyLeft}) // cursor ← (editing, not a jump)
	if m.form.currentIsButton() {
		t.Fatal("left inside a non-empty input should move the cursor, not change focus")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // cursor → back to end, still on input
	if m.form.currentIsButton() {
		t.Fatal("right that only returns the cursor to the end should not jump to the button")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // at end now ⇒ jump to button
	if !m.form.currentIsButton() {
		t.Fatal("right at end of the input should jump to the Browse button")
	}
}

func TestFormRightOnNameStaysPut(t *testing.T) {
	m := openAddForm(t) // Name input (slot 0); the Name field has no Browse button
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if m.form.focus != 0 || m.form.currentIsButton() {
		t.Fatalf("right on the Name field should stay on the Name input, got focus %d", m.form.focus)
	}
}

func TestFormDownUpStaysInButtonColumn(t *testing.T) {
	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Local input
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // Local Browse button
	if !m.form.currentIsButton() || m.form.focusField() != idxLocal {
		t.Fatalf("setup: want Local Browse button, got focus %d", m.form.focus)
	}
	// The selector row below has no button cell, so Down falls to column 0; two
	// Downs later, Right reaches the Remote row's Browse button.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // type selector (col 0 fallback)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // Remote input
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if !m.form.currentIsButton() || m.form.focusField() != idxRemotePath {
		t.Fatalf("want Remote Browse button, got focus %d (button=%v)", m.form.focus, m.form.currentIsButton())
	}
	// Up from Remote Browse: the selector row above has no button cell either,
	// so focus falls back to column 0 (the selector).
	m = update(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.form.focusKind() != slotTypeSel {
		t.Fatalf("up from Remote Browse should fall to the type selector, got focus %d", m.form.focus)
	}
}

func TestFormTabCyclesAllSlots(t *testing.T) {
	m := openAddForm(t) // slot 0 (Name input)
	n := len(m.form.slots())
	// Tab steps through every slot in order: Name, each path input, each Browse
	// button, then Save and Cancel.
	for step := 1; step <= n; step++ {
		m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
		if want := step % n; m.form.focus != want {
			t.Fatalf("after %d tab(s) focus = %d, want %d", step, m.form.focus, want)
		}
	}
	// Shift+Tab reverses (from slot 0 wraps to the last slot).
	m = update(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.form.focus != n-1 {
		t.Fatalf("shift+tab from slot 0 should wrap to slot %d, got %d", n-1, m.form.focus)
	}
}

func TestFormHasSaveCancelButtons(t *testing.T) {
	f := newForm("photos", config.Profile{}, nil)
	f.setWidth(80)
	if view := f.View(); !strings.Contains(view, "Save") || !strings.Contains(view, "Cancel") {
		t.Fatalf("form should render Save and Cancel buttons:\n%s", view)
	}
}

// TestFormHasHints guards the key-hint line re-added to the form footer.
func TestFormHasHints(t *testing.T) {
	f := newForm("photos", config.Profile{LocalRoot: "/l", RemoteRoot: "/r"}, nil)
	f.setWidth(80)
	view := f.View()
	if !strings.Contains(view, "Move") {
		t.Fatalf("form view missing the Move hint:\n%s", view)
	}
	if !strings.Contains(view, "Activate") {
		t.Fatalf("form view missing the Activate hint:\n%s", view)
	}
}

// tabToKind Tabs until the focused slot has the given kind (bounded so a wiring
// bug fails instead of hanging).
func tabToKind(t *testing.T, m model, kind slotKind) model {
	t.Helper()
	for i := 0; i <= len(m.form.slots()); i++ {
		if m.form.focusKind() == kind {
			return m
		}
		m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	}
	t.Fatalf("never reached slot kind %d via Tab", kind)
	return m
}

func TestSaveButtonSubmits(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	m := newModel(p, &config.Config{Profiles: map[string]config.Profile{}})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = typeRunes(t, m, "photos")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = typeRunes(t, m, "/home/me/pics")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // remote-type selector
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // Remote input
	m = typeRunes(t, m, "/mnt/nas/pics")
	m = tabToKind(t, m, slotSave)
	m = update(t, m, spaceKey)
	if m.mode != modeMain {
		t.Fatalf("Save should submit and return to main, mode = %d", m.mode)
	}
	saved, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Profiles["photos"]; got.LocalRoot != "/home/me/pics" || got.RemoteRoot != "/mnt/nas/pics" {
		t.Fatalf("persisted profile = %#v", got)
	}
}

func TestCancelButtonReturnsToMainWithoutSaving(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	m := newModel(p, &config.Config{Profiles: map[string]config.Profile{}})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = typeRunes(t, m, "photos")
	m = tabToKind(t, m, slotCancel)
	m = update(t, m, spaceKey)
	if m.mode != modeMain {
		t.Fatalf("Cancel should return to main, mode = %d", m.mode)
	}
	if _, exists := m.cfg.Profiles["photos"]; exists {
		t.Fatal("Cancel must not save the profile")
	}
}

// TestFormEnterActivatesSave guards the additive enter-activates-buttons behavior:
// enter on the focused Save button submits the form, exactly like space does.
func TestFormEnterActivatesSave(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	m := newModel(p, &config.Config{Profiles: map[string]config.Profile{}})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = typeRunes(t, m, "photos")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = typeRunes(t, m, "/home/me/pics")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // remote-type selector
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // Remote input
	m = typeRunes(t, m, "/mnt/nas/pics")
	m = tabToKind(t, m, slotSave)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMain {
		t.Fatalf("enter on Save should submit and return to main, mode = %d", m.mode)
	}
	saved, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Profiles["photos"]; got.LocalRoot != "/home/me/pics" || got.RemoteRoot != "/mnt/nas/pics" {
		t.Fatalf("persisted profile = %#v", got)
	}
}

// TestFormEnterOnCancelReturnsToMain guards enter activating the Cancel button.
func TestFormEnterOnCancelReturnsToMain(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	m := newModel(p, &config.Config{Profiles: map[string]config.Profile{}})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = typeRunes(t, m, "photos")
	m = tabToKind(t, m, slotCancel)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMain {
		t.Fatalf("enter on Cancel should return to main, mode = %d", m.mode)
	}
	if _, exists := m.cfg.Profiles["photos"]; exists {
		t.Fatal("Cancel must not save the profile")
	}
}

// TestFormEnterOnBrowseOpensPicker guards enter activating a Browse button,
// mirroring TestBrowseButtonOpensPicker's space case.
func TestFormEnterOnBrowseOpensPicker(t *testing.T) {
	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})   // Local input (slot 1)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // → Local Browse button (slot 2)
	if !m.form.currentIsButton() || m.form.focusField() != 1 {
		t.Fatalf("want focus on the Local Browse button, got focus %d", m.form.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.form.browsing {
		t.Fatal("enter on a Browse button should open the picker")
	}
}

func TestSaveCancelLeftRight(t *testing.T) {
	m := tabToKind(t, openAddForm(t), slotSave)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if m.form.focusKind() != slotCancel {
		t.Fatalf("right from Save should focus Cancel, got slot %d", m.form.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if m.form.focusKind() != slotSave {
		t.Fatalf("left from Cancel should focus Save, got slot %d", m.form.focus)
	}
}

func TestFieldRowsFit(t *testing.T) {
	for _, w := range []int{80, 40, 30} {
		f := newForm("photos", config.Profile{LocalRoot: "/l", RemoteRoot: "/r"}, nil)
		f.setWidth(w)
		for i := range f.inputs {
			if got, budget := lipgloss.Width(f.fieldRow(i)), f.modalWidth()-4; got > budget {
				t.Fatalf("width %d field %d: fieldRow width %d exceeds budget %d", w, i, got, budget)
			}
		}
	}
}

func typeRunes(t *testing.T, m model, s string) model {
	t.Helper()
	return update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

func TestAddProfilePersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	m := newModel(p, &config.Config{Profiles: map[string]config.Profile{}})

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}) // open add form
	if m.mode != modeForm {
		t.Fatalf("want modeForm, got %d", m.mode)
	}
	m = typeRunes(t, m, "photos")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // → Local input
	m = typeRunes(t, m, "/home/me/pics")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // → remote-type selector
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // → Remote input
	m = typeRunes(t, m, "/mnt/nas/pics")
	m = tabToKind(t, m, slotSave)
	m = update(t, m, spaceKey) // submit

	if m.mode != modeMain {
		t.Fatalf("want modeMain after save, got %d", m.mode)
	}
	saved, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	got := saved.Profiles["photos"]
	if got.LocalRoot != "/home/me/pics" || got.RemoteRoot != "/mnt/nas/pics" {
		t.Fatalf("persisted profile = %#v", got)
	}
	if got.ID == "" {
		t.Fatal("a new profile must be assigned an ID")
	}
}

// TestEditPreservesProfileID: editing (or renaming) a profile keeps its ID —
// the ID identifies checkout markers, so it must survive every form save. A
// hand-written profile that predates IDs gets one assigned on its first edit.
func TestEditPreservesProfileID(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"work":   {ID: "uuid-work", LocalRoot: "/l", RemoteRoot: "/r"},
		"legacy": {LocalRoot: "/l2", RemoteRoot: "/r2"}, // pre-ID profile
	}}
	if err := config.Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	m := newModel(p, cfg)
	m.mode = modeForm
	m.form = newForm("work", cfg.Profiles["work"], nil)
	m.form.inputs[0].SetValue("renamed")
	m = tabToKind(t, m, slotSave)
	m = update(t, m, spaceKey)
	if got := m.cfg.Profiles["renamed"].ID; got != "uuid-work" {
		t.Fatalf("renamed profile ID = %q, want the original uuid-work", got)
	}

	m.mode = modeForm
	m.form = newForm("legacy", m.cfg.Profiles["legacy"], nil)
	m = tabToKind(t, m, slotSave)
	m = update(t, m, spaceKey)
	if m.cfg.Profiles["legacy"].ID == "" {
		t.Fatal("editing a pre-ID profile must assign it an ID")
	}
}

func TestAddProfileValidationBlocks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	m := newModel(p, &config.Config{Profiles: map[string]config.Profile{}})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = tabToKind(t, m, slotSave)
	m = update(t, m, spaceKey) // empty name
	if m.mode != modeForm {
		t.Fatal("empty name should keep the form open")
	}
	if m.form.err == "" {
		t.Fatal("expected a validation error message")
	}
}

func TestEditRenameReplacesKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"old": {LocalRoot: "/l", RemoteRoot: "/r"},
	}}
	if err := config.Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	m := newModel(p, cfg)
	m.mode = modeForm
	m.form = newForm("old", cfg.Profiles["old"], nil)
	m.form.inputs[0].SetValue("new")
	m = tabToKind(t, m, slotSave)
	m = update(t, m, spaceKey)

	if _, exists := m.cfg.Profiles["old"]; exists {
		t.Error("old key should be removed after rename")
	}
	if _, exists := m.cfg.Profiles["new"]; !exists {
		t.Error("new key should exist after rename")
	}
}

// TestEditPreservesSubpaths guards against the add/edit form silently dropping a
// profile's subpaths: editing a subpaths-bearing profile (here changing its local
// root) without touching the subpath rows must round-trip the subpaths untouched
// rather than reset them to nil.
func TestEditPreservesSubpaths(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"work": {LocalRoot: "/l", RemoteRoot: "/r", Subpaths: []string{"a", "b/c"}},
	}}
	if err := config.Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	m := newModel(p, cfg)
	m.mode = modeForm
	m.form = newForm("work", cfg.Profiles["work"], nil)
	m.form.inputs[1].SetValue("/l2") // edit the local root, leaving the name unchanged
	m = tabToKind(t, m, slotSave)
	m = update(t, m, spaceKey)

	saved, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	got := saved.Profiles["work"]
	if got.LocalRoot != "/l2" {
		t.Fatalf("local root = %q, want /l2 (edit not applied)", got.LocalRoot)
	}
	if !reflect.DeepEqual(got.Subpaths, []string{"a", "b/c"}) {
		t.Fatalf("subpaths = %#v, want [a b/c] preserved across an edit", got.Subpaths)
	}
}

// TestFormShowsSubpathRows: a profile's subpaths render as editable rows in the
// form — one input per subpath (seeded with its value) with a Remove button —
// plus an Add-subpath button, under a Subpaths section label.
func TestFormShowsSubpathRows(t *testing.T) {
	f := newForm("work", config.Profile{LocalRoot: "/l", RemoteRoot: "/r", Subpaths: []string{"a", "b/c"}}, nil)
	f.setWidth(80)
	view := f.View()
	if !strings.Contains(view, "Subpaths") {
		t.Fatalf("form view missing the Subpaths label:\n%s", view)
	}
	for _, sub := range []string{"a", "b/c"} {
		if !strings.Contains(view, sub) {
			t.Fatalf("form view missing subpath %q:\n%s", sub, view)
		}
	}
	if n := strings.Count(view, "Remove"); n != 2 {
		t.Fatalf("want 2 Remove buttons (one per subpath), got %d:\n%s", n, view)
	}
	if !strings.Contains(view, "Add subpath") {
		t.Fatalf("form view missing the Add subpath button:\n%s", view)
	}
}

// TestFormAddSubpathAppendsRow: activating the Add-subpath button appends an
// empty subpath input and focuses it, ready for typing.
func TestFormAddSubpathAppendsRow(t *testing.T) {
	m := openAddForm(t)
	m = tabToKind(t, m, slotAdd)
	m = update(t, m, spaceKey)
	if got := len(m.form.inputs); got != numFixed+3 {
		t.Fatalf("inputs = %d, want %d (fixed fields, one subpath, two default ignores)", got, numFixed+3)
	}
	if k := m.form.focusKind(); k != slotInput || m.form.focusField() != numFixed {
		t.Fatalf("focus should land on the new subpath input, got kind %d field %d", k, m.form.focusField())
	}
	m = typeRunes(t, m, "docs")
	if got := m.form.inputs[numFixed].Value(); got != "docs" {
		t.Fatalf("subpath input = %q, want docs", got)
	}
}

// TestFormRemoveSubpathDeletesRow: activating a subpath's Remove button drops
// that row (and only that row).
func TestFormRemoveSubpathDeletesRow(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"work": {LocalRoot: "/l", RemoteRoot: "/r", Subpaths: []string{"a", "b"}},
	}}
	m := newModel(p, cfg)
	m.mode = modeForm
	m.form = newForm("work", cfg.Profiles["work"], nil)
	m = tabToKind(t, m, slotRemove) // first subpath's Remove button
	m = update(t, m, spaceKey)
	if got := len(m.form.inputs); got != numFixed+1 {
		t.Fatalf("inputs = %d, want %d after removing one of two subpaths", got, numFixed+1)
	}
	if got := m.form.inputs[numFixed].Value(); got != "b" {
		t.Fatalf("remaining subpath = %q, want b (first row removed)", got)
	}
}

// TestFormShowsIgnoreRows: a profile's ignore patterns render as editable rows
// under an Ignored-files section label, each with a Remove button, plus an
// Add-ignore button.
func TestFormShowsIgnoreRows(t *testing.T) {
	f := newForm("work", config.Profile{LocalRoot: "/l", RemoteRoot: "/r", Ignore: []string{".DS_Store", "*.tmp"}}, nil)
	f.setWidth(80)
	view := f.View()
	if !strings.Contains(view, "Ignored files") {
		t.Fatalf("form view missing the Ignored files label:\n%s", view)
	}
	for _, pat := range []string{".DS_Store", "*.tmp"} {
		if !strings.Contains(view, pat) {
			t.Fatalf("form view missing ignore pattern %q:\n%s", pat, view)
		}
	}
	if n := strings.Count(view, "Remove"); n != 2 {
		t.Fatalf("want 2 Remove buttons (one per ignore row), got %d:\n%s", n, view)
	}
	if !strings.Contains(view, "Add ignore") {
		t.Fatalf("form view missing the Add ignore button:\n%s", view)
	}
}

// TestFormAddIgnoreAppendsRow: activating the Add-ignore button appends an
// empty pattern input and focuses it, ready for typing.
func TestFormAddIgnoreAppendsRow(t *testing.T) {
	m := openAddForm(t) // seeded with the two default ignore rows
	m = tabToKind(t, m, slotAddIgnore)
	m = update(t, m, spaceKey)
	if got := len(m.form.inputs); got != numFixed+3 {
		t.Fatalf("inputs = %d, want %d (fixed fields, three ignores)", got, numFixed+3)
	}
	if k := m.form.focusKind(); k != slotInput || m.form.focusField() != numFixed+2 {
		t.Fatalf("focus should land on the new ignore input, got kind %d field %d", k, m.form.focusField())
	}
	m = typeRunes(t, m, "*.tmp")
	if got := m.form.inputs[numFixed+2].Value(); got != "*.tmp" {
		t.Fatalf("ignore input = %q, want *.tmp", got)
	}
}

// TestFormRemoveIgnoreDeletesRow: activating an ignore row's Remove button drops
// that row (and only that row); with no subpaths, the first Remove belongs to
// the first ignore row.
func TestFormRemoveIgnoreDeletesRow(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"work": {LocalRoot: "/l", RemoteRoot: "/r", Ignore: []string{".DS_Store", ".directory"}},
	}}
	m := newModel(p, cfg)
	m.mode = modeForm
	m.form = newForm("work", cfg.Profiles["work"], nil)
	m = tabToKind(t, m, slotRemove) // first ignore row's Remove button
	m = update(t, m, spaceKey)
	if got := len(m.form.inputs); got != numFixed+1 {
		t.Fatalf("inputs = %d, want %d after removing one of two ignore rows", got, numFixed+1)
	}
	if got := m.form.inputs[numFixed].Value(); got != ".directory" {
		t.Fatalf("remaining ignore = %q, want .directory (first row removed)", got)
	}
}

// TestFormSavePersistsIgnore: ignore edits made in the form survive Save and
// land in the config file; blank rows are dropped.
func TestFormSavePersistsIgnore(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"work": {LocalRoot: "/l", RemoteRoot: "/r", Ignore: []string{".DS_Store"}},
	}}
	if err := config.Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	m := newModel(p, cfg)
	m.mode = modeForm
	m.form = newForm("work", cfg.Profiles["work"], nil)
	m = tabToKind(t, m, slotAddIgnore)
	m = update(t, m, spaceKey) // add a second pattern row
	m = typeRunes(t, m, "*.tmp")
	m = tabToKind(t, m, slotSave)
	m = update(t, m, spaceKey)

	if m.mode != modeMain {
		t.Fatalf("want modeMain after save, got %d (err %q)", m.mode, m.form.err)
	}
	saved, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Profiles["work"].Ignore; !reflect.DeepEqual(got, []string{".DS_Store", "*.tmp"}) {
		t.Fatalf("saved ignore = %#v, want [.DS_Store *.tmp]", got)
	}
}

// TestFormSaveRejectsInvalidIgnore: a slash-containing pattern blocks the save
// with a validation error, like the other fields do.
func TestFormSaveRejectsInvalidIgnore(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"work": {LocalRoot: "/l", RemoteRoot: "/r"},
	}}
	m := newModel(p, cfg)
	m.mode = modeForm
	m.form = newForm("work", cfg.Profiles["work"], nil)
	m = tabToKind(t, m, slotAddIgnore)
	m = update(t, m, spaceKey)
	m = typeRunes(t, m, "a/b")
	m = tabToKind(t, m, slotSave)
	m = update(t, m, spaceKey)

	if m.mode != modeForm {
		t.Fatal("invalid ignore pattern should keep the form open")
	}
	if m.form.err == "" {
		t.Fatal("expected a validation error message")
	}
}

// TestFormAddProfileSeedsDefaultIgnore: opening the add form (the `a` key)
// prefills the default ignore patterns, which land in the saved profile.
func TestFormAddProfileSeedsDefaultIgnore(t *testing.T) {
	m := openAddForm(t)
	if got := m.form.ignores(); !reflect.DeepEqual(got, config.DefaultIgnore()) {
		t.Fatalf("add form ignores = %#v, want the defaults %#v", got, config.DefaultIgnore())
	}
}

// TestFormSavePersistsEditedSubpaths: subpath edits made in the form (adding a
// row, editing an existing one) survive Save and land in the config file.
func TestFormSavePersistsEditedSubpaths(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"work": {LocalRoot: "/l", RemoteRoot: "/r", Subpaths: []string{"a"}},
	}}
	if err := config.Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	m := newModel(p, cfg)
	m.mode = modeForm
	m.form = newForm("work", cfg.Profiles["work"], nil)
	m.form.inputs[numFixed].SetValue("a2") // edit the existing subpath
	m = tabToKind(t, m, slotAdd)
	m = update(t, m, spaceKey) // add a second row
	m = typeRunes(t, m, "b/c")
	m = tabToKind(t, m, slotSave)
	m = update(t, m, spaceKey)

	if m.mode != modeMain {
		t.Fatalf("want modeMain after save, got %d", m.mode)
	}
	saved, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Profiles["work"].Subpaths; !reflect.DeepEqual(got, []string{"a2", "b/c"}) {
		t.Fatalf("saved subpaths = %#v, want [a2 b/c]", got)
	}
}

// TestFormSaveDropsBlankSubpaths: an added-but-left-empty subpath row is not an
// error — it is simply dropped on save, and dropping the last one saves nil.
func TestFormSaveDropsBlankSubpaths(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"work": {LocalRoot: "/l", RemoteRoot: "/r"},
	}}
	if err := config.Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	m := newModel(p, cfg)
	m.mode = modeForm
	m.form = newForm("work", cfg.Profiles["work"], nil)
	m = tabToKind(t, m, slotAdd)
	m = update(t, m, spaceKey) // add a row, leave it blank
	m = tabToKind(t, m, slotSave)
	m = update(t, m, spaceKey)

	if m.mode != modeMain {
		t.Fatalf("want modeMain after save, got %d (err %q)", m.mode, m.form.err)
	}
	saved, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Profiles["work"].Subpaths; got != nil {
		t.Fatalf("saved subpaths = %#v, want nil (blank row dropped)", got)
	}
}

// TestFormSaveRejectsInvalidSubpath: an invalid subpath (absolute, escaping the
// root, ...) blocks the save with a validation error, like the root fields do.
func TestFormSaveRejectsInvalidSubpath(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"work": {LocalRoot: "/l", RemoteRoot: "/r"},
	}}
	m := newModel(p, cfg)
	m.mode = modeForm
	m.form = newForm("work", cfg.Profiles["work"], nil)
	m = tabToKind(t, m, slotAdd)
	m = update(t, m, spaceKey)
	m = typeRunes(t, m, "../escape")
	m = tabToKind(t, m, slotSave)
	m = update(t, m, spaceKey)

	if m.mode != modeForm {
		t.Fatal("invalid subpath should keep the form open")
	}
	if m.form.err == "" {
		t.Fatal("expected a validation error message")
	}
}

// TestFormQIsInertOnButtons: q no longer cancels the form — the form has no `q`
// binding at all now (only the main list's `q` means "quit"), so pressing it on
// a non-input slot does nothing.
func TestFormQIsInertOnButtons(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	m := newModel(p, &config.Config{Profiles: map[string]config.Profile{}})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = typeRunes(t, m, "photos")
	m = tabToKind(t, m, slotSave)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if m.mode != modeForm {
		t.Fatalf("q on Save should do nothing now, mode = %d", m.mode)
	}
}

// TestFormQTypesInTextInput guards against q being swallowed as the cancel
// shortcut while a text field is focused — a profile named "quick" must still
// be typeable.
func TestFormQTypesInTextInput(t *testing.T) {
	m := openAddForm(t) // focus starts on the Name input
	m = typeRunes(t, m, "q")
	if got := m.form.inputs[0].Value(); got != "q" {
		t.Fatalf("q on a text input should be typed, got %q", got)
	}
	if m.mode != modeForm {
		t.Fatalf("typing q must not leave the form, mode = %d", m.mode)
	}
}

// TestFormEscCancels: esc directly cancels the form and returns to the list,
// from any focus — no more two-step "esc focuses Cancel, then activate it".
func TestFormEscCancels(t *testing.T) {
	m := newModel(filepath.Join(t.TempDir(), "config.yaml"), &config.Config{Profiles: map[string]config.Profile{}})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeMain {
		t.Fatalf("esc should cancel and return to main, mode = %d", m.mode)
	}
}

// failingSavePath returns a path whose parent directory can never be created:
// <dir>/notadir is a regular file, so config.Save's MkdirAll deterministically
// fails with "not a directory".
func failingSavePath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	blocker := filepath.Join(dir, "notadir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(blocker, "config.yaml")
}

// TestAddProfileSaveFailureKeepsFormAndEdits covers spec §9: a save failure
// must be surfaced in the TUI without losing the user's edits or committing
// the change in memory ahead of disk.
func TestAddProfileSaveFailureKeepsFormAndEdits(t *testing.T) {
	p := failingSavePath(t)
	m := newModel(p, &config.Config{Profiles: map[string]config.Profile{}})

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}) // open add form
	m = typeRunes(t, m, "photos")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // → Local input
	m = typeRunes(t, m, "/home/me/pics")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // → remote-type selector
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // → Remote input
	m = typeRunes(t, m, "/mnt/nas/pics")
	m = tabToKind(t, m, slotSave)
	m = update(t, m, spaceKey) // submit; save should fail

	if m.mode != modeForm {
		t.Fatalf("want modeForm to stay open after save failure, got %d", m.mode)
	}
	if m.form.err == "" {
		t.Fatal("expected a save-failure error message on the form")
	}
	if got := m.form.inputs[0].Value(); got != "photos" {
		t.Fatalf("name input = %q, want it preserved", got)
	}
	if got := m.form.inputs[1].Value(); got != "/home/me/pics" {
		t.Fatalf("local root input = %q, want it preserved", got)
	}
	if got := m.form.inputs[idxRemotePath].Value(); got != "/mnt/nas/pics" {
		t.Fatalf("remote root input = %q, want it preserved", got)
	}
	if _, exists := m.cfg.Profiles["photos"]; exists {
		t.Error("profile should not be committed to memory when save fails")
	}
}

// TestEditRenameSaveFailureRollsBackProfiles covers the rename branch of
// submitForm: the old key is deleted and the new key set before saving, so a
// failed save must restore the original map wholesale (old key back, new key
// gone), not just undo one half of the mutation.
func TestEditRenameSaveFailureRollsBackProfiles(t *testing.T) {
	p := failingSavePath(t)
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"old": {LocalRoot: "/l", RemoteRoot: "/r"},
	}}
	m := newModel(p, cfg)
	m.mode = modeForm
	m.form = newForm("old", cfg.Profiles["old"], nil)
	m.form.inputs[0].SetValue("new")
	m = tabToKind(t, m, slotSave)
	m = update(t, m, spaceKey) // submit; save should fail

	if m.mode != modeForm {
		t.Fatalf("want modeForm to stay open after save failure, got %d", m.mode)
	}
	if m.form.err == "" {
		t.Fatal("expected a save-failure error message on the form")
	}
	if _, exists := m.cfg.Profiles["old"]; !exists {
		t.Error("old profile should be restored when save fails")
	}
	if _, exists := m.cfg.Profiles["new"]; exists {
		t.Error("new profile should not be committed when save fails")
	}
}

// --- remote-type selector & per-kind fields ---

// TestNewFormDecomposesRsyncURL: editing a legacy rsync:// profile (pre-server
// refactor) opens the form on the rsync kind with the module path extracted;
// the URL's connection info is ignored (no longer decomposed into separate fields).
func TestNewFormDecomposesRsyncURL(t *testing.T) {
	f := newForm("work", config.Profile{
		LocalRoot:          "/l",
		RemoteRoot:         "rsync://alice@nas:874/mod/inner",
		RsyncdPasswordFile: "/pw",
	}, nil)
	if f.kind != remoteRsync {
		t.Fatalf("kind = %d, want remoteRsync", f.kind)
	}
	// The module path should be seeded from the URL's module part
	if got := f.inputs[idxModulePath].Value(); got != "mod/inner" {
		t.Errorf("module path = %q, want mod/inner", got)
	}
	// No server should be selected for a legacy URL-based profile
	if f.serverSel >= 0 {
		t.Errorf("serverSel = %d, want -1 (no server for legacy URL)", f.serverSel)
	}
}

// TestNewFormDecomposesSSHURL: an ssh:// profile opens on the ssh kind with the
// raw URL in the URL field and the identity file exposed.
func TestNewFormDecomposesSSHURL(t *testing.T) {
	f := newForm("work", config.Profile{
		LocalRoot:       "/l",
		RemoteRoot:      "ssh://bob@nas/srv/data",
		SSHIdentityFile: "~/.ssh/id",
	}, nil)
	if f.kind != remoteSSH {
		t.Fatalf("kind = %d, want remoteSSH", f.kind)
	}
	if got := f.inputs[idxSSHURL].Value(); got != "ssh://bob@nas/srv/data" {
		t.Errorf("ssh url input = %q", got)
	}
	if got := f.inputs[idxSSHIdentity].Value(); got != "~/.ssh/id" {
		t.Errorf("identity input = %q", got)
	}
}

// TestNewFormPlainPathIsLocalKind: a plain-path profile opens on the Local kind.
func TestNewFormPlainPathIsLocalKind(t *testing.T) {
	f := newForm("work", config.Profile{LocalRoot: "/l", RemoteRoot: "/mnt/nas"}, nil)
	if f.kind != remoteLocal {
		t.Fatalf("kind = %d, want remoteLocal", f.kind)
	}
	if got := f.inputs[idxRemotePath].Value(); got != "/mnt/nas" {
		t.Errorf("remote path input = %q", got)
	}
}

// TestNewFormMalformedRsyncURLKeepsValue: a malformed rsync:// value must not
// crash or be discarded — it opens on the rsync kind with the raw value in the
// module-path field, and Save re-validates.
func TestNewFormMalformedRsyncURLKeepsValue(t *testing.T) {
	raw := "rsync://nas:bad-port/mod"
	f := newForm("work", config.Profile{LocalRoot: "/l", RemoteRoot: raw}, nil)
	if f.kind != remoteRsync {
		t.Fatalf("kind = %d, want remoteRsync", f.kind)
	}
	if got := f.inputs[idxModulePath].Value(); got != raw {
		t.Errorf("module-path input = %q, want the raw value %q preserved", got, raw)
	}
}

// tabToTypeSel Tabs until the remote-type selector has focus.
func tabToTypeSel(t *testing.T, m model) model {
	t.Helper()
	for i := 0; i <= len(m.form.slots()); i++ {
		if m.form.focusKind() == slotTypeSel {
			return m
		}
		m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	}
	t.Fatal("never reached the type selector via Tab")
	return m
}

// TestTypeSelectorCyclesKinds: right/left on the selector step the kind
// (wrapping), space cycles forward, and the exposed remote fields follow.
func TestTypeSelectorCyclesKinds(t *testing.T) {
	m := tabToTypeSel(t, openAddForm(t))
	if m.form.kind != remoteLocal {
		t.Fatalf("fresh form kind = %d, want remoteLocal", m.form.kind)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if m.form.kind != remoteRsync {
		t.Fatalf("right: kind = %d, want remoteRsync", m.form.kind)
	}
	if got := len(m.form.remoteFields()); got != 1 {
		t.Fatalf("rsync kind should expose 1 field (module path), got %d", got)
	}
	m = update(t, m, spaceKey)
	if m.form.kind != remoteSSH {
		t.Fatalf("space: kind = %d, want remoteSSH", m.form.kind)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // wraps
	if m.form.kind != remoteLocal {
		t.Fatalf("right from ssh: kind = %d, want remoteLocal (wrap)", m.form.kind)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyLeft}) // wraps back
	if m.form.kind != remoteSSH {
		t.Fatalf("left from Local: kind = %d, want remoteSSH (wrap)", m.form.kind)
	}
	if m.form.focusKind() != slotTypeSel {
		t.Fatal("cycling the kind must keep focus on the selector")
	}
}

// TestTypeSwitchPreservesTypedValues: values typed under one kind survive a
// switch away and back — the inputs always exist, only their exposure changes.
func TestTypeSwitchPreservesTypedValues(t *testing.T) {
	m := tabToTypeSel(t, openAddForm(t))
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})              // → rsync
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})               // → server selector (or module if no servers)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})               // → module path input
	m = typeRunes(t, m, "docs")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyUp})                 // back up
	m = update(t, m, tea.KeyMsg{Type: tea.KeyUp})                 // back to selector
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})              // ssh → Local
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})              // → rsync again
	if got := m.form.inputs[idxModulePath].Value(); got != "docs" {
		t.Fatalf("module path input = %q, want docs preserved across kind switches", got)
	}
}

// TestValuesComposesSSH: values() under the ssh kind reads the URL and identity
// file.
func TestValuesComposesSSH(t *testing.T) {
	f := newForm("", config.Profile{}, nil)
	f.kind = remoteSSH
	f.inputs[idxSSHURL].SetValue("ssh://nas/srv/data")
	f.inputs[idxSSHIdentity].SetValue("~/.ssh/id")
	_, p := f.values()
	if p.RemoteRoot != "ssh://nas/srv/data" {
		t.Errorf("RemoteRoot = %q", p.RemoteRoot)
	}
	if p.SSHIdentityFile != "~/.ssh/id" {
		t.Errorf("SSHIdentityFile = %q", p.SSHIdentityFile)
	}
	if p.Server != "" {
		t.Errorf("Server = %q, want empty (ssh not server-backed)", p.Server)
	}
}

// TestSaveRsyncProfilePersists: selecting a server and filling the module path
// through the UI and saving persists Server+RemoteModule (not a RemoteRoot URL).
func TestSaveRsyncProfilePersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{
		Profiles: map[string]config.Profile{},
		Servers:  map[string]config.Server{"nas": {Host: "nas.local"}},
	}
	m := newModel(p, cfg)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = typeRunes(t, m, "photos")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = typeRunes(t, m, "/home/me/pics")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // remote-type selector
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // → rsync
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // server selector
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // → nas
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Module/path
	m = typeRunes(t, m, "mod/photos")
	m = tabToKind(t, m, slotSave)
	m = update(t, m, spaceKey)

	if m.mode != modeMain {
		t.Fatalf("want modeMain after save, got %d (err %q)", m.mode, m.form.err)
	}
	saved, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	got := saved.Profiles["photos"]
	if got.Server != "nas" || got.RemoteModule != "mod/photos" {
		t.Fatalf("saved Server=%q RemoteModule=%q, want nas, mod/photos", got.Server, got.RemoteModule)
	}
	if got.RemoteRoot != "" {
		t.Fatalf("saved RemoteRoot=%q, want empty (server-backed)", got.RemoteRoot)
	}
}

// TestSaveRsyncMissingServerBlocked: saving the rsync kind without selecting a
// server keeps the form open with a validation error.
func TestSaveRsyncMissingServerBlocked(t *testing.T) {
	pth := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{
		Profiles: map[string]config.Profile{},
		Servers:  map[string]config.Server{"nas": {Host: "nas.local"}},
	}
	m := newModel(pth, cfg)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = typeRunes(t, m, "photos")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = typeRunes(t, m, "/home/me/pics")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // selector
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // → rsync (server not selected)
	m = tabToKind(t, m, slotSave)
	m = update(t, m, spaceKey)
	if m.mode != modeForm {
		t.Fatal("missing server should keep the form open")
	}
	if m.form.err == "" {
		t.Fatal("expected a validation error message")
	}
}

// TestEditRsyncProfileRoundTrips: opening a server-backed rsync profile and
// saving untouched round-trips the exact same Server+RemoteModule.
func TestEditRsyncProfileRoundTrips(t *testing.T) {
	pth := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{
		Profiles: map[string]config.Profile{
			"work": {LocalRoot: "/l", Server: "nas", RemoteModule: "mod/inner"},
		},
		Servers: map[string]config.Server{"nas": {Host: "nas.local"}},
	}
	if err := config.Save(pth, cfg); err != nil {
		t.Fatal(err)
	}
	m := newModel(pth, cfg)
	m.mode = modeForm
	m.form = newForm("work", cfg.Profiles["work"], cfg.Servers)
	m = tabToKind(t, m, slotSave)
	m = update(t, m, spaceKey)
	if m.mode != modeMain {
		t.Fatalf("want modeMain after save, got %d (err %q)", m.mode, m.form.err)
	}
	saved, err := config.Load(pth)
	if err != nil {
		t.Fatal(err)
	}
	got := saved.Profiles["work"]
	if got.Server != "nas" || got.RemoteModule != "mod/inner" {
		t.Fatalf("Server=%q RemoteModule=%q, want nas, mod/inner", got.Server, got.RemoteModule)
	}
	if got.RemoteRoot != "" {
		t.Fatalf("RemoteRoot=%q, want empty (server-backed)", got.RemoteRoot)
	}
}

// TestFormViewShowsTypeSelector: the form renders the radio row with the three
// kind options and the selected one marked.
func TestFormViewShowsTypeSelector(t *testing.T) {
	servers := map[string]config.Server{"nas": {Host: "nas.local"}}
	f := newForm("work", config.Profile{LocalRoot: "/l", Server: "nas", RemoteModule: "mod"}, servers)
	f.setWidth(80)
	view := f.View()
	for _, want := range []string{"Remote type", "Local", "rsync", "ssh", "(•)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("form view missing %q:\n%s", want, view)
		}
	}
	// The rsync kind's server selector and module path field are visible.
	for _, want := range []string{"Server", "Module / path"} {
		if !strings.Contains(view, want) {
			t.Fatalf("rsync kind should show %q:\n%s", want, view)
		}
	}
}

// TestFormViewGroupDividers: the form's groups after Name — root dirs,
// subpaths, ignored files — each open with a titled dotted divider
// ("┄┄ Title ┄┄┄…"), distinct from the fields' solid underlines.
func TestFormViewGroupDividers(t *testing.T) {
	f := newForm("work", config.Profile{LocalRoot: "/l", RemoteRoot: "/r"}, nil)
	f.setWidth(80)
	view := f.View()
	for _, title := range []string{"Root dirs", "Subpaths", "Ignored files"} {
		if n := strings.Count(view, "┄┄ "+title); n != 1 {
			t.Fatalf("want the %q section header exactly once, got %d:\n%s", title, n, view)
		}
	}
}

// TestFormRsyncValuesUseServerRef: rsync kind sets Server + RemoteModule, not RemoteRoot
func TestFormRsyncValuesUseServerRef(t *testing.T) {
	servers := map[string]config.Server{"nas": {Host: "nas.local"}, "backup": {Host: "b"}}
	f := newForm("", config.Profile{}, servers)
	f.kind = remoteRsync
	f.selectServer("nas")
	f.inputs[idxModulePath].SetValue("share/docs")
	name, p := f.values()
	_ = name
	if p.Server != "nas" || p.RemoteModule != "share/docs" {
		t.Fatalf("rsync values: Server=%q RemoteModule=%q", p.Server, p.RemoteModule)
	}
	if p.RemoteRoot != "" {
		t.Fatalf("rsync profile should not set RemoteRoot on disk, got %q", p.RemoteRoot)
	}
}

// TestFormSeedsRsyncFromServerRef: editing a server-backed profile selects the right server
func TestFormSeedsRsyncFromServerRef(t *testing.T) {
	servers := map[string]config.Server{"nas": {Host: "nas.local"}}
	f := newForm("edit", config.Profile{Server: "nas", RemoteModule: "share/docs"}, servers)
	if f.kind != remoteRsync {
		t.Fatalf("kind = %v, want remoteRsync", f.kind)
	}
	if f.serverSel < 0 || f.serverSel >= len(f.serverNames) || f.serverNames[f.serverSel] != "nas" {
		t.Fatalf("selected server = %q (serverSel=%d)", safeServerName(f.serverNames, f.serverSel), f.serverSel)
	}
	if f.inputs[idxModulePath].Value() != "share/docs" {
		t.Fatalf("module = %q", f.inputs[idxModulePath].Value())
	}
}

func safeServerName(names []string, idx int) string {
	if idx < 0 || idx >= len(names) {
		return "(none)"
	}
	return names[idx]
}
