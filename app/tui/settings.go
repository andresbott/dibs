package tui

import (
	"context"
	"strings"

	"github.com/andresbott/netcheckout/internal/config"
	"github.com/andresbott/netcheckout/internal/ident"
	"github.com/andresbott/netcheckout/libs/threewayrsync"
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

// settingsModel is the "Client settings" modal: one text input per
// settingsField, followed by the Save and Cancel action buttons. focus indexes
// the inputs first, then Save (saveSlot), then Cancel (cancelSlot).
type settingsModel struct {
	inputs    []textinput.Model
	focus     int
	err       string
	width     int  // terminal width last passed to setWidth
	mandatory bool // opened as the blocking first-run dialog; Cancel/Esc quits the app
}

func (s settingsModel) saveSlot() int   { return len(s.inputs) }
func (s settingsModel) cancelSlot() int { return len(s.inputs) + 1 }
func (s settingsModel) numSlots() int   { return len(s.inputs) + 2 }

// onInput reports whether focus is on a text input (as opposed to a button).
func (s settingsModel) onInput() bool { return s.focus < len(s.inputs) }

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

// setFocus moves focus to slot i (wrapping), focusing the matching input and
// blurring the rest.
func (s *settingsModel) setFocus(i int) tea.Cmd {
	n := s.numSlots()
	i = (i%n + n) % n
	s.focus = i
	var cmd tea.Cmd
	for j := range s.inputs {
		if j == i {
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
	i := s.focus
	s.inputs[i], cmd = s.inputs[i].Update(msg)
	return s, cmd
}

// underline renders input i as its value over a single bottom-border line,
// accent-coloured when focused, dim otherwise. Mirrors formModel.underline.
func (s settingsModel) underline(i int) string {
	c := colDim
	if s.focus == i {
		c = colAccent
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false). // bottom only
		BorderForeground(c).
		Width(s.fieldWidth()).
		Render(s.inputs[i].View())
}

func (s settingsModel) View() string {
	var content strings.Builder
	for i, f := range settingsFields {
		if i > 0 {
			content.WriteString("\n")
		}
		label := labelStyle
		if s.focus == i {
			label = focusLabelStyle
		}
		content.WriteString(label.Render(f.label))
		content.WriteString("\n")
		content.WriteString(s.underline(i))
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
