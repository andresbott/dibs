package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// overlay splices the multi-line fg over bg starting at cell column x, row y.
// It is width- and ANSI-aware: background lines carrying colour codes keep
// their display width, so the composite never drifts horizontally.
func overlay(bg, fg string, x, y int) string {
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")
	for i, fgLine := range fgLines {
		row := y + i
		for row >= len(bgLines) {
			bgLines = append(bgLines, "")
		}
		bgLines[row] = overlayLine(bgLines[row], fgLine, x)
	}
	return strings.Join(bgLines, "\n")
}

// overlayLine replaces the display cells [x, x+width(fg)) of bg with fg,
// keeping the untouched left and right segments (and their ANSI state) intact.
func overlayLine(bg, fg string, x int) string {
	bgw := ansi.StringWidth(bg)
	fgw := ansi.StringWidth(fg)

	left := ansi.Truncate(bg, x, "")
	if bgw < x {
		left += strings.Repeat(" ", x-bgw)
	}

	right := ""
	if bgw > x+fgw {
		right = ansi.TruncateLeft(bg, x+fgw, "")
	}
	return left + fg + right
}

// placeCenter overlays fg centered (floored) over bg.
func placeCenter(bg, fg string) string {
	x := (lipgloss.Width(bg) - lipgloss.Width(fg)) / 2
	y := (lipgloss.Height(bg) - lipgloss.Height(fg)) / 2
	return overlay(bg, fg, x, y)
}
