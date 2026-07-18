package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// listModel is the left-panel profile list: a set of names and a cursor.
type listModel struct {
	names  []string
	cursor int
}

func newList(names []string) listModel {
	return listModel{names: names}
}

func (l *listModel) setNames(names []string) {
	l.names = names
	if l.cursor >= len(names) {
		l.cursor = len(names) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}

func (l listModel) selected() (string, bool) {
	if len(l.names) == 0 {
		return "", false
	}
	return l.names[l.cursor], true
}

func (l *listModel) moveUp() {
	if l.cursor > 0 {
		l.cursor--
	}
}

func (l *listModel) moveDown() {
	if l.cursor < len(l.names)-1 {
		l.cursor++
	}
}

// view renders up to height rows, scrolled so the cursor stays visible, each
// clipped to width. The selected row is marked and accented.
func (l listModel) view(width, height int) string {
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
