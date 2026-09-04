package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// serverPicker is the modal rsync-server chooser, opened from the profile form's
// Server row when more servers are configured than fit inline (see
// maxInlineServers). It is a scrollable radio list over the form's serverNames:
// sel marks the active selection (•), cursor is the navigation highlight (▸),
// and focus reuses the local picker's three stops (list, Select, Cancel).
type serverPicker struct {
	names  []string
	sel    int // index of the (•)-marked active selection, -1 when none
	cursor int
	offset int
	height int
	focus  pickerFocus
}

// openServerPicker opens the modal server chooser, seeding the cursor and radio
// mark on the current selection so the list opens on what is already chosen.
func (f *formModel) openServerPicker() tea.Cmd {
	sp := serverPicker{
		names:  f.serverNames,
		sel:    f.serverSel,
		cursor: f.serverSel,
		height: f.pickerHeight(),
		focus:  focusList,
	}
	if sp.cursor < 0 {
		sp.cursor = 0
	}
	sp.ensureVisible()
	f.serverPick = sp
	f.browsingServer = true
	return nil
}

// updateServerPicker drives the open server chooser. List focus: w/↑ s/↓ move,
// pgup/pgdn ±5, enter/space select the highlighted server, tab/shift+tab jump to
// the buttons, esc cancels. Button focus: enter/space activates (Select confirms,
// Cancel closes), ←→ switch buttons, tab cycles, esc cancels.
func (f formModel) updateServerPicker(msg tea.Msg) (formModel, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return f, nil
	}
	if f.serverPick.focus == focusList {
		switch k.String() {
		case "w", "up":
			f.serverPick.moveBy(-1)
		case "s", "down":
			f.serverPick.moveBy(1)
		case "pgup":
			f.serverPick.moveBy(-5)
		case "pgdown":
			f.serverPick.moveBy(5)
		case "enter", " ":
			return f.confirmServerSelection()
		case "tab":
			f.serverPick.focusNext()
		case "shift+tab":
			f.serverPick.focusPrev()
		case "esc":
			f.browsingServer = false
		}
		return f, nil
	}
	switch k.String() {
	case "tab":
		f.serverPick.focusNext()
	case "shift+tab":
		f.serverPick.focusPrev()
	case "left", "a", "right", "d":
		if f.serverPick.focus == focusSelect {
			f.serverPick.focus = focusCancel
		} else {
			f.serverPick.focus = focusSelect
		}
	case "enter", " ":
		if f.serverPick.focus == focusSelect {
			return f.confirmServerSelection()
		}
		f.browsingServer = false // Cancel button
	case "esc":
		f.browsingServer = false
	}
	return f, nil
}

// confirmServerSelection writes the highlighted server into serverSel and closes
// the modal, restoring focus to the form's Server row. A no-op selection (empty
// list) just closes.
func (f formModel) confirmServerSelection() (formModel, tea.Cmd) {
	if len(f.serverPick.names) > 0 {
		f.serverSel = f.serverPick.cursor
	}
	f.browsingServer = false
	return f, f.setFocus(f.serverSlot())
}

// focusNext / focusPrev cycle the picker's three focus stops (list, Select,
// Cancel), wrapping. Bound to Tab / Shift+Tab.
func (p *serverPicker) focusNext() { p.focus = (p.focus + 1) % 3 }
func (p *serverPicker) focusPrev() { p.focus = (p.focus + 2) % 3 }

// moveBy moves the cursor by delta, clamped to the list, keeping it visible.
func (p *serverPicker) moveBy(delta int) {
	p.cursor += delta
	if last := len(p.names) - 1; p.cursor > last {
		p.cursor = last
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	p.ensureVisible()
}

// ensureVisible scrolls offset so the cursor stays within the visible window.
func (p *serverPicker) ensureVisible() {
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

// serverPickerView frames the server chooser as a modal matching the form width.
func (f formModel) serverPickerView() string {
	inner := f.modalWidth() - 4 // modal borders (2) + body Padding(0,1) (2)
	body := lipgloss.NewStyle().Padding(0, 1).Render(f.serverPick.view(inner))
	return titledBox("Select server", body, f.modalWidth(), lipgloss.Height(body)+2, true)
}

// view renders the chooser body — the radio list, the centered Select/Cancel
// buttons, and the hint line — to innerWidth cells wide.
func (p serverPicker) view(innerWidth int) string {
	var b strings.Builder
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

// listView renders the server list as exactly height rows (blank-padded), or a
// placeholder when the list is empty.
func (p serverPicker) listView() string {
	if len(p.names) == 0 {
		out := helpTextStyle.Render("(no servers)")
		if p.height > 1 {
			out += strings.Repeat("\n", p.height-1)
		}
		return out
	}
	if p.height <= 0 {
		return ""
	}
	rows := make([]string, p.height)
	for r := 0; r < p.height; r++ {
		rows[r] = p.rowView(p.offset + r)
	}
	return strings.Join(rows, "\n")
}

// rowView renders one list line for server index i: "▸ (•) name" for the cursor
// row (accent while the list has focus, dim once focus moves to the buttons),
// "  (•)/( ) name" for other rows, and "" for an index past the end.
func (p serverPicker) rowView(i int) string {
	if i >= len(p.names) {
		return ""
	}
	mark := "( )"
	if i == p.sel {
		mark = "(•)"
	}
	name := mark + " " + p.names[i]
	if i == p.cursor {
		st := selectedRowStyle
		if p.focus != focusList {
			st = lipgloss.NewStyle().Foreground(colDim)
		}
		return st.Render("▸ " + name)
	}
	return "  " + name
}

// hints is the chooser's key-hint line, contextual to whether the list or the
// buttons have focus.
func (p serverPicker) hints() string {
	sep := helpTextStyle.Render(" · ")
	if p.focus == focusList {
		return strings.Join([]string{
			hint("↑↓", "Move"), hint("enter/space", "Select"), hint("tab", "Buttons"),
			hint("esc", "Cancel"),
		}, sep)
	}
	return strings.Join([]string{
		hint("enter/space", "Activate"), hint("←→", "Switch"), hint("tab", "List"),
		hint("esc", "Cancel"),
	}, sep)
}
