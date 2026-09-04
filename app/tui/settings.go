package tui

import (
	"context"
	"strings"

	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/internal/ident"
	"github.com/andresbott/dibs/libs/threewayrsync"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// settingsField describes one client-level config field the settings modal can
// edit: a display label and the getter/setter pair mapping it to a Config. This
// slice is the single place to add future client-level settings.
type settingsField struct {
	label    string
	get      func(*config.Config) string
	set      func(*config.Config, string)
	validate func(string) error // optional; nil means no validation
}

var settingsFields = []settingsField{
	{
		label:    "Identity",
		get:      func(c *config.Config) string { return c.Identity },
		set:      func(c *config.Config, v string) { c.Identity = v },
		validate: config.ValidateIdentity,
	},
	{
		label:    "Rsync path",
		get:      func(c *config.Config) string { return c.RsyncPath },
		set:      func(c *config.Config, v string) { c.RsyncPath = v },
		validate: validateRsyncPath,
	},
	{
		label:    defaultLocalRootLabel,
		get:      func(c *config.Config) string { return c.DefaultLocalRoot },
		set:      func(c *config.Config, v string) { c.DefaultLocalRoot = v },
		validate: config.ValidateDefaultLocalRoot,
	},
}

// validateRsyncPath gates saving a non-empty rsync override on the binary actually
// being a usable GNU rsync, so a typo'd path or an openrsync can never be persisted.
// Empty is allowed — it means "rsync" from PATH — and is re-verified after save by
// the startup-style check submitSettings fires. Running `--version` synchronously
// here is fine: it is a milliseconds-fast local exec.
func validateRsyncPath(v string) error {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	_, err := (&threewayrsync.Syncer{Bin: v}).CheckBinary(context.Background())
	return err
}

// defaultLocalRootLabel is the settingsField label of the path field that gets
// an inline Browse button (the one place the label is matched, so a rename only
// needs updating here).
const defaultLocalRootLabel = "Default local root"

// defaultLocalRootIdx returns the input index of the Default local root field,
// or -1 if it is not present.
func defaultLocalRootIdx() int {
	for i, f := range settingsFields {
		if f.label == defaultLocalRootLabel {
			return i
		}
	}
	return -1
}

// settingsSlotKind is the kind of a settings Tab stop.
type settingsSlotKind int

const (
	stInput  settingsSlotKind = iota // a text input
	stBrowse                         // the Default local root's Browse button
	stSave
	stCancel
)

// settingsSlot is one Tab stop: its kind and, for input/browse slots, the input
// index it belongs to (-1 for the action buttons).
type settingsSlot struct {
	kind  settingsSlotKind
	field int
}

// settingsModel is the "Client settings" modal: one text input per
// settingsField, an inline Browse button beside the Default local root, then the
// Save and Cancel action buttons. focus indexes slots() — the inputs (each
// followed by its Browse button, if any), then Save, then Cancel.
type settingsModel struct {
	inputs     []textinput.Model
	focus      int
	err        string
	width      int  // terminal width last passed to setWidth
	termHeight int  // terminal height, for sizing the picker
	mandatory  bool // opened as the blocking first-run dialog; Cancel/Esc quits the app

	picker   dirPicker // directory picker for the Default local root
	browsing bool      // true while the directory picker is open
}

// slots is the Tab order: each input in turn (the Default local root input
// followed by its Browse button), then Save, then Cancel.
func (s settingsModel) slots() []settingsSlot {
	dlr := defaultLocalRootIdx()
	out := make([]settingsSlot, 0, len(s.inputs)+3)
	for i := range s.inputs {
		out = append(out, settingsSlot{stInput, i})
		if i == dlr {
			out = append(out, settingsSlot{stBrowse, i})
		}
	}
	return append(out, settingsSlot{stSave, -1}, settingsSlot{stCancel, -1})
}

func (s settingsModel) slotIndex(kind settingsSlotKind) int {
	for i, sl := range s.slots() {
		if sl.kind == kind {
			return i
		}
	}
	return -1
}

func (s settingsModel) saveSlot() int   { return s.slotIndex(stSave) }
func (s settingsModel) cancelSlot() int { return s.slotIndex(stCancel) }
func (s settingsModel) browseSlot() int { return s.slotIndex(stBrowse) }
func (s settingsModel) numSlots() int   { return len(s.slots()) }

// focusKind is the kind of the currently focused slot.
func (s settingsModel) focusKind() settingsSlotKind { return s.slots()[s.focus].kind }

// focusField is the input index the focused slot belongs to (its own for an
// input, the buttoned field for the Browse button, -1 for the actions).
func (s settingsModel) focusField() int { return s.slots()[s.focus].field }

// onInput reports whether focus is on a text input (as opposed to a button).
func (s settingsModel) onInput() bool { return s.focusKind() == stInput }

// atInputEnd reports whether the focused input's cursor sits at the end of its
// text — the point at which Right leaves the field for its Browse button.
func (s settingsModel) atInputEnd() bool {
	in := s.inputs[s.focusField()]
	return in.Position() == len([]rune(in.Value()))
}

// newSettings builds the modal for cfg: one input per field prefilled from cfg,
// with the Identity field's placeholder set to the resolved default so an empty
// value reads as "using $USER@$HOSTNAME". The first input is focused.
func newSettings(cfg *config.Config) settingsModel {
	inputs := make([]textinput.Model, len(settingsFields))
	for i, f := range settingsFields {
		in := textinput.New()
		in.CharLimit = 128
		// Match newForm: drop the "> " prompt and placeholder so the underline
		// fills gap-free.
		in.Prompt = ""
		in.Placeholder = ""
		val := f.get(cfg)
		if f.label == "Identity" {
			if id, err := ident.Resolve(cfg); err == nil {
				in.Placeholder = id.By
				if strings.TrimSpace(val) == "" {
					val = id.By // prefill resolved default so first-run Save is valid
				}
			}
		}
		if f.label == "Rsync path" {
			in.Placeholder = "rsync (from PATH)"
		}
		if f.label == defaultLocalRootLabel {
			in.Placeholder = "~/dibs (optional)"
		}
		in.SetValue(val)
		inputs[i] = in
	}
	s := settingsModel{inputs: inputs}
	if len(s.inputs) > 0 {
		s.inputs[0].Focus()
	}
	return s
}

// modalWidth is the settings window's total width: capped, and clamped to a
// sensible minimum for narrow terminals. Mirrors formModel.modalWidth.
func (s settingsModel) modalWidth() int {
	w := s.width - 8
	if w > 60 {
		w = 60
	}
	if w < 30 {
		w = 30
	}
	return w
}

// fieldWidth is the display width of an input's underline: the content budget
// less the modal borders and body padding.
func (s settingsModel) fieldWidth() int {
	w := s.modalWidth() - 4
	if w < 6 {
		w = 6
	}
	return w
}

// setWidth records the terminal width and sizes each input to fit within its
// underline (reserving one cell for textinput's trailing cursor).
func (s *settingsModel) setWidth(w int) {
	s.width = w
	iw := s.fieldWidth() - 1
	if iw < 6 {
		iw = 6
	}
	for i := range s.inputs {
		s.inputs[i].Width = iw
	}
}

// setFocus moves focus to slot i (wrapping), focusing the input that slot maps
// to (if any) and blurring the rest.
func (s *settingsModel) setFocus(i int) tea.Cmd {
	n := s.numSlots()
	i = (i%n + n) % n
	s.focus = i
	sl := s.slots()[i]
	field := -1
	if sl.kind == stInput {
		field = sl.field
	}
	var cmd tea.Cmd
	for j := range s.inputs {
		if j == field {
			cmd = s.inputs[j].Focus()
		} else {
			s.inputs[j].Blur()
		}
	}
	return cmd
}

func (s *settingsModel) focusNext() tea.Cmd { return s.setFocus(s.focus + 1) }
func (s *settingsModel) focusPrev() tea.Cmd { return s.setFocus(s.focus - 1) }

// apply writes the (trimmed) input values into cfg. An empty Identity is
// allowed — it means "use the $USER@$HOSTNAME default".
func (s settingsModel) apply(cfg *config.Config) {
	for i, f := range settingsFields {
		f.set(cfg, strings.TrimSpace(s.inputs[i].Value()))
	}
}

// validate runs each field's optional validator against its trimmed input value,
// returning the first error. It gates submitSettings so an invalid config (e.g. a
// blank Identity) is never written to disk.
func (s settingsModel) validate() error {
	for i, f := range settingsFields {
		if f.validate == nil {
			continue
		}
		if err := f.validate(strings.TrimSpace(s.inputs[i].Value())); err != nil {
			return err
		}
	}
	return nil
}

// update forwards a message to the focused input (no-op on the buttons, which
// don't consume keystrokes).
func (s settingsModel) update(msg tea.Msg) (settingsModel, tea.Cmd) {
	if !s.onInput() {
		return s, nil
	}
	var cmd tea.Cmd
	i := s.focusField()
	s.inputs[i], cmd = s.inputs[i].Update(msg)
	return s, cmd
}

// inputFocused reports whether the text input for field i currently has focus.
func (s settingsModel) inputFocused(i int) bool { return s.onInput() && s.focusField() == i }

// underline renders input i as its value over a single bottom-border line,
// accent-coloured when focused, dim otherwise. Mirrors formModel.underline. The
// Default local root reserves room for its inline Browse button so the underline
// does not span the button.
func (s settingsModel) underline(i int) string {
	c := colDim
	if s.inputFocused(i) {
		c = colAccent
	}
	w := s.fieldWidth()
	if i == defaultLocalRootIdx() {
		w -= browseButtonWidth
		if w < 6 {
			w = 6
		}
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false). // bottom only
		BorderForeground(c).
		Width(w).
		Render(s.inputs[i].View())
}

// browseButtonWidth is the horizontal budget the inline "[ Browse ]" control
// (plus its leading space) takes from the Default local root's underline.
const browseButtonWidth = 11

// browseButton renders the Default local root's inline Browse button, accent +
// bold when it is the focused slot, dim otherwise.
func (s settingsModel) browseButton() string {
	st := lipgloss.NewStyle().Foreground(colDim)
	if s.focusKind() == stBrowse {
		st = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	}
	return st.Render("[ Browse ]")
}

// fieldRow renders field i's editable area: the underline, plus the inline
// Browse button for the Default local root.
func (s settingsModel) fieldRow(i int) string {
	if i != defaultLocalRootIdx() {
		return s.underline(i)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, s.underline(i), " ", s.browseButton())
}

func (s settingsModel) View() string {
	if s.browsing {
		return s.pickerView()
	}
	var content strings.Builder
	for i, f := range settingsFields {
		if i > 0 {
			content.WriteString("\n")
		}
		label := labelStyle
		if s.inputFocused(i) {
			label = focusLabelStyle
		}
		content.WriteString(label.Render(f.label))
		content.WriteString("\n")
		content.WriteString(s.fieldRow(i))
	}

	// Centered Save / Cancel action row. The second button (and the esc hint) read
	// "Quit" for the mandatory first-run dialog, where Cancel exits the app.
	cancelLabel := "Cancel"
	if s.mandatory {
		cancelLabel = "Quit"
	}
	content.WriteString("\n\n")
	actions := lipgloss.JoinHorizontal(lipgloss.Top,
		confirmButton("Save", s.focus == s.saveSlot()), "   ",
		confirmButton(cancelLabel, s.focus == s.cancelSlot()))
	content.WriteString(lipgloss.NewStyle().Width(s.modalWidth() - 4).Align(lipgloss.Center).Render(actions))

	if s.err != "" {
		content.WriteString("\n\n")
		content.WriteString(errStyle.Render(s.err))
	}

	content.WriteString("\n\n")
	sep := helpTextStyle.Render(" · ")
	content.WriteString(hint("tab", "Move") + sep + hint("enter/space", "Activate") + sep + hint("esc", cancelLabel))

	body := lipgloss.NewStyle().Padding(0, 1).Render(content.String())
	return titledBox("Client settings", body, s.modalWidth(), lipgloss.Height(body)+2, true)
}
