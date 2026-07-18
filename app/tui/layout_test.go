package tui

import (
	"strings"
	"testing"

	"github.com/andresbott/netcheckout/internal/config"
	"github.com/andresbott/netcheckout/internal/localstat"
	"github.com/andresbott/netcheckout/internal/sanity"
	"github.com/charmbracelet/lipgloss"
)

func TestRenderHeaderFitsAndShowsIdentity(t *testing.T) {
	h := renderHeader(60, "1.2.3", "andres@thinkpad")
	if w := lipgloss.Width(h); w > 60 {
		t.Errorf("header width %d > 60", w)
	}
	if !strings.Contains(h, "1.2.3") || !strings.Contains(h, "andres@thinkpad") {
		t.Errorf("header missing version/identity: %q", h)
	}
}

func TestRenderFooterFitsAndHasHints(t *testing.T) {
	f := renderFooter(80)
	if w := lipgloss.Width(f); w > 80 {
		t.Errorf("footer width %d > 80", w)
	}
	if !strings.Contains(f, "Add") || !strings.Contains(f, "Quit") {
		t.Errorf("footer missing hints: %q", f)
	}
	if !strings.Contains(f, "Identity") {
		t.Errorf("footer missing Identity hint: %q", f)
	}
}

func TestHintUsesColonFormat(t *testing.T) {
	if got := hint("a", "Add"); !strings.Contains(got, "a: Add") {
		t.Errorf("hint should render \"a: Add\", got %q", got)
	}
}

func TestRenderDetailsShowsRoots(t *testing.T) {
	d := renderDetails("photos", config.Profile{LocalRoot: "/home/me/pics", RemoteRoot: "/mnt/nas/pics"}, nil, 40)
	for _, want := range []string{"photos", "/home/me/pics", "/mnt/nas/pics"} {
		if !strings.Contains(d, want) {
			t.Errorf("details missing %q:\n%s", want, d)
		}
	}
}

func TestRenderDetailsShowsSubpaths(t *testing.T) {
	p := config.Profile{
		LocalRoot:  "/home/me/pics",
		RemoteRoot: "/mnt/nas/pics",
		Subpaths:   []string{"docs", "src/app"},
	}
	d := renderDetails("photos", p, nil, 40)
	for _, want := range []string{"Subpaths (2)", "docs", "src/app"} {
		if !strings.Contains(d, want) {
			t.Errorf("details missing %q:\n%s", want, d)
		}
	}
}

func TestRenderDetailsWithoutSubpathsHasNoHeader(t *testing.T) {
	d := renderDetails("photos", config.Profile{LocalRoot: "/home/me/pics", RemoteRoot: "/mnt/nas/pics"}, nil, 40)
	if strings.Contains(d, "Subpaths") {
		t.Errorf("details should not mention subpaths when none are set:\n%s", d)
	}
}

func TestRenderDetailsChecking(t *testing.T) {
	d := renderDetails("photos", config.Profile{LocalRoot: "/l", RemoteRoot: "/r"}, nil, 40)
	if !strings.Contains(d, "… checkout") {
		t.Errorf("nil result should render '… checkout' (checkoutLine pending):\n%s", d)
	}
}

func TestRenderDetailsMarksRoots(t *testing.T) {
	d := renderDetails("photos", config.Profile{LocalRoot: "/l", RemoteRoot: "/r"},
		&sanity.Result{LocalRoot: true, RemoteRoot: false}, 40)
	if !strings.Contains(d, "✓") {
		t.Errorf("present local root should show ✓:\n%s", d)
	}
	if !strings.Contains(d, "✗") {
		t.Errorf("missing remote root should show ✗:\n%s", d)
	}
}

func TestRenderDetailsCheckoutStates(t *testing.T) {
	base := config.Profile{LocalRoot: "/l", RemoteRoot: "/r"}
	notOut := renderDetails("p", base, &sanity.Result{RemoteRoot: true, CheckedOut: false}, 40)
	if !strings.Contains(notOut, "not checked out") {
		t.Errorf("want 'not checked out':\n%s", notOut)
	}
	out := renderDetails("p", base, &sanity.Result{RemoteRoot: true, CheckedOut: true}, 40)
	if !strings.Contains(out, "checked out") || strings.Contains(out, "not checked out") {
		t.Errorf("want 'checked out' (not 'not checked out'):\n%s", out)
	}
	down := renderDetails("p", base, &sanity.Result{RemoteRoot: false}, 40)
	if !strings.Contains(down, "? checkout") {
		t.Errorf("unmounted remote should show '? checkout':\n%s", down)
	}
}

func TestRenderDetailsSubpathMarks(t *testing.T) {
	// "zeta"/"qux" (rather than "a"/"b") so the assertions can't be satisfied by
	// the "Subpaths" header word itself, which contains both "a" and "b".
	p := config.Profile{LocalRoot: "/l", RemoteRoot: "/r", Subpaths: []string{"zeta", "qux"}}
	res := &sanity.Result{RemoteRoot: true, Subpaths: []sanity.Subpath{{Path: "zeta", Exists: true}, {Path: "qux", Exists: false}}}
	d := renderDetails("p", p, res, 40)
	for _, want := range []string{"Subpaths (2)", "zeta", "qux", "✓", "✗"} {
		if !strings.Contains(d, want) {
			t.Errorf("details missing %q:\n%s", want, d)
		}
	}
}

// TestRenderDetailsSubpathMarksPendingAndUnknown covers subpathMark's other two
// branches (a pending nil result, and a result whose remote isn't mounted),
// which TestRenderDetailsSubpathMarks and TestRenderDetailsChecking don't
// exercise. The assertions key off "<mark> <subpath name>" rather than a bare
// "…"/"?" so they can only pass if subpathMark itself produced that mark —
// existMark and checkoutLine emit the same glyphs elsewhere in renderDetails'
// output, which would make a bare-glyph check pass vacuously.
func TestRenderDetailsSubpathMarksPendingAndUnknown(t *testing.T) {
	p := config.Profile{LocalRoot: "/l", RemoteRoot: "/r", Subpaths: []string{"zeta", "qux"}}

	pending := renderDetails("p", p, nil, 40)
	if !strings.Contains(pending, "… zeta") || !strings.Contains(pending, "… qux") {
		t.Errorf("nil result should render pending '…' marks on each subpath line:\n%s", pending)
	}

	unknown := renderDetails("p", p, &sanity.Result{RemoteRoot: false}, 40)
	if !strings.Contains(unknown, "? zeta") || !strings.Contains(unknown, "? qux") {
		t.Errorf("unmounted remote should render '?' marks on each subpath line:\n%s", unknown)
	}
}

func TestRenderDetailsShowsUnlistedLocal(t *testing.T) {
	res := &sanity.Result{
		LocalRoot:     true,
		RemoteRoot:    true,
		UnlistedLocal: []string{"top.txt", "c"},
	}
	p := config.Profile{LocalRoot: "/l", RemoteRoot: "/r", Subpaths: []string{"a"}}
	out := renderDetails("p", p, res, 80)
	if !strings.Contains(out, "top.txt") || !strings.Contains(out, "c") {
		t.Errorf("renderDetails should list unlisted local paths, got:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "not synced") {
		t.Errorf("renderDetails should warn these are not synced, got:\n%s", out)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:       "0 B",
		512:     "512 B",
		1024:    "1.0 KB",
		1536:    "1.5 KB",
		1048576: "1.0 MB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q; want %q", in, got, want)
		}
	}
}

func TestGroupThousands(t *testing.T) {
	cases := map[int]string{0: "0", 12: "12", 999: "999", 1234: "1,234", 1234567: "1,234,567"}
	for in, want := range cases {
		if got := groupThousands(in); got != want {
			t.Errorf("groupThousands(%d) = %q; want %q", in, got, want)
		}
	}
}

func TestContentsBlockIdleIsEmpty(t *testing.T) {
	if got := contentsBlock(nil, false, nil); got != "" {
		t.Errorf("idle contentsBlock should be empty, got %q", got)
	}
}

func TestContentsBlockScanning(t *testing.T) {
	if got := contentsBlock(nil, true, nil); !strings.Contains(got, "scanning") {
		t.Errorf("scanning contentsBlock should show a pending line, got %q", got)
	}
}

func TestContentsBlockShowsStats(t *testing.T) {
	got := contentsBlock(&localstat.Stats{Dirs: 12, Files: 3456, Bytes: 1536}, false, nil)
	for _, want := range []string{"Contents", "Folders", "12", "Files", "3,456", "Size", "1.5 KB"} {
		if !strings.Contains(got, want) {
			t.Errorf("contentsBlock missing %q:\n%s", want, got)
		}
	}
}
