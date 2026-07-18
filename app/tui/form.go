package tui

import (
	"strings"

	"github.com/andresbott/netcheckout/internal/config"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type slotKind int

const (
	slotInput slotKind = iota
	slotButton
	slotSave
	slotCancel
)

// focusSlot is one Tab stop, positioned on a grid: row is its line in the form
// (Name=0, Local=1, Remote=2, action row=3) and col is 0 (inputs / Save) or 1
// (Browse buttons / Cancel). field indexes formModel.inputs (-1 for the actions).
type focusSlot struct {
	kind  slotKind
	field int
	row   int
	col   int
}

// formSlots is the fixed Tab order (and the grid). Every row has a column-0 cell,
// so Down/Up can always fall back to column 0 when a row lacks column 1.
var formSlots = []focusSlot{
	{slotInput, 0, 0, 0},   // Name
	{slotInput, 1, 1, 0},   // Local root input
	{slotButton, 1, 1, 1},  // Local Browse
	{slotInput, 2, 2, 0},   // Remote root input
	{slotButton, 2, 2, 1},  // Remote Browse
	{slotSave, -1, 3, 0},   // Save
	{slotCancel, -1, 3, 1}, // Cancel
}

// focusField is the input index the current slot belongs to (its own index for an
// input slot, the browsed field for a button slot).
func (f formModel) focusField() int { return formSlots[f.focus].field }

// currentIsButton reports whether the focused slot is a Browse button.
func (f formModel) currentIsButton() bool { return formSlots[f.focus].kind == slotButton }

// atInputEnd reports whether the focused input's cursor sits at the end of its
// text — the point at which Right leaves the field for its Browse button.
func (f formModel) atInputEnd() bool {
	in := f.inputs[f.focusField()]
	return in.Position() == len([]rune(in.Value()))
}

// inputSlot returns the slot index of a field's text input (used to return focus
// to the input after a browse).
func inputSlot(field int) int {
	for i, s := range formSlots {
		if s.kind == slotInput && s.field == field {
			return i
		}
	}
	return 0
}

// buttonSlot returns the slot index of a path field's Browse button, or -1 if the
// field has none (the Name field).
func buttonSlot(field int) int {
	for i, s := range formSlots {
		if s.kind == slotButton && s.field == field {
			return i
		}
	}
	return -1
}

// actionSlot returns the slot index of the Save or Cancel button.
func actionSlot(kind slotKind) int {
	for i, s := range formSlots {
		if s.kind == kind {
			return i
		}
	}
	return -1
}

// numRows is the number of grid rows (Name, each path field, and the action row).
func numRows() int {
	max := 0
	for _, s := range formSlots {
		if s.row > max {
			max = s.row
		}
	}
	return max + 1
}

// slotAt returns the slot index at grid cell (row, col), or -1 if empty.
func slotAt(row, col int) int {
	for i, s := range formSlots {
		if s.row == row && s.col == col {
			return i
		}
	}
	return -1
}

type formModel struct {
	inputs     []textinput.Model
	focus      int
	origName   string // "" for add; the existing name for edit
	err        string
	width      int       // terminal width last passed to setWidth
	termHeight int       // terminal height, for sizing the picker
	picker     dirPicker // directory picker for the focused path field
	browsing   bool      // true while the directory picker is open
	// subpaths retains the edited profile's subpaths. The form has no subpaths input,
	// so it carries them through unchanged — otherwise an edit-and-save would wipe
	// subpaths authored in the config YAML.
	subpaths []string
}

func newForm(origName string, p config.Profile) formModel {
	name := textinput.New()
	name.CharLimit = 64
	name.SetValue(origName)

	local := textinput.New()
	local.SetValue(p.LocalRoot)

	remote := textinput.New()
	remote.SetValue(p.RemoteRoot)

	inputs := []textinput.Model{name, local, remote}
	for i := range inputs {
		// Drop the default "> " prompt and the placeholder. The labels already
		// name each field, and an empty placeholder lets textinput fill the whole
		// field with its (background-styled) trailing padding, so the bar renders
		// gap-free (the placeholder path leaves that padding unstyled).
		inputs[i].Prompt = ""
		inputs[i].Placeholder = ""
	}

	f := formModel{
		inputs:   inputs,
		origName: origName,
		subpaths: p.Subpaths,
	}
	f.inputs[0].Focus()
	return f
}

// focusNext / focusPrev cycle through every slot in Tab order — Name, each path
// input, and each Browse button — wrapping. Bound to Tab / Shift+Tab.
func (f *formModel) focusNext() tea.Cmd { return f.setFocus(f.focus + 1) }
func (f *formModel) focusPrev() tea.Cmd { return f.setFocus(f.focus - 1) }

// focusNextField / focusPrevField move focus one step down / up within the
// currently focused column, wrapping: the input fields (Name → Local → Remote)
// when on an input, or the Browse buttons (Local → Remote) when on a button — so
// vertical nav keeps you in the same column. Crossing columns is Left/Right.
// Bound to Down / Up.
func (f *formModel) focusNextField() tea.Cmd { return f.stepField(1) }
func (f *formModel) focusPrevField() tea.Cmd { return f.stepField(-1) }

// stepField moves focus dir (+1 down / -1 up) to the adjacent grid row, staying
// in the same column when that row has a cell there, else falling to column 0
// (every row has one). Rows wrap.
func (f *formModel) stepField(dir int) tea.Cmd {
	cur := formSlots[f.focus]
	n := numRows()
	targetRow := ((cur.row+dir)%n + n) % n
	if s := slotAt(targetRow, cur.col); s >= 0 {
		return f.setFocus(s)
	}
	return f.setFocus(slotAt(targetRow, 0))
}

// modalWidth is the form window's total width: capped, and clamped to a
// sensible minimum for narrow terminals.
func (f formModel) modalWidth() int {
	w := f.width - 8
	if w > 100 {
		w = 100
	}
	if w < 30 {
		w = 30
	}
	return w
}

// setWidth records the terminal width and sizes each input to fit within its
// underline (see fieldWidth / underline).
func (f *formModel) setWidth(w int) {
	f.width = w
	for i := range f.inputs {
		// The underline pads the value row to fieldWidth(i); reserve one cell for
		// textinput's trailing cursor so its View never overflows and truncates.
		iw := f.fieldWidth(i) - 1
		if iw < 6 {
			iw = 6
		}
		f.inputs[i].Width = iw
	}
}

func (f *formModel) setFocus(i int) tea.Cmd {
	n := len(formSlots)
	i = (i%n + n) % n
	f.focus = i
	s := formSlots[i]
	var cmd tea.Cmd
	for j := range f.inputs {
		if s.kind == slotInput && s.field == j {
			cmd = f.inputs[j].Focus()
		} else {
			f.inputs[j].Blur()
		}
	}
	return cmd
}

// navKey handles the focus-movement keys (Tab/Shift+Tab and the arrows),
// mutating focus and returning the resulting cmd. ok is false when key is not a
// navigation key, so the caller can handle it (activation or text input).
func (f *formModel) navKey(key string) (cmd tea.Cmd, ok bool) {
	switch key {
	case "tab":
		return f.focusNext(), true
	case "shift+tab":
		return f.focusPrev(), true
	case "down":
		return f.focusNextField(), true
	case "up":
		return f.focusPrevField(), true
	case "right":
		// On Save → Cancel; on a path input with the cursor at the end → its
		// Browse button; otherwise not a nav key (let the input move its cursor).
		if formSlots[f.focus].kind == slotSave {
			return f.setFocus(actionSlot(slotCancel)), true
		}
		if formSlots[f.focus].kind == slotInput && buttonSlot(f.focusField()) >= 0 && f.atInputEnd() {
			return f.setFocus(buttonSlot(f.focusField())), true
		}
	case "left":
		// On Cancel → Save; on a Browse button → its input; otherwise not a nav
		// key (let the input move its cursor).
		if formSlots[f.focus].kind == slotCancel {
			return f.setFocus(actionSlot(slotSave)), true
		}
		if formSlots[f.focus].kind == slotButton {
			return f.setFocus(inputSlot(f.focusField())), true
		}
	}
	return nil, false
}

func (f formModel) updateInputs(msg tea.Msg) (formModel, tea.Cmd) {
	if formSlots[f.focus].kind != slotInput {
		return f, nil // only text inputs consume keystrokes (buttons are inert)
	}
	var cmd tea.Cmd
	field := f.focusField()
	f.inputs[field], cmd = f.inputs[field].Update(msg)
	return f, cmd
}

func (f formModel) values() (string, config.Profile) {
	return strings.TrimSpace(f.inputs[0].Value()),
		config.Profile{
			LocalRoot:  strings.TrimSpace(f.inputs[1].Value()),
			RemoteRoot: strings.TrimSpace(f.inputs[2].Value()),
			Subpaths:   f.subpaths,
		}
}

func (f formModel) View() string {
	if f.browsing {
		return f.pickerView()
	}
	title := "Add profile"
	if f.origName != "" {
		title = "Edit profile: " + f.origName
	}

	var content strings.Builder
	labels := []string{"Name", "Local root", "Remote root"}
	for i := range f.inputs {
		if i > 0 {
			content.WriteString("\n")
		}
		label := labelStyle
		if f.focusField() == i {
			label = focusLabelStyle
		}
		content.WriteString(label.Render(labels[i]))
		content.WriteString("\n")
		content.WriteString(f.fieldRow(i))
	}

	// Centered Save / Cancel action row.
	content.WriteString("\n\n")
	actions := lipgloss.JoinHorizontal(lipgloss.Top,
		f.actionButton(slotSave, "Save"), "   ", f.actionButton(slotCancel, "Cancel"))
	content.WriteString(lipgloss.NewStyle().Width(f.modalWidth() - 4).Align(lipgloss.Center).Render(actions))

	if f.err != "" {
		content.WriteString("\n\n")
		content.WriteString(errStyle.Render(f.err))
	}

	content.WriteString("\n\n")
	sep := helpTextStyle.Render(" · ")
	content.WriteString(hint("tab", "Move") + sep + hint("enter/space", "Activate") + sep + hint("esc", "Cancel"))

	// Inset the content one column from the border on each side; the bar widths
	// already reserve those two columns (see setWidth).
	body := lipgloss.NewStyle().Padding(0, 1).Render(content.String())
	return titledBox(title, body, f.modalWidth(), lipgloss.Height(body)+2, true)
}

// fieldWidth is the display width of input i's underline. (Task 2 narrows the
// path fields to leave room for the Browse button.)
func (f formModel) fieldWidth(i int) int {
	w := f.modalWidth() - 4 // content budget: modal borders (2) + body padding (2)
	if i != 0 {
		w -= 11 // " [ Browse ]": 1-col gap + 10-col button
	}
	if w < 6 {
		w = 6
	}
	return w
}

// underline renders input i as its value over a single bottom-border line — the
// field affordance is an underline, not a box. The line is accent-coloured when
// the field is focused, dim otherwise.
func (f formModel) underline(i int) string {
	c := colDim
	if f.focus == inputSlot(i) {
		c = colAccent
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false). // bottom only
		BorderForeground(c).
		Width(f.fieldWidth(i)).
		Render(f.inputs[i].View())
}

// browseButton renders the inline [ Browse ] control for path field i: accent +
// bold when it is the focused slot, dim otherwise.
func (f formModel) browseButton(i int) string {
	st := lipgloss.NewStyle().Foreground(colDim)
	if f.currentIsButton() && f.focusField() == i {
		st = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	}
	return st.Render("[ Browse ]")
}

// actionButton renders a bracketed [ label ] action button (Save / Cancel):
// accent + bold when it is the focused slot, dim otherwise.
func (f formModel) actionButton(kind slotKind, label string) string {
	st := lipgloss.NewStyle().Foreground(colDim)
	if formSlots[f.focus].kind == kind {
		st = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	}
	return st.Render("[ " + label + " ]")
}

// fieldRow renders one field's editable area: the underlined input, plus the
// Browse button for the two path fields (Name has none).
func (f formModel) fieldRow(i int) string {
	if i == 0 {
		return f.underline(i)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, f.underline(i), " ", f.browseButton(i))
}
