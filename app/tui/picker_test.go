package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andresbott/netcheckout/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// openAddForm returns a model with the add form open and sized.
func openAddForm(t *testing.T) model {
	t.Helper()
	m := newModel(filepath.Join(t.TempDir(), "x.yaml"), &config.Config{Profiles: map[string]config.Profile{}})
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 30})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if m.mode != modeForm {
		t.Fatalf("want modeForm, got %d", m.mode)
	}
	return m
}

// spaceKey is a real space keypress as bubbletea delivers it (KeySpace with the
// rune populated); key.String() is " ".
var spaceKey = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}

func TestBrowseButtonOpensPicker(t *testing.T) {
	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})   // Local input (slot 1)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // → Local Browse button (slot 2)
	if !m.form.currentIsButton() || m.form.focusField() != 1 {
		t.Fatalf("want focus on the Local Browse button, got focus %d", m.form.focus)
	}
	m = update(t, m, spaceKey)
	if !m.form.browsing {
		t.Fatal("space on a Browse button should open the picker")
	}
	if info, err := os.Stat(m.form.picker.dir); err != nil || !info.IsDir() {
		t.Fatalf("picker should start in an existing directory, got %q", m.form.picker.dir)
	}
}

func TestCtrlOIsInert(t *testing.T) {
	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // Local input
	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlO})
	if m.form.browsing {
		t.Fatal("ctrl+o should no longer open the picker")
	}
}

func TestSpaceOnInputTypesSpace(t *testing.T) {
	m := openAddForm(t) // focus starts on the Name input (slot 0)
	m = typeRunes(t, m, "ab")
	m = update(t, m, spaceKey)
	m = typeRunes(t, m, "cd")
	if got := m.form.inputs[0].Value(); got != "ab cd" {
		t.Fatalf("name value = %q, want %q", got, "ab cd")
	}
	if m.form.browsing {
		t.Fatal("space in a text field must not open the picker")
	}
}

func TestPickerCancelButtonKeepsValue(t *testing.T) {
	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // Local input
	m.form.inputs[1].SetValue("/some/typed/path")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // → Local Browse button
	m = update(t, m, spaceKey)                       // open
	if !m.form.browsing {
		t.Fatal("picker should be open")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // → Select button
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // → Cancel button
	if m.form.picker.focus != focusCancel {
		t.Fatalf("two tabs from the list should focus Cancel, got %d", m.form.picker.focus)
	}
	m = update(t, m, spaceKey) // activate Cancel
	if m.form.browsing {
		t.Fatal("Cancel should close the picker")
	}
	if m.mode != modeForm {
		t.Fatalf("cancel should return to the form, not leave it (mode %d)", m.mode)
	}
	if got := m.form.inputs[1].Value(); got != "/some/typed/path" {
		t.Fatalf("cancel must preserve the typed value, got %q", got)
	}
}

func TestPickerSelectFillsField(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}

	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})        // Local input
	m.form.inputs[1].SetValue(filepath.Join(dir, "nope")) // missing → picker lists dir
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})      // → Local Browse button
	m = update(t, m, spaceKey)                            // open picker (dir listed, "target" highlighted)
	if !m.form.browsing {
		t.Fatal("picker should be open")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // → Select button
	if m.form.picker.focus != focusSelect {
		t.Fatalf("tab should focus the Select button, got %d", m.form.picker.focus)
	}
	m = update(t, m, spaceKey) // space activates Select
	if m.form.browsing {
		t.Fatal("selecting should close the picker")
	}
	if got := m.form.inputs[1].Value(); got != target {
		t.Fatalf("selected folder should fill the field: got %q, want %q", got, target)
	}
	if m.form.focus != inputSlot(1) {
		t.Fatalf("focus should return to the Local input (slot %d), got %d", inputSlot(1), m.form.focus)
	}
}

// TestPickerEnterActivatesSelect guards enter activating the focused Select
// button, exactly like space does (TestPickerSelectFillsField's space case).
func TestPickerEnterActivatesSelect(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}

	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})        // Local input
	m.form.inputs[1].SetValue(filepath.Join(dir, "nope")) // missing → picker lists dir
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})      // → Local Browse button
	m = update(t, m, spaceKey)                            // open picker (dir listed, "target" highlighted)
	if !m.form.browsing {
		t.Fatal("picker should be open")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // → Select button
	if m.form.picker.focus != focusSelect {
		t.Fatalf("tab should focus the Select button, got %d", m.form.picker.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // enter activates Select
	if m.form.browsing {
		t.Fatal("selecting should close the picker")
	}
	if got := m.form.inputs[1].Value(); got != target {
		t.Fatalf("selected folder should fill the field: got %q, want %q", got, target)
	}
}

// TestPickerSpaceSelectsImmediately guards D8: space on the list selects the
// highlighted folder and closes the picker directly, matching its hint text,
// instead of only moving focus to the Select button.
func TestPickerSpaceSelectsImmediately(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}

	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})        // Local input
	m.form.inputs[1].SetValue(filepath.Join(dir, "nope")) // missing → picker lists dir
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})      // → Local Browse button
	m = update(t, m, spaceKey)                            // open picker (dir listed, "target" highlighted)
	if m.form.picker.focus != focusList {
		t.Fatalf("picker should open with the list focused, got %d", m.form.picker.focus)
	}
	m = update(t, m, spaceKey) // space in the list selects immediately
	if m.form.browsing {
		t.Fatal("space in the list should close the picker")
	}
	if got := m.form.inputs[1].Value(); got != target {
		t.Fatalf("space should select the highlighted folder: got %q, want %q", got, target)
	}
}

func TestPickerEnterOpensFolder(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}

	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})        // Local input
	m.form.inputs[1].SetValue(filepath.Join(dir, "nope")) // missing → picker lists dir
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})      // → Local Browse button
	m = update(t, m, spaceKey)                            // open picker (dir listed, "target" highlighted)
	if !m.form.browsing {
		t.Fatal("picker should be open")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // enter descends into the highlighted folder
	if !m.form.browsing {
		t.Fatal("enter on the list should not close the picker")
	}
	if m.form.picker.dir != target {
		t.Fatalf("enter should descend into the highlighted folder: dir = %q, want %q", m.form.picker.dir, target)
	}
}

func TestPickerLeftGoesUp(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}
	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m.form.inputs[1].SetValue(target) // existing → opens in root, "target" highlighted
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	m = update(t, m, spaceKey) // open: dir == root
	if m.form.picker.dir != root {
		t.Fatalf("reopen should list the parent; dir = %q, want %q", m.form.picker.dir, root)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // open highlighted "target"
	if m.form.picker.dir != target {
		t.Fatalf("right should descend; dir = %q, want %q", m.form.picker.dir, target)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyLeft}) // up to parent
	if m.form.picker.dir != root {
		t.Fatalf("left should go up; dir = %q, want %q", m.form.picker.dir, root)
	}
	if m.form.browsing != true {
		t.Fatal("left in the list must not cancel the picker")
	}
}

// TestPickerEscCancelsFromList guards D7: esc while the list has focus cancels
// the whole picker (keeping the field's original value), instead of navigating
// up a directory — up-a-directory is still reachable via a/←, just not esc.
func TestPickerEscCancelsFromList(t *testing.T) {
	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // Local input
	m.form.inputs[1].SetValue("/some/typed/path")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // → Local Browse button
	m = update(t, m, spaceKey)                       // open (list focus)
	if m.form.picker.focus != focusList {
		t.Fatalf("setup: want list focus, got %d", m.form.picker.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.form.browsing {
		t.Fatal("esc on the list should cancel and close the picker")
	}
	if m.mode != modeForm {
		t.Fatalf("cancel should return to the form, not leave it (mode %d)", m.mode)
	}
	if got := m.form.inputs[1].Value(); got != "/some/typed/path" {
		t.Fatalf("esc-cancel must preserve the typed value, got %q", got)
	}
}

func TestPickerTabCyclesFocus(t *testing.T) {
	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	m = update(t, m, spaceKey) // open picker
	if m.form.picker.focus != focusList {
		t.Fatalf("picker should open with the list focused, got %d", m.form.picker.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.form.picker.focus != focusSelect {
		t.Fatalf("tab from the list should focus Select, got %d", m.form.picker.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.form.picker.focus != focusCancel {
		t.Fatalf("tab from Select should focus Cancel, got %d", m.form.picker.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.form.picker.focus != focusList {
		t.Fatalf("tab from Cancel should wrap to the list, got %d", m.form.picker.focus)
	}
}

func TestPickerButtonSwitchKeys(t *testing.T) {
	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	m = update(t, m, spaceKey)                     // open picker
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // → Select
	if m.form.picker.focus != focusSelect {
		t.Fatalf("setup: want focusSelect, got %d", m.form.picker.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if m.form.picker.focus != focusCancel {
		t.Fatalf("right on Select should focus Cancel, got %d", m.form.picker.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if m.form.picker.focus != focusSelect {
		t.Fatalf("left on Cancel should focus Select, got %d", m.form.picker.focus)
	}
}

// TestPickerEscCancelsFromButton guards D7: esc while a button (Select or
// Cancel) has focus also cancels the whole picker, so esc no longer flips
// meaning depending on which control is focused.
func TestPickerEscCancelsFromButton(t *testing.T) {
	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m.form.inputs[1].SetValue("/some/typed/path")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	m = update(t, m, spaceKey)                     // open picker
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // → Select
	if m.form.picker.focus != focusSelect {
		t.Fatalf("setup: want focusSelect, got %d", m.form.picker.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.form.browsing {
		t.Fatal("esc on a button should cancel and close the picker")
	}
	if got := m.form.inputs[1].Value(); got != "/some/typed/path" {
		t.Fatalf("esc-cancel must preserve the typed value, got %q", got)
	}
}

func TestPickerPageKeysMove5(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m.form.inputs[1].SetValue(filepath.Join(dir, "nope")) // missing child → picker lists dir directly
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	m = update(t, m, spaceKey) // open picker listing dir's 7 subdirs
	if len(m.form.picker.entries) != 7 {
		t.Fatalf("setup: want 7 entries, got %d: %v", len(m.form.picker.entries), m.form.picker.entries)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.form.picker.cursor != 5 {
		t.Fatalf("pgdown should move the cursor by 5, got %d", m.form.picker.cursor)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.form.picker.cursor != 0 {
		t.Fatalf("pgup should move the cursor by -5, got %d", m.form.picker.cursor)
	}
}

func TestPickerReopenHighlightsInParent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}
	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m.form.inputs[1].SetValue(target)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	m = update(t, m, spaceKey)
	if m.form.picker.dir != root {
		t.Fatalf("dir = %q, want parent %q", m.form.picker.dir, root)
	}
	if got := m.form.picker.entries[m.form.picker.cursor]; got != "target" {
		t.Fatalf("reopen should highlight %q, cursor on %q", "target", got)
	}
}

func TestNearestExistingDir(t *testing.T) {
	dir := t.TempDir()
	if got := nearestExistingDir(dir); got != dir {
		t.Fatalf("existing dir: got %q, want %q", got, dir)
	}
	missing := filepath.Join(dir, "nope", "deeper")
	if got := nearestExistingDir(missing); got != dir {
		t.Fatalf("nearest ancestor: got %q, want %q", got, dir)
	}
}

func TestPickerStart(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "child")
	if err := os.Mkdir(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()

	tests := []struct {
		name, in, wantDir, wantHi string
	}{
		{"existing dir shown in parent", sub, dir, "child"},
		{"trailing slash still resolves to parent", sub + "/", dir, "child"},
		{"missing path lists nearest ancestor", filepath.Join(sub, "gone"), sub, ""},
		{"empty falls back to home", "", home, ""},
		{"filesystem root", "/", "/", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotDir, gotHi := pickerStart(tc.in)
			if gotDir != tc.wantDir || gotHi != tc.wantHi {
				t.Fatalf("pickerStart(%q) = (%q,%q), want (%q,%q)", tc.in, gotDir, gotHi, tc.wantDir, tc.wantHi)
			}
		})
	}
}

func TestReadSubdirs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b", "a", ".hidden"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := readSubdirs(dir)
	want := []string{"a", "b"} // sorted, no dotdir, no file
	if len(got) != len(want) || got[0] != "a" || got[1] != "b" {
		t.Fatalf("readSubdirs = %v, want %v", got, want)
	}
}

func TestIndexOf(t *testing.T) {
	ss := []string{"a", "b", "c"}
	if got := indexOf(ss, "b"); got != 1 {
		t.Fatalf("indexOf found = %d, want 1", got)
	}
	if got := indexOf(ss, "zzz"); got != 0 {
		t.Fatalf("indexOf absent = %d, want 0", got)
	}
}

func TestDirPickerMoveClamps(t *testing.T) {
	p := dirPicker{entries: []string{"a", "b", "c"}, height: 10}
	p.moveUp() // already at top
	if p.cursor != 0 {
		t.Fatalf("moveUp at top: cursor = %d, want 0", p.cursor)
	}
	p.moveDown()
	p.moveDown()
	p.moveDown() // would be 3, clamps to 2
	if p.cursor != 2 {
		t.Fatalf("moveDown clamps: cursor = %d, want 2", p.cursor)
	}
	p.moveUp()
	if p.cursor != 1 {
		t.Fatalf("moveUp: cursor = %d, want 1", p.cursor)
	}
}

func TestDirPickerEnsureVisibleScrolls(t *testing.T) {
	entries := make([]string, 20)
	for i := range entries {
		entries[i] = string(rune('a' + i))
	}
	p := dirPicker{entries: entries, height: 5}
	for i := 0; i < 7; i++ {
		p.moveDown() // cursor 7, window height 5 -> offset must follow
	}
	if p.cursor != 7 {
		t.Fatalf("cursor = %d, want 7", p.cursor)
	}
	if p.offset != 3 { // cursor - height + 1 = 7 - 5 + 1
		t.Fatalf("offset = %d, want 3", p.offset)
	}
	for i := 0; i < 7; i++ {
		p.moveUp() // back to cursor 0
	}
	if p.offset != 0 {
		t.Fatalf("offset after scrolling back = %d, want 0", p.offset)
	}
}

func TestDirPickerSetHeight(t *testing.T) {
	p := dirPicker{entries: make([]string, 20), height: 5, cursor: 10, offset: 6}
	p.setHeight(4)
	if p.height != 4 {
		t.Fatalf("height = %d, want 4", p.height)
	}
	if p.cursor < p.offset || p.cursor >= p.offset+p.height {
		t.Fatalf("cursor %d not visible in [%d,%d)", p.cursor, p.offset, p.offset+p.height)
	}
}

func TestDirPickerOpen(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	inner := filepath.Join(target, "inner")
	if err := os.MkdirAll(inner, 0o750); err != nil {
		t.Fatal(err)
	}
	p := dirPicker{dir: root, entries: readSubdirs(root), height: 10}
	p.open() // descend into "target"
	if p.dir != target {
		t.Fatalf("open: dir = %q, want %q", p.dir, target)
	}
	if len(p.entries) != 1 || p.entries[0] != "inner" {
		t.Fatalf("open: entries = %v, want [inner]", p.entries)
	}
	if p.cursor != 0 {
		t.Fatalf("open: cursor = %d, want 0", p.cursor)
	}
}

func TestDirPickerOpenEmptyNoop(t *testing.T) {
	p := dirPicker{dir: "/", entries: nil, height: 10}
	p.open()
	if p.dir != "/" {
		t.Fatalf("open on empty list changed dir to %q", p.dir)
	}
}

func TestDirPickerUpDirHighlightsChild(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	sibling := filepath.Join(root, "sibling")
	for _, d := range []string{target, sibling} {
		if err := os.Mkdir(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	p := dirPicker{dir: target, entries: readSubdirs(target), height: 10}
	p.upDir()
	if p.dir != root {
		t.Fatalf("upDir: dir = %q, want %q", p.dir, root)
	}
	if p.entries[p.cursor] != "target" {
		t.Fatalf("upDir should highlight the directory just left; cursor on %q", p.entries[p.cursor])
	}
}

func TestDirPickerMoveByPage(t *testing.T) {
	p := dirPicker{entries: make([]string, 20), height: 5}
	p.moveBy(10)
	if p.cursor != 10 {
		t.Fatalf("moveBy(10): cursor = %d, want 10", p.cursor)
	}
	if p.cursor < p.offset || p.cursor >= p.offset+p.height {
		t.Fatalf("cursor %d not visible in [%d,%d)", p.cursor, p.offset, p.offset+p.height)
	}
	p.moveBy(10)
	if p.cursor != 19 {
		t.Fatalf("moveBy(10) again should clamp: cursor = %d, want 19", p.cursor)
	}
	p.moveBy(-100)
	if p.cursor != 0 {
		t.Fatalf("moveBy(-100) should clamp: cursor = %d, want 0", p.cursor)
	}
}

func TestEllipsisLeft(t *testing.T) {
	if got := ellipsisLeft("short", 10); got != "short" {
		t.Fatalf("short unchanged: got %q", got)
	}
	got := ellipsisLeft("/home/andres/projects/netcheckout", 12)
	if lipgloss.Width(got) > 12 {
		t.Fatalf("too wide: %q (%d)", got, lipgloss.Width(got))
	}
	if !strings.HasPrefix(got, "…") {
		t.Fatalf("want left ellipsis, got %q", got)
	}
	if !strings.HasSuffix(got, "netcheckout") {
		t.Fatalf("want tail preserved, got %q", got)
	}
}

func TestDirPickerViewContent(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"alpha", "beta"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	p := dirPicker{dir: root, entries: readSubdirs(root), height: 6}
	out := p.view(50)
	for _, want := range []string{
		filepath.Base(root), "alpha/", "beta/",
		"Move", "Open", "Cancel", "Buttons",
		"[ Select ]", "[ Cancel ]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q in:\n%s", want, out)
		}
	}

	// Empty directory shows the placeholder.
	empty := dirPicker{dir: t.TempDir(), height: 6}
	if !strings.Contains(empty.view(50), "no sub-folders") {
		t.Fatal("empty dir should show the (no sub-folders) placeholder")
	}
}

func TestPickerSelectEmptyDirUsesCurrentDir(t *testing.T) {
	empty := t.TempDir() // no sub-directories
	m := openAddForm(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m.form.inputs[1].SetValue(filepath.Join(empty, "nope")) // missing → picker lists `empty`
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	m = update(t, m, spaceKey) // open (empty list)
	if len(m.form.picker.entries) != 0 {
		t.Fatalf("want empty entries, got %v", m.form.picker.entries)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // → Select button
	m = update(t, m, spaceKey)                     // select with empty list → current dir
	if m.form.browsing {
		t.Fatal("select should close the picker")
	}
	if got := m.form.inputs[1].Value(); got != empty {
		t.Fatalf("empty-dir select should fill the current dir: got %q, want %q", got, empty)
	}
}

func TestReadSubdirsFollowsSymlinkedDirs(t *testing.T) {
	dir := t.TempDir()
	target := t.TempDir() // symlink target, outside dir

	if err := os.Mkdir(filepath.Join(dir, "real"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "linkdir")); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file, filepath.Join(dir, "linkfile")); err != nil {
		t.Fatal(err)
	}

	got := readSubdirs(dir)
	// A symlink to a directory ("linkdir") is followed and listed; a plain file, a
	// symlink to a file ("linkfile"), and dot-entries are not.
	if len(got) != 2 || got[0] != "linkdir" || got[1] != "real" {
		t.Fatalf("readSubdirs = %v, want [linkdir real]", got)
	}
}
