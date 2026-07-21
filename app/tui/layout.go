package tui

import (
	"fmt"
	"strings"

	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/internal/localstat"
	"github.com/andresbott/dibs/internal/sanity"
	"github.com/andresbott/dibs/internal/status"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// renderHeader is the top bar: "dibs <version>" left, identity right.
func renderHeader(width int, version, identity string) string {
	left := headerAppStyle.Render("dibs") + " " + headerIDStyle.Render(version)
	right := headerIDStyle.Render(identity)
	gap := width - 1 - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := " " + left + strings.Repeat(" ", gap) + right
	return ansi.Truncate(line, width, "")
}

// hint renders one "key: label" entry — the key accent-coloured, the ": label"
// dim — shared by the footer bars and the add/edit form's help line.
func hint(k, label string) string {
	return helpKeyStyle.Render(k) + helpTextStyle.Render(": "+label)
}

// renderFooter is the main view's bottom key-hint bar.
func renderFooter(width int) string {
	parts := []string{
		hint("a", "Add"), hint("e", "Edit"), hint("d", "Delete"),
		hint("i", "Identity"), hint("↵", "Actions"), hint("q", "Quit"),
	}
	return ansi.Truncate(" "+strings.Join(parts, "  "), width, "")
}

// renderProfileFooter is the actions-substate bottom key-hint bar. Each
// action's options (steal lock, allow deletes, local wins, clean, abandon)
// live as checkboxes in that action's confirm dialog, not on this bar. The
// hints are pane-aware: the Actions pane offers Run/Select plus a Tab to the
// Activity panel; the Activity pane offers the scroll keys plus a Tab back.
// While an action runs only monitoring and canceling apply, so the bar shows
// just those.
func renderProfileFooter(width int, activityFocused, running bool) string {
	var parts []string
	switch {
	case running:
		parts = []string{
			hint("↑↓/⇧↑↓/PgUp/PgDn", "Scroll"), hint("←→", "Filter"), hint("esc", "Cancel"),
		}
	case activityFocused:
		parts = []string{
			hint("↑↓/⇧↑↓/PgUp/PgDn", "Scroll"), hint("←→", "Filter"), hint("tab", "Actions"), hint("esc", "Back"),
		}
	default:
		parts = []string{
			hint("↵", "Run"), hint("↑↓", "Select"), hint("tab", "Activity"), hint("esc", "Back"),
		}
	}
	return ansi.Truncate(" "+strings.Join(parts, "  "), width, "")
}

// detailLabel renders a fixed-width faint field label, shared by renderDetails
// and contentsBlock so their value columns line up.
func detailLabel(s string) string { return labelStyle.Render(fmt.Sprintf("%-8s", s)) }

// renderDetails is the profile summary body, styled to match the Actions box: an
// accented profile-name header, a spacer, the checkout state, then a marked row
// per root, and (when scoped) a Subpaths section. Every row carries a one-space
// left margin and each mark sits at the start of its line so titledBox's width
// clip trims the path tail, never the mark. Sections are separated by a blank
// line for breathing room.
func renderDetails(name string, p config.Profile, res *sanity.Result, width int) string {
	if name == "" {
		return " " + helpTextStyle.Render("No profile selected.")
	}
	var b strings.Builder
	// Accented name header + spacer, mirroring renderActions.
	b.WriteString(" " + profileNameStyle.Render(name))
	b.WriteString("\n")
	// Checkout state sits above the roots as its own group.
	b.WriteString("\n " + checkoutLine(res))
	b.WriteString("\n\n " + existMark(res, res != nil && res.LocalRoot) + " " + detailLabel("Local") + p.LocalRoot)
	b.WriteString("\n " + existMark(res, res != nil && res.RemoteRoot) + " " + detailLabel("Remote") + p.RemoteRoot)
	if len(p.Subpaths) > 0 {
		b.WriteString("\n\n " + labelStyle.Render(fmt.Sprintf("Subpaths (%d)", len(p.Subpaths))))
		for i, sub := range p.Subpaths {
			b.WriteString("\n " + subpathMark(res, i) + " " + sub)
		}
	}
	if res != nil && len(res.UnlistedLocal) > 0 {
		b.WriteString("\n\n " + errStyle.Render(fmt.Sprintf("⚠ Not synced — outside subpaths (%d)", len(res.UnlistedLocal))))
		for _, u := range res.UnlistedLocal {
			b.WriteString("\n " + errStyle.Render("✗") + " " + u)
		}
	}
	return b.String()
}

// existMark is the glyph for a root existence check: "…" while pending (res nil),
// green ✓ if present, red ✗ if missing.
func existMark(res *sanity.Result, ok bool) string {
	switch {
	case res == nil:
		return helpTextStyle.Render("…")
	case ok:
		return okStyle.Render("✓")
	default:
		return errStyle.Render("✗")
	}
}

// checkoutLine is the one-line checkout state, using a filled/hollow status dot
// distinct from the roots' ✓/✗ existence marks: pending ("…"), unknown ("?",
// remote not mounted so no marker can be read), a green ● when checked out, or a
// dim ○ for the benign "not checked out" (a normal state, not an error).
func checkoutLine(res *sanity.Result) string {
	switch {
	case res == nil:
		return helpTextStyle.Render("… checkout")
	case !res.RemoteRoot:
		return helpTextStyle.Render("? checkout")
	case res.CheckedOut:
		return okStyle.Render("●") + " checked out"
	default:
		return helpTextStyle.Render("○ not checked out")
	}
}

// subpathMark is the glyph for the i-th declared subpath: pending, "?" when the
// remote isn't mounted (can't tell), green ✓ if present on the remote, red ✗ if
// missing.
func subpathMark(res *sanity.Result, i int) string {
	switch {
	case res == nil:
		return helpTextStyle.Render("…")
	case !res.RemoteRoot:
		return helpTextStyle.Render("?")
	case i < len(res.Subpaths) && res.Subpaths[i].Exists:
		return okStyle.Render("✓")
	default:
		return errStyle.Render("✗")
	}
}

// contentsBlock is the local-tree summary appended to the Details box after the
// Status action scans a profile: a faint "Contents" header and aligned
// Folders/Files/Size rows. It is empty while idle (no scan run) so Details is
// unchanged during browsing, a faint pending line while a scan is in flight, and
// a styled error if the scan failed. The returned string leads with "\n" when
// non-empty so it appends cleanly onto the existing body.
func contentsBlock(stats *localstat.Stats, scanning bool, err error) string {
	switch {
	case scanning:
		return "\n\n " + helpTextStyle.Render("… scanning")
	case err != nil:
		return "\n\n " + errStyle.Render("scan failed: "+err.Error())
	case stats == nil:
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n " + labelStyle.Render("Contents"))
	// Three leading spaces align the value column with the marked rows above
	// (" ✓ Local"), whose label starts after the reserved mark column.
	b.WriteString("\n   " + detailLabel("Folders") + groupThousands(stats.Dirs))
	b.WriteString("\n   " + detailLabel("Files") + groupThousands(stats.Files))
	b.WriteString("\n   " + detailLabel("Size") + humanBytes(stats.Bytes))
	return b.String()
}

// pendingBlock is the sync-plan summary appended to the Details box after the
// Status action ran: a faint "Pending" header and one aligned count row per
// verb present in the plan (Add/Modify/Delete/Conflict/Ignored) — zero rows
// are dropped so the box stays compact. A plan with nothing pending reads
// "in sync" (ignored paths still counted: a sync never touches them either
// way). Empty while no Status result is in, or when the result carries no
// plan (not checked out, or no local baseline). Like contentsBlock, the
// returned string leads with "\n" when non-empty so it appends cleanly.
func pendingBlock(st *status.ProfileStatus) string {
	if st == nil || !st.CheckedOut || !st.HasBaseline {
		return ""
	}
	var add, modify, del, conflict, ignored int
	for _, t := range st.Targets {
		for _, c := range t.Push {
			if c.Modify {
				modify++
			} else {
				add++
			}
		}
		for _, c := range t.Pull {
			if c.Modify {
				modify++
			} else {
				add++
			}
		}
		del += len(t.LocalDeletes) + len(t.RemoteDeletes)
		conflict += len(t.Conflicts)
		ignored += len(t.Ignored)
	}
	var b strings.Builder
	b.WriteString("\n\n " + labelStyle.Render("Pending"))
	if add+modify+del+conflict == 0 {
		b.WriteString("\n   " + okStyle.Render("in sync"))
	}
	// Three leading spaces align the value column with the marked rows above,
	// mirroring contentsBlock.
	row := func(label string, n int) {
		if n > 0 {
			b.WriteString("\n   " + detailLabel(label) + groupThousands(n))
		}
	}
	row("Add", add)
	row("Modify", modify)
	row("Delete", del)
	row("Conflict", conflict)
	row("Ignored", ignored)
	return b.String()
}

// humanBytes renders a byte count as a base-1024 size: whole bytes below 1 KB,
// otherwise one decimal with a binary unit suffix.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	if exp >= len(units) {
		exp = len(units) - 1
	}
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), units[exp])
}

// groupThousands formats a non-negative count with comma thousands separators,
// e.g. 1234567 -> "1,234,567".
func groupThousands(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
