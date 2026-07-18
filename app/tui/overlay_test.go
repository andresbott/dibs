package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestOverlayPlacesForeground(t *testing.T) {
	bg := strings.Join([]string{".....", ".....", "....."}, "\n")
	got := overlay(bg, "XX", 1, 1)
	want := strings.Join([]string{".....", ".XX..", "....."}, "\n")
	if got != want {
		t.Fatalf("overlay = %q, want %q", got, want)
	}
}

func TestOverlayPreservesWidthOverStyledBackground(t *testing.T) {
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	bg := strings.Join([]string{styled.Render("abcde"), styled.Render("fghij")}, "\n")
	got := overlay(bg, "##", 2, 0)
	// The colored background line still measures 5 cells after splicing.
	firstLine := strings.Split(got, "\n")[0]
	if w := lipgloss.Width(firstLine); w != 5 {
		t.Fatalf("width after overlay = %d, want 5", w)
	}
	if !strings.Contains(firstLine, "##") {
		t.Fatalf("overlay content missing: %q", firstLine)
	}
}

func TestOverlayBelowBackgroundPads(t *testing.T) {
	got := overlay("ab", "XY", 0, 2)
	if got != "ab\n\nXY" {
		t.Fatalf("overlay below bg = %q, want %q", got, "ab\n\nXY")
	}
}

func TestPlaceCenter(t *testing.T) {
	bg := strings.Join([]string{"........", "........", "........", "........"}, "\n")
	got := placeCenter(bg, "AA")
	lines := strings.Split(got, "\n")
	// bg is 8 wide, 4 tall; "AA" is 2x1 -> x=3, y=1 (integer center, floor).
	if lines[1] != "...AA..." {
		t.Fatalf("centered line = %q, want %q", lines[1], "...AA...")
	}
}

func TestPlaceCenterForegroundLargerThanBackground(t *testing.T) {
	bg := "ab\ncd"
	fg := "WXYZ\nWXYZ\nWXYZ"
	got := placeCenter(bg, fg) // must not panic; clamps to (0,0)
	if lipgloss.Width(got) < lipgloss.Width(fg) {
		t.Fatalf("placeCenter dropped width: got %d, want >= %d", lipgloss.Width(got), lipgloss.Width(fg))
	}
	if !strings.Contains(got, "WXYZ") {
		t.Fatalf("placeCenter lost foreground:\n%s", got)
	}
}
