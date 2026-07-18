package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/andresbott/netcheckout/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// openPicker opens the directory browser for the focused path field: an existing
// value is shown inside its parent with that folder highlighted, else the nearest
// existing ancestor (or home) is listed.
func (f *formModel) openPicker() tea.Cmd {
	dir, highlight := pickerStart(f.inputs[f.focusField()].Value())
	p := dirPicker{dir: dir, height: f.pickerHeight(), focus: focusList}
	p.entries = readSubdirs(dir)
	p.cursor = indexOf(p.entries, highlight)
	p.ensureVisible()
	f.picker = p
	f.browsing = true
	return nil
}

// updatePicker drives the open browser. List focus: w/↑ s/↓ move, pgup/pgdn ±5,
// enter/d/→ open a folder, a/← go up, space selects immediately, esc cancels the
// picker, tab/shift+tab jump to the buttons. Button focus: enter/space activates
// (Select confirms, Cancel closes), ←→/a/d switch buttons, tab cycles, esc
// cancels the picker (same as the list, so it never flips meaning by focus).
func (f formModel) updatePicker(msg tea.Msg) (formModel, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return f, nil
	}
	if f.picker.focus == focusList {
		switch k.String() {
		case "w", "up":
			f.picker.moveUp()
		case "s", "down":
			f.picker.moveDown()
		case "pgup":
			f.picker.moveBy(-5)
		case "pgdown":
			f.picker.moveBy(5)
		case " ":
			return f.confirmSelection()
		case "enter", "d", "right":
			f.picker.open()
		case "a", "left":
			f.picker.upDir()
		case "tab":
			f.picker.focusNext()
		case "shift+tab":
			f.picker.focusPrev()
		case "esc":
			f.browsing = false
		}
		return f, nil
	}
	switch k.String() {
	case "tab":
		f.picker.focusNext()
	case "shift+tab":
		f.picker.focusPrev()
	case "left", "a", "right", "d":
		if f.picker.focus == focusSelect {
			f.picker.focus = focusCancel
		} else {
			f.picker.focus = focusSelect
		}
	case "enter", " ":
		if f.picker.focus == focusSelect {
			return f.confirmSelection()
		}
		f.browsing = false // Cancel button
	case "esc":
		f.browsing = false
	}
	return f, nil
}

// confirmSelection writes the chosen folder into the focused field and closes the
// picker: the highlighted sub-directory, or the current directory when it has none.
func (f formModel) confirmSelection() (formModel, tea.Cmd) {
	sel := f.picker.dir
	if len(f.picker.entries) > 0 {
		sel = filepath.Join(f.picker.dir, f.picker.entries[f.picker.cursor])
	}
	field := f.focusField()
	f.inputs[field].SetValue(sel)
	f.browsing = false
	return f, f.setFocus(inputSlot(field))
}

// pickerHeight is the number of directory rows listed, sized to the terminal and
// clamped so the modal (list + path + hints + borders) fits small screens.
func (f formModel) pickerHeight() int {
	h := f.termHeight - 11
	if h > 18 {
		h = 18
	}
	if h < 4 {
		h = 4
	}
	return h
}

// pickerView frames the directory browser as a modal matching the form's width.
func (f formModel) pickerView() string {
	title := "Select local root"
	if f.focusField() == 2 {
		title = "Select remote root"
	}
	inner := f.modalWidth() - 4 // modal borders (2) + body Padding(0,1) (2)
	body := lipgloss.NewStyle().Padding(0, 1).Render(f.picker.view(inner))
	return titledBox(title, body, f.modalWidth(), lipgloss.Height(body)+2, true)
}

// nearestExistingDir returns p if it is an existing directory, else its nearest
// existing ancestor, else "".
func nearestExistingDir(p string) string {
	for d := p; d != ""; {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
	return ""
}

// pickerStart returns the directory the picker should list and the entry name to
// highlight ("" for none) for a field value: an existing folder is shown inside its
// parent (highlighted); a non-existent path lists its nearest existing ancestor;
// empty falls back to the home directory, else the working directory.
func pickerStart(val string) (dir, highlight string) {
	p := config.ExpandRoot(strings.TrimSpace(val))
	if p == "" {
		return homeDir(), ""
	}
	p = filepath.Clean(p) // normalize trailing slashes so an existing dir yields (parent, base)
	near := nearestExistingDir(p)
	if near == "" {
		return homeDir(), ""
	}
	if near != p {
		return near, "" // exact path missing → list the nearest existing ancestor
	}
	if parent := filepath.Dir(p); parent != p {
		return parent, filepath.Base(p) // existing dir → show it inside its parent
	}
	return p, "" // p is the filesystem root
}

// homeDir returns the user's home directory, or "." if it can't be determined.
func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return "."
}

// readSubdirs returns the names of dir's sub-directories (dot-directories
// excluded), sorted. Symlinks that resolve to a directory are followed and
// included. An unreadable directory yields nil.
func readSubdirs(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		switch {
		case e.IsDir():
			names = append(names, e.Name())
		case e.Type()&os.ModeSymlink != 0:
			// Follow the symlink: include it only if the target is a directory.
			if info, err := os.Stat(filepath.Join(dir, e.Name())); err == nil && info.IsDir() {
				names = append(names, e.Name())
			}
		}
	}
	sort.Strings(names)
	return names
}

// indexOf returns the index of s in ss, or 0 if absent (an unknown highlight leaves
// the cursor at the top of the list).
func indexOf(ss []string, s string) int {
	for i, v := range ss {
		if v == s {
			return i
		}
	}
	return 0
}

// pickerFocus is which control in the directory browser currently has input
// focus: the list itself, or one of the Select/Cancel buttons below it.
type pickerFocus int

const (
	focusList pickerFocus = iota
	focusSelect
	focusCancel
)

// dirPicker is the state of the directory browser: the directory currently listed,
// its sub-directories (dirs only, sorted), the highlighted index, the scroll window
// top, the number of visible rows, and which control currently has focus.
type dirPicker struct {
	dir     string
	entries []string
	cursor  int
	offset  int
	height  int
	focus   pickerFocus
}

// focusNext / focusPrev cycle through the picker's three focus stops (list,
// Select, Cancel), wrapping. Bound to Tab / Shift+Tab while the picker is open.
func (p *dirPicker) focusNext() { p.focus = (p.focus + 1) % 3 }
func (p *dirPicker) focusPrev() { p.focus = (p.focus + 2) % 3 }

// ensureVisible scrolls offset so the cursor stays within the visible window.
func (p *dirPicker) ensureVisible() {
	if p.height < 1 {
		p.height = 1
	}
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+p.height {
		p.offset = p.cursor - p.height + 1
	}
	if p.offset < 0 {
		p.offset = 0
	}
}

// setHeight sets the visible row count and re-clamps the scroll window.
func (p *dirPicker) setHeight(h int) {
	p.height = h
	p.ensureVisible()
}

// moveBy moves the cursor by delta, clamped to [0, len(entries)-1], keeping it visible.
func (p *dirPicker) moveBy(delta int) {
	p.cursor += delta
	if last := len(p.entries) - 1; p.cursor > last {
		p.cursor = last
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	p.ensureVisible()
}

// moveUp / moveDown move the cursor one row, keeping it visible.
func (p *dirPicker) moveUp()   { p.moveBy(-1) }
func (p *dirPicker) moveDown() { p.moveBy(1) }

// open descends into the highlighted sub-directory and lists it. No-op on an empty
// list.
func (p *dirPicker) open() {
	if len(p.entries) == 0 {
		return
	}
	p.dir = filepath.Join(p.dir, p.entries[p.cursor])
	p.entries = readSubdirs(p.dir)
	p.cursor = 0
	p.offset = 0
}

// upDir moves to the parent directory, highlighting the directory just left. No-op
// at the filesystem root.
func (p *dirPicker) upDir() {
	parent := filepath.Dir(p.dir)
	if parent == p.dir {
		return
	}
	child := filepath.Base(p.dir)
	p.dir = parent
	p.entries = readSubdirs(parent)
	p.cursor = indexOf(p.entries, child)
	p.offset = 0
	p.ensureVisible()
}

// ellipsisLeft truncates s from the LEFT to width cells, prefixing "…" when it does
// not fit, so the most-specific tail (the deepest path segment) stays visible.
func ellipsisLeft(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	r := []rune(s)
	for i := 1; i <= len(r); i++ {
		if suffix := string(r[i:]); lipgloss.Width(suffix) <= width-1 {
			return "…" + suffix
		}
	}
	return "…"
}

// hints is the picker's key-hint line, styled like the rest of the app, and
// contextual to whether the list or the buttons currently have focus.
func (p dirPicker) hints() string {
	sep := helpTextStyle.Render(" · ")
	if p.focus == focusList {
		return strings.Join([]string{
			hint("↑↓", "Move"), hint("enter", "Open"), hint("esc", "Cancel"),
			hint("space", "Select"), hint("tab", "Buttons"), hint("pgup/pgdn", "Jump"),
		}, sep)
	}
	return strings.Join([]string{
		hint("enter/space", "Activate"), hint("←→", "Switch"), hint("tab", "List"),
		hint("esc", "Cancel"),
	}, sep)
}

// pickerButton renders a bracketed [ label ] button, accent+bold when focused.
func pickerButton(label string, focused bool) string {
	st := lipgloss.NewStyle().Foreground(colDim)
	if focused {
		st = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	}
	return st.Render("[ " + label + " ]")
}

// view renders the picker body — current path, the directory list (a stable
// height-row window), the centered Select/Cancel buttons, and the hint line — to
// innerWidth cells wide. The modal frame is added by formModel.pickerView.
func (p dirPicker) view(innerWidth int) string {
	var b strings.Builder
	b.WriteString(helpTextStyle.Render(ellipsisLeft(p.dir, innerWidth)))
	b.WriteString("\n\n")
	b.WriteString(p.listView())
	b.WriteString("\n\n")
	buttons := lipgloss.JoinHorizontal(lipgloss.Top,
		pickerButton("Select", p.focus == focusSelect), "   ",
		pickerButton("Cancel", p.focus == focusCancel))
	b.WriteString(lipgloss.NewStyle().Width(innerWidth).Align(lipgloss.Center).Render(buttons))
	b.WriteString("\n\n")
	b.WriteString(p.hints())
	return b.String()
}

// listView renders the directory list as exactly height rows (blank-padded), or a
// placeholder line (also height rows tall) when there are no sub-directories.
func (p dirPicker) listView() string {
	if len(p.entries) == 0 {
		out := helpTextStyle.Render("(no sub-folders)")
		if p.height > 1 {
			out += strings.Repeat("\n", p.height-1)
		}
		return out
	}
	if p.height <= 0 {
		return "" // no rows to render (also avoids make() panicking on a negative height)
	}
	rows := make([]string, p.height)
	for r := 0; r < p.height; r++ {
		rows[r] = p.rowView(p.offset + r)
	}
	return strings.Join(rows, "\n")
}

// rowView renders one list line for entry index i: "▸ name/" for the cursor row
// (accent while the list has focus, dimmed once focus has moved to the buttons),
// "  name/" for other rows, and "" for an index past the end (a blank padding row).
func (p dirPicker) rowView(i int) string {
	if i >= len(p.entries) {
		return ""
	}
	name := p.entries[i] + "/"
	if i == p.cursor {
		st := selectedRowStyle
		if p.focus != focusList {
			st = lipgloss.NewStyle().Foreground(colDim)
		}
		return st.Render("▸ " + name)
	}
	return "  " + name
}
