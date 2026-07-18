package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/andresbott/netcheckout/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestFormViewHasBorder(t *testing.T) {
	f := newForm("photos", config.Profile{LocalRoot: "/l", RemoteRoot: "/r"})
	f.setWidth(80)
	if view := f.View(); !strings.Contains(view, "╭") || !strings.Contains(view, "╯") {
		t.Fatalf("form view missing rounded border corners:\n%s", view)
	}
}

// TestFormFieldsUnderlined asserts each input renders as a value over a single
// underline (bottom border only), with no field boxes. Complements
// TestFormViewHasBorder, which covers the modal's own rounded frame.
func TestFormFieldsUnderlined(t *testing.T) {
	f := newForm("photos", config.Profile{LocalRoot: "/l", RemoteRoot: "/r"})
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
	f := newForm("photos", config.Profile{LocalRoot: "/l", RemoteRoot: "/r"})
	f.setWidth(80)
	if n := strings.Count(f.View(), "Browse"); n != 2 {
		t.Fatalf("want 2 Browse buttons (path fields only), got %d:\n%s", n, f.View())
	}
}

func TestFormDownUpColumn0IncludesSave(t *testing.T) {
	m := openAddForm(t) // Name input (slot 0)
	// Down descends the left column: Name → Local input → Remote input → Save → wrap.
	for i, want := range []int{1, 3, 5, 0} { // Local input, Remote input, Save, wrap to Name
		m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
		if m.form.focus != want {
			t.Fatalf("down #%d: focus = %d, want %d", i+1, m.form.focus, want)
		}
	}
	// Up from Name wraps to the bottom of the left column (Save).
	m = update(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.form.focus != 5 {
		t.Fatalf("up from Name: focus = %d, want 5 (Save)", m.form.focus)
	}
}

func TestFormDownFromRemoteBrowseToCancel(t *testing.T) {
	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Local input (empty ⇒ cursor at end)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // Local Browse
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Remote Browse
	if formSlots[m.form.focus].kind != slotButton || m.form.focusField() != 2 {
		t.Fatalf("setup: want Remote Browse, got slot %d", m.form.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // → Cancel
	if formSlots[m.form.focus].kind != slotCancel {
		t.Fatalf("down from Remote Browse should focus Cancel, got slot %d", m.form.focus)
	}
}

func TestFormUpFromFirstBrowseGoesToName(t *testing.T) {
	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Local input (empty ⇒ cursor at end)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // Local Browse (the first Browse)
	if formSlots[m.form.focus].kind != slotButton || m.form.focusField() != 1 {
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
	if m.form.currentIsButton() || m.form.focus != inputSlot(1) {
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
	if !m.form.currentIsButton() || m.form.focusField() != 1 {
		t.Fatalf("setup: want Local Browse button, got focus %d", m.form.focus)
	}
	// Down from a button stays on the button column → Remote Browse (not the input).
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if !m.form.currentIsButton() || m.form.focusField() != 2 {
		t.Fatalf("down from Local Browse should focus Remote Browse, got focus %d (button=%v)", m.form.focus, m.form.currentIsButton())
	}
	// Up returns to Local Browse.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if !m.form.currentIsButton() || m.form.focusField() != 1 {
		t.Fatalf("up from Remote Browse should focus Local Browse, got focus %d (button=%v)", m.form.focus, m.form.currentIsButton())
	}
}

func TestFormTabCyclesAllSlots(t *testing.T) {
	m := openAddForm(t) // slot 0 (Name input)
	n := len(formSlots)
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
	f := newForm("photos", config.Profile{})
	f.setWidth(80)
	if view := f.View(); !strings.Contains(view, "Save") || !strings.Contains(view, "Cancel") {
		t.Fatalf("form should render Save and Cancel buttons:\n%s", view)
	}
}

// TestFormHasHints guards the key-hint line re-added to the form footer.
func TestFormHasHints(t *testing.T) {
	f := newForm("photos", config.Profile{LocalRoot: "/l", RemoteRoot: "/r"})
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
	for i := 0; i <= len(formSlots); i++ {
		if formSlots[m.form.focus].kind == kind {
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
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
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
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
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
	if formSlots[m.form.focus].kind != slotCancel {
		t.Fatalf("right from Save should focus Cancel, got slot %d", m.form.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if formSlots[m.form.focus].kind != slotSave {
		t.Fatalf("left from Cancel should focus Save, got slot %d", m.form.focus)
	}
}

func TestFieldRowsFit(t *testing.T) {
	for _, w := range []int{80, 40, 30} {
		f := newForm("photos", config.Profile{LocalRoot: "/l", RemoteRoot: "/r"})
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
	m.form = newForm("old", cfg.Profiles["old"])
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
// profile's subpaths. The form has no subpaths input, so editing a subpaths-bearing
// profile (here changing its local root) and saving must round-trip the subpaths
// untouched rather than reset them to nil.
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
	m.form = newForm("work", cfg.Profiles["work"])
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
	if got := m.form.inputs[2].Value(); got != "/mnt/nas/pics" {
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
	m.form = newForm("old", cfg.Profiles["old"])
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
