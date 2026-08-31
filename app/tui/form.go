package tui

import (
	"strings"

	"github.com/andresbott/dibs/internal/config"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type slotKind int

const (
	slotInput slotKind = iota
	slotButton
	slotRemove
	slotAdd
	slotAddIgnore
	slotSave
	slotCancel
	slotTypeSel   // the remote-type radio row (Local / rsync / ssh)
	slotServerSel // the server selector row for rsync kind
)

// remoteKind is the remote-location type selected in the form: a plain local
// path (a mounted share), an rsync daemon, or an rsync-over-ssh location.
type remoteKind int

const (
	remoteLocal remoteKind = iota
	remoteRsync
	remoteSSH
	numKinds
)

// kindLabels are the selector's option labels, indexed by remoteKind.
var kindLabels = [numKinds]string{"Local", "rsync", "ssh"}

// The fixed inputs. All of them always exist so switching the remote type never
// loses typed values; slots() exposes only the selected kind's fields.
// inputs[numFixed : numFixed+numSubs] are the subpath fields and
// inputs[numFixed+numSubs:] the ignore-pattern fields.
const (
	idxName       = iota // profile name
	idxLocal             // local root path
	idxRemotePath        // kind Local: remote root path (a mounted share)
	idxSSHURL            // kind ssh: ssh://[user@]host[:port]/abs/path
	idxSSHIdentity       // kind ssh: identity file for ssh -i
	idxModulePath        // kind rsync: "module[/path]", typed or browsed
	numFixed
)

// focusSlot is one Tab stop, positioned on a grid: row is its line in the form
// and col is 0 (inputs / Add / Save) or 1 (Browse and Remove buttons / Cancel).
// field indexes formModel.inputs (-1 for the row-less actions).
type focusSlot struct {
	kind  slotKind
	field int
	row   int
	col   int
}

// slots is the Tab order (and the grid), built per call because the subpath and
// ignore sections grow and shrink, and the remote fields swap with the selected
// remote type: Name, Local root, the remote-type selector row, (for rsync: the
// server selector row), the selected kind's remote fields, one row per subpath
// (input + Remove), the Add-subpath button, one row per ignore pattern (input +
// Remove), the Add-ignore button, and the Save/Cancel action row. Every row has
// a column-0 cell, so Down/Up can always fall back to column 0 when a row lacks
// column 1.
func (f formModel) slots() []focusSlot {
	s := []focusSlot{
		{slotInput, idxName, 0, 0},   // Name
		{slotInput, idxLocal, 1, 0},  // Local root input
		{slotButton, idxLocal, 1, 1}, // Local Browse
		{slotTypeSel, -1, 2, 0},      // remote-type radio row
	}
	row := 3
	// For rsync kind, add the server selector row before the remote fields.
	if f.kind == remoteRsync {
		s = append(s, focusSlot{slotServerSel, -1, row, 0})
		row++
	}
	for _, i := range f.remoteFields() {
		s = append(s, focusSlot{slotInput, i, row, 0})
		if i == idxRemotePath || i == idxModulePath {
			s = append(s, focusSlot{slotButton, i, row, 1})
		}
		row++
	}
	for i := numFixed; i < numFixed+f.numSubs; i++ {
		s = append(s, focusSlot{slotInput, i, row, 0}, focusSlot{slotRemove, i, row, 1})
		row++
	}
	s = append(s, focusSlot{slotAdd, -1, row, 0})
	row++
	for i := numFixed + f.numSubs; i < len(f.inputs); i++ {
		s = append(s, focusSlot{slotInput, i, row, 0}, focusSlot{slotRemove, i, row, 1})
		row++
	}
	return append(s,
		focusSlot{slotAddIgnore, -1, row, 0},
		focusSlot{slotSave, -1, row + 1, 0},
		focusSlot{slotCancel, -1, row + 1, 1})
}

// remoteFields returns the fixed-input indexes the selected remote kind
// exposes, in display order. For rsync, only the module path input is returned;
// the server selector is a separate slot (slotServerSel), not an input.
func (f formModel) remoteFields() []int {
	switch f.kind {
	case remoteRsync:
		return []int{idxModulePath}
	case remoteSSH:
		return []int{idxSSHURL, idxSSHIdentity}
	default:
		return []int{idxRemotePath}
	}
}

// focusKind is the kind of the currently focused slot.
func (f formModel) focusKind() slotKind { return f.slots()[f.focus].kind }

// focusField is the input index the current slot belongs to (its own index for an
// input slot, the buttoned field for a Browse/Remove slot).
func (f formModel) focusField() int { return f.slots()[f.focus].field }

// currentIsButton reports whether the focused slot is a Browse button.
func (f formModel) currentIsButton() bool { return f.focusKind() == slotButton }

// atInputEnd reports whether the focused input's cursor sits at the end of its
// text — the point at which Right leaves the field for its Browse button.
func (f formModel) atInputEnd() bool {
	in := f.inputs[f.focusField()]
	return in.Position() == len([]rune(in.Value()))
}

// inputSlot returns the slot index of a field's text input (used to return focus
// to the input after a browse).
func (f formModel) inputSlot(field int) int {
	for i, s := range f.slots() {
		if s.kind == slotInput && s.field == field {
			return i
		}
	}
	return 0
}

// buttonSlot returns the slot index of a field's Browse or Remove button, or -1
// if the field has none (the Name field).
func (f formModel) buttonSlot(field int) int {
	for i, s := range f.slots() {
		if (s.kind == slotButton || s.kind == slotRemove) && s.field == field {
			return i
		}
	}
	return -1
}

// actionSlot returns the slot index of the Add, Save or Cancel button.
func (f formModel) actionSlot(kind slotKind) int {
	for i, s := range f.slots() {
		if s.kind == kind {
			return i
		}
	}
	return -1
}

// numRows is the number of grid rows (Name, each path field, each subpath row,
// the Add row, and the action row).
func (f formModel) numRows() int {
	slots := f.slots()
	return slots[len(slots)-1].row + 1
}

// slotAt returns the slot index at grid cell (row, col), or -1 if empty.
func (f formModel) slotAt(row, col int) int {
	for i, s := range f.slots() {
		if s.row == row && s.col == col {
			return i
		}
	}
	return -1
}

type formModel struct {
	inputs      []textinput.Model
	numSubs     int    // count of subpath inputs; see the numFixed layout comment
	focus       int
	origName    string // "" for add; the existing name for edit
	err         string
	width       int        // terminal width last passed to setWidth
	termHeight  int        // terminal height, for sizing the picker
	picker      dirPicker  // directory picker for the focused path field
	browsing    bool       // true while the directory picker is open
	kind        remoteKind // selected remote-location type (Local / rsync / ssh)
	servers     map[string]config.Server
	serverNames []string // sorted keys of servers
	serverSel   int      // index into serverNames, -1 when none selected

	// Remote (rsync daemon) browsing state — see remotepicker.go.
	rsyncBin       string       // rsync binary override for the browse listings
	remote         remotePicker // the module/path browser modal
	browsingRemote bool         // true while the remote browser is open
	browseSeq      int          // stamp matching in-flight listings to the open browser
	listModules    moduleLister // test seam; nil => a Syncer-backed lister
	listDirs       dirLister    // test seam; nil => a Syncer-backed lister
}

// newInput builds a textinput with the form's shared styling: no "> " prompt
// and no placeholder. The labels already name each field, and an empty
// placeholder lets textinput fill the whole field with its (background-styled)
// trailing padding, so the bar renders gap-free (the placeholder path leaves
// that padding unstyled).
func newInput(value string) textinput.Model {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = ""
	in.SetValue(value)
	return in
}

func newForm(origName string, p config.Profile, servers map[string]config.Server) formModel {
	name := newInput(origName)
	name.CharLimit = 64

	inputs := make([]textinput.Model, numFixed)
	inputs[idxName] = name
	inputs[idxLocal] = newInput(p.LocalRoot)
	for i := idxRemotePath; i < numFixed; i++ {
		inputs[i] = newInput("")
	}
	inputs[idxSSHIdentity].SetValue(p.SSHIdentityFile)

	f := formModel{
		inputs:   inputs,
		numSubs:  len(p.Subpaths),
		origName: origName,
		serverSel: -1,
	}
	f.setServers(servers)

	// Seed remote fields: if the profile has a Server set, it's a server-backed
	// rsync profile; otherwise use the RemoteRoot decomposition.
	kind := f.seedRemoteFields(p)
	f.kind = kind

	for _, sub := range p.Subpaths {
		f.inputs = append(f.inputs, newInput(sub))
	}
	for _, pat := range p.Ignore {
		f.inputs = append(f.inputs, newInput(pat))
	}

	f.inputs[0].Focus()
	return f
}

// setServers stores the servers map and builds the sorted serverNames list.
func (f *formModel) setServers(servers map[string]config.Server) {
	f.servers = servers
	f.serverNames = nil
	for name := range servers {
		f.serverNames = append(f.serverNames, name)
	}
	// Sort for stable order
	for i := 0; i < len(f.serverNames); i++ {
		for j := i + 1; j < len(f.serverNames); j++ {
			if f.serverNames[i] > f.serverNames[j] {
				f.serverNames[i], f.serverNames[j] = f.serverNames[j], f.serverNames[i]
			}
		}
	}
}

// selectServer sets serverSel to the index of the named server, or -1 if not found.
func (f *formModel) selectServer(name string) {
	f.serverSel = -1
	for i, n := range f.serverNames {
		if n == name {
			f.serverSel = i
			return
		}
	}
}

// seedRemoteFields decomposes an existing profile into the per-kind inputs and
// returns the kind it selects. For a server-backed rsync profile (p.Server non-empty),
// it selects the server and seeds the module path. For a URL-based profile, it
// decomposes the RemoteRoot. A malformed ssh:// or rsync:// value keeps its kind
// (by prefix) with the raw URL seeded into that kind's primary field, so nothing
// is silently discarded — Save re-validates either way.
func (f *formModel) seedRemoteFields(p config.Profile) remoteKind {
	// Server-backed rsync profile
	if p.Server != "" {
		f.selectServer(p.Server)
		f.inputs[idxModulePath].SetValue(p.RemoteModule)
		return remoteRsync
	}

	// URL-based profile: decompose RemoteRoot
	parts, err := config.SplitRemoteRoot(p.RemoteRoot)
	switch {
	case parts.Kind == "rsync" && err == nil:
		// Legacy rsync:// URL (before server refactor)
		// Not expected in new configs, but handle for migration
		f.inputs[idxModulePath].SetValue(parts.ModulePath)
		return remoteRsync
	case parts.Kind == "rsync":
		f.inputs[idxModulePath].SetValue(p.RemoteRoot)
		return remoteRsync
	case parts.Kind == "ssh":
		f.inputs[idxSSHURL].SetValue(p.RemoteRoot)
		return remoteSSH
	default:
		f.inputs[idxRemotePath].SetValue(p.RemoteRoot)
		return remoteLocal
	}
}

// addSubpath inserts an empty subpath input at the end of the subpath section
// and focuses it.
func (f *formModel) addSubpath() tea.Cmd {
	at := numFixed + f.numSubs
	f.inputs = append(f.inputs[:at], append([]textinput.Model{newInput("")}, f.inputs[at:]...)...)
	f.numSubs++
	f.sizeInputs()
	return f.setFocus(f.inputSlot(at))
}

// addIgnore appends an empty ignore-pattern input and focuses it.
func (f *formModel) addIgnore() tea.Cmd {
	f.inputs = append(f.inputs, newInput(""))
	f.sizeInputs()
	return f.setFocus(f.inputSlot(len(f.inputs) - 1))
}

// removeSubpath deletes a subpath or ignore input field (an index into
// f.inputs), moving focus to the next row's Remove button (or the section's Add
// button when it was the section's last row).
func (f *formModel) removeSubpath(field int) tea.Cmd {
	inSubs := field < numFixed+f.numSubs
	f.inputs = append(f.inputs[:field], f.inputs[field+1:]...)
	if inSubs {
		f.numSubs--
		if field < numFixed+f.numSubs {
			return f.setFocus(f.buttonSlot(field))
		}
		return f.setFocus(f.actionSlot(slotAdd))
	}
	if field < len(f.inputs) {
		return f.setFocus(f.buttonSlot(field))
	}
	return f.setFocus(f.actionSlot(slotAddIgnore))
}

// subpaths returns the trimmed, non-blank subpath input values (nil when none) —
// blank rows are dropped rather than rejected.
func (f formModel) subpaths() []string {
	return nonBlank(f.inputs[numFixed : numFixed+f.numSubs])
}

// ignores returns the trimmed, non-blank ignore-pattern input values (nil when
// none) — blank rows are dropped rather than rejected.
func (f formModel) ignores() []string {
	return nonBlank(f.inputs[numFixed+f.numSubs:])
}

// nonBlank collects the trimmed, non-empty values of a run of inputs.
func nonBlank(inputs []textinput.Model) []string {
	var out []string
	for _, in := range inputs {
		if v := strings.TrimSpace(in.Value()); v != "" {
			out = append(out, v)
		}
	}
	return out
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
	cur := f.slots()[f.focus]
	n := f.numRows()
	targetRow := ((cur.row+dir)%n + n) % n
	if s := f.slotAt(targetRow, cur.col); s >= 0 {
		return f.setFocus(s)
	}
	return f.setFocus(f.slotAt(targetRow, 0))
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
	f.sizeInputs()
}

// sizeInputs resizes every input to its underline width (called on width change
// and when the input list grows or shrinks).
func (f *formModel) sizeInputs() {
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
	slots := f.slots()
	n := len(slots)
	i = (i%n + n) % n
	f.focus = i
	s := slots[i]
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
		// On the type selector → next kind; on the server selector → next server;
		// on Save → Cancel; on a path/subpath input with the cursor at the end →
		// its Browse/Remove button; otherwise not a nav key (let the input move
		// its cursor).
		if f.focusKind() == slotTypeSel {
			f.cycleKind(1)
			return nil, true
		}
		if f.focusKind() == slotServerSel {
			f.cycleServer(1)
			return nil, true
		}
		if f.focusKind() == slotSave {
			return f.setFocus(f.actionSlot(slotCancel)), true
		}
		if f.focusKind() == slotInput && f.buttonSlot(f.focusField()) >= 0 && f.atInputEnd() {
			return f.setFocus(f.buttonSlot(f.focusField())), true
		}
	case "left":
		// On the type selector → previous kind; on the server selector → previous
		// server; on Cancel → Save; on a Browse/Remove button → its input;
		// otherwise not a nav key (let the input move its cursor).
		if f.focusKind() == slotTypeSel {
			f.cycleKind(-1)
			return nil, true
		}
		if f.focusKind() == slotServerSel {
			f.cycleServer(-1)
			return nil, true
		}
		if f.focusKind() == slotCancel {
			return f.setFocus(f.actionSlot(slotSave)), true
		}
		if f.focusKind() == slotButton || f.focusKind() == slotRemove {
			return f.setFocus(f.inputSlot(f.focusField())), true
		}
	}
	return nil, false
}

func (f formModel) updateInputs(msg tea.Msg) (formModel, tea.Cmd) {
	if f.focusKind() != slotInput {
		return f, nil // only text inputs consume keystrokes (buttons are inert)
	}
	var cmd tea.Cmd
	field := f.focusField()
	f.inputs[field], cmd = f.inputs[field].Update(msg)
	return f, cmd
}

// values composes the profile from the selected kind's fields: only that
// kind's remote root (and its auth key) is read, so stale values typed under
// another kind never leak into the saved profile. For rsync, the server selector
// and module path set p.Server and p.RemoteModule; RemoteRoot is left empty and
// will be resolved by the config layer before syncing.
func (f formModel) values() (string, config.Profile) {
	p := config.Profile{
		LocalRoot: strings.TrimSpace(f.inputs[idxLocal].Value()),
		Subpaths:  f.subpaths(),
		Ignore:    f.ignores(),
	}
	get := func(i int) string { return strings.TrimSpace(f.inputs[i].Value()) }
	switch f.kind {
	case remoteRsync:
		if f.serverSel >= 0 && f.serverSel < len(f.serverNames) {
			p.Server = f.serverNames[f.serverSel]
		}
		p.RemoteModule = get(idxModulePath)
	case remoteSSH:
		p.RemoteRoot = get(idxSSHURL)
		p.SSHIdentityFile = get(idxSSHIdentity)
	default:
		p.RemoteRoot = get(idxRemotePath)
	}
	return get(idxName), p
}

func (f formModel) View() string {
	if f.browsing {
		return f.pickerView()
	}
	if f.browsingRemote {
		return f.remotePickerView()
	}
	title := "Add profile"
	if f.origName != "" {
		title = "Edit profile: " + f.origName
	}

	var content strings.Builder
	writeField := func(i int) {
		label := labelStyle
		if f.focusKind() == slotInput && f.focusField() == i {
			label = focusLabelStyle
		}
		content.WriteString(label.Render(fieldLabels[i]))
		content.WriteString("\n")
		content.WriteString(f.fieldRow(i))
	}
	// Each group after the first opens with a titled dotted divider
	// ("┄┄ Title ┄┄┄…"; the fields' underlines already use "─", so a distinct
	// glyph keeps them apart): Name, the root dirs (local + remote), the
	// subpaths, and the ignored files.
	writeField(idxName)
	content.WriteString(f.sectionHeader("Root dirs", false))
	writeField(idxLocal)

	// Remote-type selector row, then (for rsync) the server selector row, then
	// the selected kind's fields.
	content.WriteString("\n")
	label := labelStyle
	if f.focusKind() == slotTypeSel {
		label = focusLabelStyle
	}
	content.WriteString(label.Render("Remote type"))
	content.WriteString("\n")
	content.WriteString(f.typeSelRow())

	// For rsync kind, show the server selector
	if f.kind == remoteRsync {
		content.WriteString("\n")
		serverLabel := labelStyle
		if f.focusKind() == slotServerSel {
			serverLabel = focusLabelStyle
		}
		content.WriteString(serverLabel.Render("Server"))
		content.WriteString("\n")
		content.WriteString(f.serverSelRow())
	}

	for _, i := range f.remoteFields() {
		content.WriteString("\n")
		writeField(i)
	}

	// Subpaths section: one input+Remove row per subpath and the Add-subpath
	// button; its title lives in the section header, highlighted while any of
	// its rows has focus.
	subFocus := (f.focusKind() == slotInput || f.focusKind() == slotRemove) &&
		f.focusField() >= numFixed && f.focusField() < numFixed+f.numSubs ||
		f.focusKind() == slotAdd
	content.WriteString(f.sectionHeader("Subpaths", subFocus))
	for i := numFixed; i < numFixed+f.numSubs; i++ {
		content.WriteString(f.fieldRow(i))
		content.WriteString("\n")
	}
	content.WriteString(f.actionButton(slotAdd, "Add subpath"))

	// Ignored-files section: same shape.
	ignFocus := (f.focusKind() == slotInput || f.focusKind() == slotRemove) &&
		f.focusField() >= numFixed+f.numSubs ||
		f.focusKind() == slotAddIgnore
	content.WriteString(f.sectionHeader("Ignored files", ignFocus))
	for i := numFixed + f.numSubs; i < len(f.inputs); i++ {
		content.WriteString(f.fieldRow(i))
		content.WriteString("\n")
	}
	content.WriteString(f.actionButton(slotAddIgnore, "Add ignore"))

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

// sectionHeader renders a group's titled divider — "┄┄ Title ┄┄┄…" spanning
// the content width, blank-line padded — the title accented while the group
// holds focus, dim otherwise.
func (f formModel) sectionHeader(title string, focused bool) string {
	st := labelStyle
	if focused {
		st = focusLabelStyle
	}
	w := f.modalWidth() - 4 // content budget: modal borders (2) + body padding (2)
	lead := "┄┄ "
	trail := strings.Repeat("┄", max(w-lipgloss.Width(lead+title)-1, 3))
	return "\n\n" + helpTextStyle.Render(lead) + st.Render(title) + " " + helpTextStyle.Render(trail) + "\n\n"
}

// fieldLabels names each fixed input in the form, indexed like inputs.
var fieldLabels = [numFixed]string{
	idxName:        "Name",
	idxLocal:       "Local root",
	idxRemotePath:  "Remote root",
	idxSSHURL:      "Remote root (ssh://[user@]host[:port]/path)",
	idxSSHIdentity: "SSH identity file (optional)",
	idxModulePath:  "Module / path",
}

// buttonLabel is the inline button next to field i: Browse for the browsable
// path fields, Remove for the subpath/ignore rows, "" for buttonless fields.
func buttonLabel(i int) string {
	switch {
	case i == idxLocal || i == idxRemotePath || i == idxModulePath:
		return "Browse"
	case i >= numFixed:
		return "Remove"
	default:
		return ""
	}
}

// cycleKind steps the remote-type selector by dir (+1 / -1), wrapping, and
// re-fits the layout to the new kind's slot grid.
func (f *formModel) cycleKind(dir int) {
	f.kind = remoteKind((int(f.kind) + dir + int(numKinds)) % int(numKinds))
	f.sizeInputs()
	// The slot list changed shape; re-resolve focus so it stays on the selector.
	for i, s := range f.slots() {
		if s.kind == slotTypeSel {
			f.focus = i
			return
		}
	}
}

// cycleServer steps the server selector by dir (+1 / -1), wrapping.
func (f *formModel) cycleServer(dir int) {
	if len(f.serverNames) == 0 {
		f.serverSel = -1
		return
	}
	if f.serverSel < 0 {
		// No selection yet: start at first (forward) or last (backward)
		if dir > 0 {
			f.serverSel = 0
		} else {
			f.serverSel = len(f.serverNames) - 1
		}
		return
	}
	n := len(f.serverNames)
	f.serverSel = (f.serverSel + dir + n) % n
}

// typeSelRow renders the remote-type radio row: one (•)/( ) option per kind,
// the selected one marked, the whole row accented while the selector is focused.
func (f formModel) typeSelRow() string {
	focused := f.focusKind() == slotTypeSel
	var opts []string
	for k := remoteLocal; k < numKinds; k++ {
		mark := "( )"
		if k == f.kind {
			mark = "(•)"
		}
		opt := mark + " " + kindLabels[k]
		st := lipgloss.NewStyle().Foreground(colDim)
		switch {
		case focused && k == f.kind:
			st = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
		case k == f.kind:
			st = lipgloss.NewStyle()
		}
		opts = append(opts, st.Render(opt))
	}
	return strings.Join(opts, "   ")
}

// serverSelRow renders the server selector row: cycles through available servers
// or shows a hint when none exist. Accented while focused.
func (f formModel) serverSelRow() string {
	focused := f.focusKind() == slotServerSel
	if len(f.serverNames) == 0 {
		msg := "(no servers — press v on the main view to add one)"
		if focused {
			return lipgloss.NewStyle().Foreground(colAccent).Bold(true).Render(msg)
		}
		return helpTextStyle.Render(msg)
	}
	sel := "(none)"
	if f.serverSel >= 0 && f.serverSel < len(f.serverNames) {
		sel = f.serverNames[f.serverSel]
	}
	st := lipgloss.NewStyle().Foreground(colDim)
	if focused {
		st = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	}
	return st.Render("< " + sel + " >")
}

// fieldWidth is the display width of input i's underline. Fields with an inline
// button are narrowed to leave room for it.
func (f formModel) fieldWidth(i int) int {
	w := f.modalWidth() - 4 // content budget: modal borders (2) + body padding (2)
	if buttonLabel(i) != "" {
		w -= 11 // " [ Browse ]" / " [ Remove ]": 1-col gap + 10-col button
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
	if f.focus == f.inputSlot(i) {
		c = colAccent
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false). // bottom only
		BorderForeground(c).
		Width(f.fieldWidth(i)).
		Render(f.inputs[i].View())
}

// rowButton renders the inline bracketed control next to field i ([ Browse ]
// for the path fields, [ Remove ] for subpath rows): accent + bold when it is
// the focused slot, dim otherwise.
func (f formModel) rowButton(i int, label string) string {
	st := lipgloss.NewStyle().Foreground(colDim)
	k := f.focusKind()
	if (k == slotButton || k == slotRemove) && f.focusField() == i {
		st = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	}
	return st.Render("[ " + label + " ]")
}

// actionButton renders a bracketed [ label ] action button (Add subpath /
// Save / Cancel): accent + bold when it is the focused slot, dim otherwise.
func (f formModel) actionButton(kind slotKind, label string) string {
	st := lipgloss.NewStyle().Foreground(colDim)
	if f.focusKind() == kind {
		st = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	}
	return st.Render("[ " + label + " ]")
}

// fieldRow renders one field's editable area: the underlined input, plus its
// inline Browse/Remove button when the field has one.
func (f formModel) fieldRow(i int) string {
	label := buttonLabel(i)
	if label == "" {
		return f.underline(i)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, f.underline(i), " ", f.rowButton(i, label))
}
