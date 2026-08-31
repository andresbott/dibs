package tui

import (
	"context"
	"strings"
	"time"

	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/libs/threewayrsync"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// rpPhase is the remote browser's state: loading or showing the daemon's module
// list, loading or showing a directory listing inside the chosen module, or a
// failed listing (with Retry).
type rpPhase int

const (
	rpLoadingModules rpPhase = iota
	rpModules
	rpLoadingDirs
	rpDirs
	rpError
)

// moduleLister / dirLister are the listing functions the browser calls — a seam
// so tests inject canned results instead of a live daemon.
type moduleLister func(ctx context.Context, d threewayrsync.Daemon) ([]threewayrsync.Module, error)
type dirLister func(ctx context.Context, d threewayrsync.Daemon, path string) ([]string, error)

// listTimeout bounds one remote listing; a daemon that accepts the connection
// but never answers must not wedge the browser forever.
const listTimeout = 30 * time.Second

// remoteListResultMsg carries one background listing back into Update. seq is
// the launch stamp so a result abandoned by esc (or superseded by a newer
// request) is recognised and dropped. Exactly one of modules/dirs is set on
// success; err reports a failed listing.
type remoteListResultMsg struct {
	seq     int
	modules []threewayrsync.Module
	dirs    []string
	err     error
}

// loadModulesCmd fetches the daemon's module list off the UI thread.
func loadModulesCmd(fn moduleLister, d threewayrsync.Daemon, seq int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
		defer cancel()
		mods, err := fn(ctx, d)
		return remoteListResultMsg{seq: seq, modules: mods, err: err}
	}
}

// loadDirsCmd fetches the directory names at path inside d.Module off the UI thread.
func loadDirsCmd(fn dirLister, d threewayrsync.Daemon, path string, seq int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
		defer cancel()
		dirs, err := fn(ctx, d, path)
		return remoteListResultMsg{seq: seq, dirs: dirs, err: err}
	}
}

// remotePicker is the state of the rsync-daemon browser: the daemon it talks
// to, the current phase, the module and path navigated into, the entries
// listed (module names or directory names), and the cursor/scroll window over
// them. focus reuses the local picker's three stops (list, Select, Cancel).
type remotePicker struct {
	daemon  threewayrsync.Daemon
	phase   rpPhase
	modules []threewayrsync.Module // cached module list ("" module → shown again on up)
	module  string                 // "" while choosing a module
	path    string                 // slash-joined path inside module ("" = module root)
	entries []string
	cursor  int
	offset  int
	height  int
	focus   pickerFocus
	errMsg  string
}

// syncerListers returns the production module/dir listers, backed by a Syncer
// shelling out to the given rsync binary ("" = rsync from PATH).
func syncerListers(bin string) (moduleLister, dirLister) {
	s := &threewayrsync.Syncer{Bin: bin}
	return s.ListModules, s.ListDirs
}

// listers resolves the form's listing seams, defaulting to the Syncer-backed ones.
func (f *formModel) listers() (moduleLister, dirLister) {
	mods, dirs := f.listModules, f.listDirs
	if mods == nil || dirs == nil {
		m, d := syncerListers(f.rsyncBin)
		if mods == nil {
			mods = m
		}
		if dirs == nil {
			dirs = d
		}
	}
	return mods, dirs
}

// openRemotePicker opens the daemon browser for the module-path field: it
// builds the daemon from the selected server (surfacing errors as a form error
// instead of opening), seeds the starting module/path from the field's current
// value so re-browsing resumes there, and kicks off the first listing.
func (f *formModel) openRemotePicker() tea.Cmd {
	// Build the daemon from the selected server
	if f.serverSel < 0 || f.serverSel >= len(f.serverNames) {
		f.err = "select a server before browsing"
		return nil
	}
	srv := f.servers[f.serverNames[f.serverSel]]
	if strings.TrimSpace(srv.Host) == "" {
		f.err = "selected server has no host"
		return nil
	}
	f.err = ""
	d := threewayrsync.Daemon{
		Host:         srv.Host,
		Port:         srv.Port,
		User:         srv.User,
		PasswordFile: config.ExpandRoot(srv.PasswordFile),
	}
	get := func(i int) string { return strings.TrimSpace(f.inputs[i].Value()) }
	module, path := splitModulePath(get(idxModulePath))
	f.browseSeq++
	f.browsingRemote = true
	rp := remotePicker{daemon: d, height: f.pickerHeight(), focus: focusList, module: module, path: path}
	f.remote = rp
	mods, dirs := f.listers()
	if module != "" {
		f.remote.phase = rpLoadingDirs
		f.remote.daemon.Module = module
		return loadDirsCmd(dirs, f.remote.daemon, path, f.browseSeq)
	}
	f.remote.phase = rpLoadingModules
	return loadModulesCmd(mods, d, f.browseSeq)
}

// splitModulePath splits a "module[/path]" field value into its module and the
// path inside it.
func splitModulePath(v string) (module, path string) {
	v = strings.Trim(v, "/")
	module, path, _ = strings.Cut(v, "/")
	return module, path
}

// applyRemoteListResult folds a finished listing into the open browser. Stale
// results — the browser was closed, or a newer listing was requested — are
// dropped by the seq stamp.
func (f *formModel) applyRemoteListResult(res remoteListResultMsg) {
	if !f.browsingRemote || res.seq != f.browseSeq {
		return
	}
	if res.err != nil {
		f.remote.errMsg = res.err.Error()
		f.remote.phase = rpError
		f.remote.focus = focusSelect // land on Retry
		return
	}
	f.remote.errMsg = ""
	f.remote.focus = focusList
	switch f.remote.phase {
	case rpLoadingModules:
		f.remote.modules = res.modules
		f.remote.entries = moduleNames(res.modules)
		f.remote.phase = rpModules
	case rpLoadingDirs:
		f.remote.entries = res.dirs
		f.remote.phase = rpDirs
	}
	f.remote.cursor, f.remote.offset = 0, 0
}

func moduleNames(mods []threewayrsync.Module) []string {
	names := make([]string, len(mods))
	for i, m := range mods {
		names[i] = m.Name
	}
	return names
}

// updateRemotePicker drives the open daemon browser. List focus mirrors the
// local picker: w/↑ s/↓ move, pgup/pgdn ±5, enter/d/→ open (select a module /
// descend into a folder), a/← go up (to the parent folder, or from the module
// root back to the module list), space selects immediately, esc cancels,
// tab/shift+tab jump to the buttons. While a listing is in flight only esc
// works. The error phase offers Retry (re-issue the failed listing) and Cancel.
func (f formModel) updateRemotePicker(msg tea.Msg) (formModel, tea.Cmd) {
	if res, ok := msg.(remoteListResultMsg); ok {
		f.applyRemoteListResult(res)
		return f, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return f, nil
	}
	key := k.String()
	if key == "esc" {
		f.browsingRemote = false
		return f, nil
	}
	switch f.remote.phase {
	case rpLoadingModules, rpLoadingDirs:
		return f, nil // only esc while loading
	case rpError:
		return f.updateRemoteError(key)
	}
	if f.remote.focus == focusList {
		return f.updateRemoteList(key)
	}
	return f.updateRemoteButtons(key)
}

// updateRemoteError handles the failed-listing phase: ←→/tab switch between
// Retry and Cancel, enter/space activates.
func (f formModel) updateRemoteError(key string) (formModel, tea.Cmd) {
	switch key {
	case "left", "a", "right", "d", "tab", "shift+tab":
		if f.remote.focus == focusSelect {
			f.remote.focus = focusCancel
		} else {
			f.remote.focus = focusSelect
		}
	case "enter", " ":
		if f.remote.focus == focusCancel {
			f.browsingRemote = false
			return f, nil
		}
		return f, f.retryListing()
	}
	return f, nil
}

// retryListing re-issues the listing for the browser's current position: the
// module list when no module is chosen, else the directory listing at the
// current path.
func (f *formModel) retryListing() tea.Cmd {
	f.browseSeq++
	mods, dirs := f.listers()
	if f.remote.module == "" {
		f.remote.phase = rpLoadingModules
		return loadModulesCmd(mods, f.remote.daemon, f.browseSeq)
	}
	f.remote.phase = rpLoadingDirs
	return loadDirsCmd(dirs, f.remote.daemon, f.remote.path, f.browseSeq)
}

// updateRemoteList handles keys while the entry list has focus.
func (f formModel) updateRemoteList(key string) (formModel, tea.Cmd) {
	switch key {
	case "w", "up":
		f.remote.moveBy(-1)
	case "s", "down":
		f.remote.moveBy(1)
	case "pgup":
		f.remote.moveBy(-5)
	case "pgdown":
		f.remote.moveBy(5)
	case " ":
		return f.confirmRemoteSelection()
	case "enter", "d", "right":
		return f.openRemoteEntry()
	case "a", "left":
		return f.remoteUp()
	case "tab":
		f.remote.focus = focusSelect
	case "shift+tab":
		f.remote.focus = focusCancel
	}
	return f, nil
}

// updateRemoteButtons handles keys while Select/Cancel have focus, mirroring
// the local picker's button behavior.
func (f formModel) updateRemoteButtons(key string) (formModel, tea.Cmd) {
	switch key {
	case "tab":
		if f.remote.focus == focusSelect {
			f.remote.focus = focusCancel
		} else {
			f.remote.focus = focusList
		}
	case "shift+tab":
		if f.remote.focus == focusCancel {
			f.remote.focus = focusSelect
		} else {
			f.remote.focus = focusList
		}
	case "left", "a", "right", "d":
		if f.remote.focus == focusSelect {
			f.remote.focus = focusCancel
		} else {
			f.remote.focus = focusSelect
		}
	case "enter", " ":
		if f.remote.focus == focusSelect {
			return f.confirmRemoteSelection()
		}
		f.browsingRemote = false
	}
	return f, nil
}

// openRemoteEntry descends into the highlighted entry: on the module list it
// selects the module and lists its root; on a directory list it appends the
// folder to the path and lists it. No-op on an empty list.
func (f formModel) openRemoteEntry() (formModel, tea.Cmd) {
	if len(f.remote.entries) == 0 {
		return f, nil
	}
	sel := f.remote.entries[f.remote.cursor]
	_, dirs := f.listers()
	if f.remote.phase == rpModules {
		f.remote.module = sel
		f.remote.path = ""
	} else {
		f.remote.path = joinSlash(f.remote.path, sel)
	}
	f.remote.daemon.Module = f.remote.module
	f.remote.phase = rpLoadingDirs
	f.browseSeq++
	return f, loadDirsCmd(dirs, f.remote.daemon, f.remote.path, f.browseSeq)
}

// remoteUp moves one level up: to the parent folder, or — already at the module
// root — back to the module list (cached when it was fetched this session,
// re-fetched otherwise). No-op on the module list itself.
func (f formModel) remoteUp() (formModel, tea.Cmd) {
	switch {
	case f.remote.phase != rpDirs:
		return f, nil
	case f.remote.path != "":
		parent := ""
		if i := strings.LastIndex(f.remote.path, "/"); i >= 0 {
			parent = f.remote.path[:i]
		}
		f.remote.path = parent
		f.remote.phase = rpLoadingDirs
		f.browseSeq++
		_, dirs := f.listers()
		return f, loadDirsCmd(dirs, f.remote.daemon, parent, f.browseSeq)
	case len(f.remote.modules) > 0:
		// Back to the cached module list, the just-left module highlighted.
		f.remote.entries = moduleNames(f.remote.modules)
		f.remote.cursor = indexOf(f.remote.entries, f.remote.module)
		f.remote.offset = 0
		f.remote.module = ""
		f.remote.phase = rpModules
		f.remote.ensureVisible()
		return f, nil
	default:
		// Browsing started inside a module (seeded from the field): the module
		// list was never fetched, so fetch it now.
		f.remote.module = ""
		f.remote.phase = rpLoadingModules
		f.browseSeq++
		mods, _ := f.listers()
		return f, loadModulesCmd(mods, f.remote.daemon, f.browseSeq)
	}
}

// confirmRemoteSelection writes the chosen location into the module-path field
// and closes the browser: the highlighted entry under the current position, or
// the current position itself when the list is empty.
func (f formModel) confirmRemoteSelection() (formModel, tea.Cmd) {
	module, path := f.remote.module, f.remote.path
	if len(f.remote.entries) > 0 {
		sel := f.remote.entries[f.remote.cursor]
		if f.remote.phase == rpModules {
			module, path = sel, ""
		} else {
			path = joinSlash(path, sel)
		}
	}
	if module == "" {
		// Nothing chosen (empty module list): keep the field as it was.
		f.browsingRemote = false
		return f, f.setFocus(f.inputSlot(idxModulePath))
	}
	f.inputs[idxModulePath].SetValue(joinSlash(module, path))
	f.browsingRemote = false
	return f, f.setFocus(f.inputSlot(idxModulePath))
}

// joinSlash joins two slash-relative segments, either of which may be empty.
func joinSlash(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + "/" + b
	}
}

// moveBy moves the cursor by delta, clamped to the entry list, keeping it visible.
func (rp *remotePicker) moveBy(delta int) {
	rp.cursor += delta
	if last := len(rp.entries) - 1; rp.cursor > last {
		rp.cursor = last
	}
	if rp.cursor < 0 {
		rp.cursor = 0
	}
	rp.ensureVisible()
}

// ensureVisible scrolls offset so the cursor stays within the visible window.
func (rp *remotePicker) ensureVisible() {
	if rp.height < 1 {
		rp.height = 1
	}
	if rp.cursor < rp.offset {
		rp.offset = rp.cursor
	}
	if rp.cursor >= rp.offset+rp.height {
		rp.offset = rp.cursor - rp.height + 1
	}
	if rp.offset < 0 {
		rp.offset = 0
	}
}

// remotePickerView frames the daemon browser as a modal matching the form's width.
func (f formModel) remotePickerView() string {
	title := "Select rsync module"
	if f.remote.module != "" {
		title = "Browse " + joinSlash(f.remote.module, f.remote.path)
	}
	inner := f.modalWidth() - 4 // modal borders (2) + body Padding(0,1) (2)
	body := lipgloss.NewStyle().Padding(0, 1).Render(f.remote.view(inner))
	return titledBox(title, body, f.modalWidth(), lipgloss.Height(body)+2, true)
}

// view renders the browser body — the location line, the entry list (or the
// loading/error/empty placeholder), the Select/Cancel (or Retry/Cancel)
// buttons, and the hint line — to innerWidth cells wide.
func (rp remotePicker) view(innerWidth int) string {
	var b strings.Builder
	loc := "rsync://" + rp.daemon.Host
	if rp.module != "" {
		loc += "/" + joinSlash(rp.module, rp.path)
	}
	b.WriteString(helpTextStyle.Render(ellipsisLeft(loc, innerWidth)))
	b.WriteString("\n\n")
	b.WriteString(rp.bodyView())
	b.WriteString("\n\n")
	selectLabel := "Select"
	if rp.phase == rpError {
		selectLabel = "Retry"
	}
	buttons := lipgloss.JoinHorizontal(lipgloss.Top,
		pickerButton(selectLabel, rp.focus == focusSelect), "   ",
		pickerButton("Cancel", rp.focus == focusCancel))
	b.WriteString(lipgloss.NewStyle().Width(innerWidth).Align(lipgloss.Center).Render(buttons))
	b.WriteString("\n\n")
	b.WriteString(rp.hints())
	return b.String()
}

// bodyView renders the phase's main area as exactly height rows: the entry
// list, a loading note, the error message, or the empty-list placeholder.
func (rp remotePicker) bodyView() string {
	pad := func(s string, lines int) string {
		if rp.height > lines {
			return s + strings.Repeat("\n", rp.height-lines)
		}
		return s
	}
	switch rp.phase {
	case rpLoadingModules:
		return pad("Loading modules…", 1)
	case rpLoadingDirs:
		return pad("Loading folders…", 1)
	case rpError:
		return pad(errStyle.Render(rp.errMsg), 1)
	}
	if len(rp.entries) == 0 {
		if rp.phase == rpModules {
			return pad(helpTextStyle.Render("(no modules listed — type the module name manually)"), 1)
		}
		return pad(helpTextStyle.Render("(no sub-folders)"), 1)
	}
	if rp.height <= 0 {
		return ""
	}
	rows := make([]string, rp.height)
	for r := 0; r < rp.height; r++ {
		rows[r] = rp.rowView(rp.offset + r)
	}
	return strings.Join(rows, "\n")
}

// rowView renders one list line for entry index i, matching the local picker's
// look; module rows carry no trailing slash, directory rows do.
func (rp remotePicker) rowView(i int) string {
	if i >= len(rp.entries) {
		return ""
	}
	name := rp.entries[i]
	if rp.phase == rpDirs {
		name += "/"
	} else if i < len(rp.modules) && rp.modules[i].Comment != "" {
		name += "  " + rp.modules[i].Comment
	}
	if i == rp.cursor {
		st := selectedRowStyle
		if rp.focus != focusList {
			st = lipgloss.NewStyle().Foreground(colDim)
		}
		return st.Render("▸ " + name)
	}
	return "  " + name
}

// hints is the browser's key-hint line, contextual to the phase and focus.
func (rp remotePicker) hints() string {
	sep := helpTextStyle.Render(" · ")
	switch {
	case rp.phase == rpLoadingModules || rp.phase == rpLoadingDirs:
		return hint("esc", "Cancel")
	case rp.phase == rpError:
		return strings.Join([]string{
			hint("enter/space", "Activate"), hint("←→", "Switch"), hint("esc", "Cancel"),
		}, sep)
	case rp.focus == focusList:
		return strings.Join([]string{
			hint("↑↓", "Move"), hint("enter", "Open"), hint("a/←", "Up"), hint("esc", "Cancel"),
			hint("space", "Select"), hint("tab", "Buttons"),
		}, sep)
	default:
		return strings.Join([]string{
			hint("enter/space", "Activate"), hint("←→", "Switch"), hint("tab", "List"),
			hint("esc", "Cancel"),
		}, sep)
	}
}
