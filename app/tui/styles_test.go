package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestTitledBoxDimensions(t *testing.T) {
	box := titledBox("Profiles", "line one\nline two", 24, 6, true)
	if w := lipgloss.Width(box); w != 24 {
		t.Errorf("width = %d, want 24", w)
	}
	if h := lipgloss.Height(box); h != 6 {
		t.Errorf("height = %d, want 6", h)
	}
	if !strings.Contains(box, "Profiles") {
		t.Errorf("box missing title:\n%s", box)
	}
	if !strings.Contains(box, "╭") || !strings.Contains(box, "╯") {
		t.Errorf("box missing rounded corners:\n%s", box)
	}
}

// A title wider than the box must not push the border past the requested width.
func TestTitledBoxLongTitleFitsWidth(t *testing.T) {
	box := titledBox("a very long title that overflows", "body", 16, 4, false)
	if w := lipgloss.Width(box); w != 16 {
		t.Fatalf("width = %d, want 16", w)
	}
}
