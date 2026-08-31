package tui

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/andresbott/dibs/internal/config"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// serverFormModel is the server add/edit form: fixed labeled underline inputs
// for Name, Host, Port, User, and Password file, followed by Save and Cancel
// action buttons. Modeled on settingsModel.
type serverFormModel struct {
	inputs   []textinput.Model
	focus    int
	err      string
	width    int
	origName string // the original name when editing (empty when adding)
}

// Field indexes for serverFormModel.inputs.
const (
	srvName = iota
	srvHost
	srvPort
	srvUser
	srvPass
)

func (s serverFormModel) saveSlot() int   { return len(s.inputs) }
func (s serverFormModel) cancelSlot() int { return len(s.inputs) + 1 }
func (s serverFormModel) numSlots() int   { return len(s.inputs) + 2 }

// onInput reports whether focus is on a text input (as opposed to a button).
func (s serverFormModel) onInput() bool { return s.focus < len(s.inputs) }

// newServerForm builds the form for a server: one input per field, prefilled
// with the given server values. The first input is focused. origName is the
// server name when editing (used to allow renaming), or empty when adding.
func newServerForm(origName string, srv config.Server) serverFormModel {
	inputs := make([]textinput.Model, 5)
	for i := range inputs {
		inputs[i] = textinput.New()
		inputs[i].CharLimit = 128
		inputs[i].Prompt = ""
		inputs[i].Placeholder = ""
	}

	// Prefill values
	inputs[srvName].SetValue(origName)
	inputs[srvHost].SetValue(srv.Host)
	if srv.Port > 0 {
		inputs[srvPort].SetValue(strconv.Itoa(srv.Port))
	}
	inputs[srvUser].SetValue(srv.User)
	inputs[srvPass].SetValue(srv.PasswordFile)

	inputs[srvPort].Placeholder = "873 (default)"
	inputs[srvUser].Placeholder = "none"
	inputs[srvPass].Placeholder = "none"

	s := serverFormModel{inputs: inputs, origName: origName}
	if len(s.inputs) > 0 {
		s.inputs[0].Focus()
	}
	return s
}

// modalWidth caps the form window's width.
func (s serverFormModel) modalWidth() int {
	w := s.width - 8
	if w > 60 {
		w = 60
	}
	if w < 30 {
		w = 30
	}
	return w
}

// fieldWidth is the display width of an input's underline.
func (s serverFormModel) fieldWidth() int {
	w := s.modalWidth() - 4
	if w < 6 {
		w = 6
	}
	return w
}

// setWidth records the terminal width and sizes each input to fit within its
// underline.
func (s *serverFormModel) setWidth(w int) {
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
func (s *serverFormModel) setFocus(i int) tea.Cmd {
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

func (s *serverFormModel) focusNext() tea.Cmd { return s.setFocus(s.focus + 1) }
func (s *serverFormModel) focusPrev() tea.Cmd { return s.setFocus(s.focus - 1) }

// values returns the parsed (name, server) pair from the form's inputs.
// Trims all fields; port is parsed via strconv.Atoi (empty → 0).
func (s serverFormModel) values() (string, config.Server) {
	name := strings.TrimSpace(s.inputs[srvName].Value())
	host := strings.TrimSpace(s.inputs[srvHost].Value())
	portStr := strings.TrimSpace(s.inputs[srvPort].Value())
	user := strings.TrimSpace(s.inputs[srvUser].Value())
	passFile := strings.TrimSpace(s.inputs[srvPass].Value())

	port := 0
	if portStr != "" {
		port, _ = strconv.Atoi(portStr)
	}

	return name, config.Server{
		Host:         host,
		Port:         port,
		User:         user,
		PasswordFile: passFile,
	}
}

// validate runs validation on the form values: name via ValidateName, port
// must be numeric if non-empty, and server fields via ValidateServer.
func (s serverFormModel) validate() error {
	name, srv := s.values()

	if err := config.ValidateName(name); err != nil {
		return err
	}

	// Check for non-numeric port
	portStr := strings.TrimSpace(s.inputs[srvPort].Value())
	if portStr != "" {
		if _, err := strconv.Atoi(portStr); err != nil {
			return errors.New("port must be a number")
		}
	}

	return config.ValidateServer(srv)
}

// update forwards a message to the focused input (no-op on the buttons).
func (s serverFormModel) update(msg tea.Msg) (serverFormModel, tea.Cmd) {
	if !s.onInput() {
		return s, nil
	}
	var cmd tea.Cmd
	i := s.focus
	s.inputs[i], cmd = s.inputs[i].Update(msg)
	return s, cmd
}

// underline renders input i as its value over a single bottom-border line,
// accent-coloured when focused, dim otherwise.
func (s serverFormModel) underline(i int) string {
	c := colDim
	if s.focus == i {
		c = colAccent
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(c).
		Width(s.fieldWidth()).
		Render(s.inputs[i].View())
}

func (s serverFormModel) View() string {
	labels := []string{"Name", "Host", "Port", "User", "Password file"}
	var content strings.Builder
	for i, label := range labels {
		if i > 0 {
			content.WriteString("\n")
		}
		labelSt := labelStyle
		if s.focus == i {
			labelSt = focusLabelStyle
		}
		content.WriteString(labelSt.Render(label))
		content.WriteString("\n")
		content.WriteString(s.underline(i))
	}

	// Centered Save / Cancel action row.
	content.WriteString("\n\n")
	actions := lipgloss.JoinHorizontal(lipgloss.Top,
		confirmButton("Save", s.focus == s.saveSlot()), "   ",
		confirmButton("Cancel", s.focus == s.cancelSlot()))
	content.WriteString(lipgloss.NewStyle().Width(s.modalWidth() - 4).Align(lipgloss.Center).Render(actions))

	if s.err != "" {
		content.WriteString("\n\n")
		content.WriteString(errStyle.Render(s.err))
	}

	content.WriteString("\n\n")
	sep := helpTextStyle.Render(" · ")
	content.WriteString(hint("tab", "Move") + sep + hint("enter/space", "Activate") + sep + hint("esc", "Cancel"))

	title := "Add server"
	if s.origName != "" {
		title = "Edit server"
	}
	body := lipgloss.NewStyle().Padding(0, 1).Render(content.String())
	return titledBox(title, body, s.modalWidth(), lipgloss.Height(body)+2, true)
}

// serversModel is the servers list: names + cursor, mirroring listModel.
type serversModel struct {
	names  []string
	cursor int
}

func newServersList(names []string) serversModel {
	return serversModel{names: names}
}

func (l *serversModel) setNames(names []string) {
	l.names = names
	if l.cursor >= len(names) {
		l.cursor = len(names) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}

func (l serversModel) selected() (string, bool) {
	if len(l.names) == 0 {
		return "", false
	}
	return l.names[l.cursor], true
}

func (l *serversModel) moveUp() {
	if l.cursor > 0 {
		l.cursor--
	}
}

func (l *serversModel) moveDown() {
	if l.cursor < len(l.names)-1 {
		l.cursor++
	}
}

// view renders up to height rows, scrolled so the cursor stays visible.
func (l serversModel) view(width, height int) string {
	if height <= 0 || height > len(l.names) {
		height = len(l.names)
	}
	start := 0
	if l.cursor >= height {
		start = l.cursor - height + 1
	}
	end := start + height
	if end > len(l.names) {
		end = len(l.names)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		if i > start {
			b.WriteString("\n")
		}
		line := l.names[i]
		prefix := "  "
		if i == l.cursor {
			prefix = "▌ "
			line = selectedRowStyle.Render(line)
		}
		b.WriteString(ansi.Truncate(prefix+line, width, ""))
	}
	return b.String()
}

// serverRefCount returns the names of profiles that reference the given server,
// used to warn before deleting a server still in use.
func serverRefCount(cfg *config.Config, server string) []string {
	var refs []string
	for name, p := range cfg.Profiles {
		if p.Server == server {
			refs = append(refs, name)
		}
	}
	return refs
}

// cloneServers makes a shallow copy of the servers map for rollback on save failure.
func cloneServers(s map[string]config.Server) map[string]config.Server {
	out := make(map[string]config.Server, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out
}

// commitServers saves cfg to path. On failure it restores prev into
// cfg.Servers so in-memory state never diverges from what is on disk.
func commitServers(path string, cfg *config.Config, prev map[string]config.Server) error {
	if err := config.Save(path, cfg); err != nil {
		cfg.Servers = prev
		return err
	}
	return nil
}

// sortedServerNames returns the server names in sorted order.
func sortedServerNames(servers map[string]config.Server) []string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
