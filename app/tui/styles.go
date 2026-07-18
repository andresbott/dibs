package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	labelStyle = lipgloss.NewStyle().Faint(true)
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
)

// --- Posting-style palette (256-colour). Only the colours/styles titledBox
// itself uses are defined here; the header/footer/details/list styles are
// added by Tasks 3-4 where they are first used, so each intermediate commit
// stays clean under golangci-lint's `unused` check. ---
var (
	colAccent = lipgloss.Color("205") // hot pink: focus border + key hints
	colDim    = lipgloss.Color("240") // gray: unfocused borders, labels
	colTitle  = lipgloss.Color("141") // purple: header app name
)

var (
	titleActiveStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	titleDimStyle    = lipgloss.NewStyle().Foreground(colDim)
	selectedRowStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	profileNameStyle = lipgloss.NewStyle().Foreground(colTitle).Bold(true)
	helpKeyStyle     = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	helpTextStyle    = lipgloss.NewStyle().Foreground(colDim)
	headerAppStyle   = lipgloss.NewStyle().Foreground(colTitle).Bold(true)
	headerIDStyle    = lipgloss.NewStyle().Foreground(colDim)
)

// focusLabelStyle highlights the focused form field's label in the accent
// colour; that field's box border switches to the accent colour too (see
// fieldBox in form.go).
var focusLabelStyle = lipgloss.NewStyle().Foreground(colAccent)

// titledBox renders a rounded-border box of exactly width x height cells with
// title embedded in the top border and body clipped/padded to fit. Border and
// title use the accent colour when focused, dim otherwise.
func titledBox(title, body string, width, height int, focused bool) string {
	if width < 4 {
		width = 4
	}
	if height < 3 {
		height = 3
	}
	iw := width - 2  // inner width (between the side borders)
	ih := height - 2 // inner height (body rows)

	border := lipgloss.NewStyle().Foreground(colDim)
	ts := titleDimStyle
	if focused {
		border = lipgloss.NewStyle().Foreground(colAccent)
		ts = titleActiveStyle
	}

	label := " " + title + " "
	maxLabel := iw - 3 // room for "╭─" + a trailing corner dash and "╮"
	if maxLabel < 0 {
		maxLabel = 0
	}
	if lipgloss.Width(label) > maxLabel {
		label = ansi.Truncate(label, maxLabel, "")
	}
	fill := iw - lipgloss.Width(label) - 1
	if fill < 0 {
		fill = 0
	}
	top := border.Render("╭─") + ts.Render(label) + border.Render(strings.Repeat("─", fill)+"╮")

	lines := strings.Split(body, "\n")
	rows := make([]string, 0, ih)
	for i := 0; i < ih; i++ {
		content := ""
		if i < len(lines) {
			content = lines[i]
		}
		content = ansi.Truncate(content, iw, "")
		if pad := iw - lipgloss.Width(content); pad > 0 {
			content += strings.Repeat(" ", pad)
		}
		rows = append(rows, border.Render("│")+content+border.Render("│"))
	}

	bottom := border.Render("╰" + strings.Repeat("─", iw) + "╯")
	return strings.Join(append(append([]string{top}, rows...), bottom), "\n")
}
