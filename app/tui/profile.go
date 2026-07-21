package tui

import (
	"fmt"
	"strings"

	"github.com/andresbott/dibs/internal/ident"
	"github.com/andresbott/dibs/internal/lifecycle"
	"github.com/andresbott/dibs/internal/localstat"
	"github.com/andresbott/dibs/internal/sanity"
	"github.com/andresbott/dibs/internal/status"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// profileModel is the state for the Actions box: which profile is open, which
// action row is selected, and the outcome of the most recent Status run. The
// profile's roots are looked up fresh from cfg at render time, so only the name
// is stored here.
type profileModel struct {
	name     string
	cursor   int
	checking bool                  // a Status compute is in flight
	result   *status.ProfileStatus // last successful Status result; nil until run
	err      error                 // last Status error; nil if none
	acting   bool                  // a mutating action (Checkout) is in flight
	applied  []lifecycle.Event     // changes streamed live by the in-flight/last Sync or Check-in
	// progress tracks the in-flight sync's per-verb counters and bar: planned
	// totals snapshotted from the last Status result at launch (nil totals =
	// indeterminate), done counts fed by the event stream. nil while no sync
	// is running and after its result (or cancel) lands.
	progress     *syncProgress
	actionReport *lifecycle.Report // last successful action's outcome; nil until run
	actionErr    error             // last action error; nil if none
	scanning     bool              // a local file-stat scan is in flight (part of Status)
	fileStats    *localstat.Stats  // last successful local scan; nil until run
	statErr      error             // last local-scan error; nil if none
	statusScroll int               // first visible Activity line; scrolled with ↑↓/shift+↑↓/PgUp/PgDn
	// opFilter narrows the Activity change list to one operation group (an opKey
	// like "add → remote"); empty shows everything. Cycled with ←→ through the
	// groups present in the current body, and reset whenever a new run starts.
	opFilter string
	canceled bool // the in-flight action was stopped via Esc; shows a "Canceled." note
}

func newProfileView(name string) profileModel { return profileModel{name: name} }

// visibleActions returns only the actions that apply to the profile's known
// checkout state (sanity), in display order — inapplicable actions are hidden,
// not greyed out. A not-checked-out profile can only be checked out; one
// checked out by THIS profile (id + profileID) offers Status, Sync, then
// Check-in. A foreign lock — held elsewhere, by another profile on this
// machine, or a marker too corrupt to attribute — offers only Checkout, whose
// dialog is where the lock can be stolen: Sync and Check-in always refuse a
// foreign lock, so they are hidden. A nil sanity result (the stat-only check
// hasn't returned yet) yields no actions, and the box shows a "checking…" note
// instead. The lifecycle Runner stays the real guard.
func visibleActions(r *sanity.Result, id ident.Ident, profileID string) []string {
	switch {
	case r == nil:
		return nil
	case r.CheckedOut && r.Marker != nil && r.Marker.OwnedBy(id.By, id.Host, profileID):
		return []string{"Status", "Sync", "Check-in"}
	default:
		return []string{"Checkout"}
	}
}

// clampCursor keeps the cursor within [0, n) after the visible action list
// changes length (e.g. a checkout flips the list from [Checkout] to
// [Status, Sync, Check-in], or a check-in the other way).
func (p *profileModel) clampCursor(n int) {
	if p.cursor >= n {
		p.cursor = n - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

func (p *profileModel) moveUp() {
	if p.cursor > 0 {
		p.cursor--
	}
}

func (p *profileModel) moveDown(n int) {
	if p.cursor < n-1 {
		p.cursor++
	}
}

// opKey identifies one operation group in the Activity change list — the verb
// plus the side it lands on, e.g. "add → remote" — used to match rows against
// the ←→ operation filter.
func opKey(verb, side string) string { return verb + " → " + side }

// statusOpKeys returns the distinct operation groups present in a Status
// result, in the order statusBody renders them, so the ←→ filter cycles in
// display order.
func statusOpKeys(st status.ProfileStatus) []string {
	var keys []string
	seen := make(map[string]bool)
	add := func(verb, side string) {
		k := opKey(verb, side)
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for _, t := range st.Targets {
		for _, c := range t.Push {
			add(changeVerb(c.Modify).verb, "remote")
		}
		for _, c := range t.Pull {
			add(changeVerb(c.Modify).verb, "local")
		}
		if len(t.LocalDeletes) > 0 {
			add("delete", "local")
		}
		if len(t.RemoteDeletes) > 0 {
			add("delete", "remote")
		}
		if len(t.Conflicts) > 0 {
			add("conflict", "both")
		}
		if len(t.Ignored) > 0 {
			add("ignored", "-")
		}
	}
	return keys
}

// appliedOpKeys returns the distinct operation groups present in an applied
// change list (the live/last Sync, Checkout, or Check-in stream), in first-seen
// order.
func appliedOpKeys(events []lifecycle.Event) []string {
	var keys []string
	seen := make(map[string]bool)
	for _, e := range events {
		k := opKey(appliedVerb(e.Kind).verb, sideLabel(e.Side))
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	return keys
}

// activityOpKeys returns the operation groups the ←→ filter can cycle through
// for the Activity body currently showing. It mirrors renderStatus's case
// order: only the applied change list and the Status change list are
// filterable; every other body (errors, conflicts, placeholders) yields none.
func (p profileModel) activityOpKeys() []string {
	switch {
	case p.canceled:
		return nil
	case p.acting:
		return appliedOpKeys(p.applied)
	case p.actionReport != nil && len(p.actionReport.Conflicts) > 0:
		return nil
	case p.actionErr != nil:
		return nil
	case p.actionReport != nil:
		return appliedOpKeys(p.applied)
	case p.checking:
		return nil
	case p.err != nil:
		return nil
	case p.result != nil:
		return statusOpKeys(*p.result)
	default:
		return nil
	}
}

// actionGlyph is the leading icon for an action row: a pull arrow for Checkout,
// a summary bars glyph for Status, a two-way arrow for Sync, and a push arrow
// for Check-in. All are single-width so the rows stay aligned.
func actionGlyph(a string) string {
	switch a {
	case "Checkout":
		return "↓"
	case "Status":
		return "≡"
	case "Sync":
		return "⇅"
	case "Check-in":
		return "↑"
	default:
		return " "
	}
}

// renderActions is the Actions box body: one glyph-prefixed row per applicable
// action (visibleActions) — the selected row marked and accented. The profile
// name and checkout-state indicator both live in the Details box below, so the
// Actions box stays focused on the action list; before sanity has returned there
// are no actions. While an action runs the whole list renders dimmed with no
// cursor marker: nothing can be launched until the run finishes or is canceled.
func renderActions(cursor, width int, res *sanity.Result, id ident.Ident, profileID string, running bool) string {
	var b strings.Builder
	for i, a := range visibleActions(res, id, profileID) {
		if i > 0 {
			b.WriteString("\n")
		}
		var row string
		switch {
		case running:
			row = "  " + helpTextStyle.Render(actionGlyph(a)+" "+a)
		case i == cursor:
			row = "▸ " + selectedRowStyle.Render(actionGlyph(a)+" "+a)
		default:
			row = "  " + helpTextStyle.Render(actionGlyph(a)) + " " + a
		}
		b.WriteString(ansi.Truncate(row, width, ""))
	}
	return b.String()
}

// renderActivity is the right-column placeholder while browsing the profile
// list: no action can run from here, so it points at the actions view.
func renderActivity() string {
	return helpTextStyle.Render("No activity — open a profile's actions (↵) to run one.")
}

// renderToggleHelp is the Activity body while a profile's actions are idle (no
// action run yet): it points at where each action's options live, since they
// are chosen per run in that action's confirm dialog.
func renderToggleHelp() string {
	var b strings.Builder
	b.WriteString(helpTextStyle.Render("Run an action to see its progress here."))
	b.WriteString("\n\n" + helpTextStyle.Render("Each action asks for its options before running:"))
	b.WriteString("\n\n " + helpTextStyle.Render("Sync — allow deletes (off: deletes are skipped and"))
	b.WriteString("\n " + helpTextStyle.Render("reported pending) and local wins conflicts."))
	b.WriteString("\n\n " + helpTextStyle.Render("Checkout — steal the lock, offered only when the"))
	b.WriteString("\n " + helpTextStyle.Render("profile is checked out by someone else."))
	return b.String()
}

// renderStatus is the Activity box body while a profile's actions are showing:
// the idle placeholder before Status has run, an in-flight "Checking…", a styled
// error, or the formatted result of the most recent Status run. width is the
// inner panel width, used to size the per-target dividers; titledBox clips the
// body to the panel, so no other width handling is needed here. The profile name
// is not repeated — the Details box already shows it.
//
// A stopped-on-conflict action carries both a report (with the conflicting
// paths) and an error, so the conflict path list takes precedence over the
// bare error message; any other action error (e.g. a lock/mount failure, where
// the report is empty) still renders as an error rather than an empty body.
func renderStatus(p profileModel, width int) string {
	switch {
	case p.canceled:
		// Wins over any leftover state (a prior result, a dropped straggler's report)
		// until the next action clears it.
		return "Canceled."
	case p.acting:
		// Show applied changes as they stream in; before the first one arrives,
		// a bare "Working…".
		return filterHeader(p.opFilter) + appliedBody(p.applied, "Working…", p.opFilter)
	case p.actionReport != nil && len(p.actionReport.Conflicts) > 0:
		return conflictBody(*p.actionReport)
	case p.actionErr != nil:
		return errStyle.Render(p.actionErr.Error())
	case p.actionReport != nil:
		return actionBody(p)
	case p.checking:
		return "Checking…"
	case p.err != nil:
		return errStyle.Render(p.err.Error())
	case p.result != nil:
		return filterHeader(p.opFilter) + statusBody(*p.result, width, p.opFilter)
	default:
		return renderToggleHelp()
	}
}

// filterHeader is the line above a filtered change list naming the active
// operation group, so a narrowed view is never mistaken for the full one.
// Empty (no line) when no filter is active.
func filterHeader(filter string) string {
	if filter == "" {
		return ""
	}
	return helpTextStyle.Render("filter: ") + selectedRowStyle.Render(filter) + "\n"
}

// actionBody formats a completed mutating action: the full list of applied
// changes (in the same rows the live stream used) followed by a one-line summary.
func actionBody(p profileModel) string {
	rep := *p.actionReport
	dels := len(rep.RemovedRemote) + len(rep.RemovedLocal)
	summary := fmt.Sprintf("%s: pull %d, push %d, del %d",
		rep.Action, len(rep.Pulled), len(rep.Pushed), dels)
	if pend := len(rep.PendingRemote) + len(rep.PendingLocal); pend > 0 {
		summary += fmt.Sprintf(", %d delete(s) pending (re-run Sync with \"allow deletes\")", pend)
	}
	if n := len(rep.Ignored); n > 0 {
		summary += fmt.Sprintf(", %d ignored", n)
	}
	if rep.Abandoned {
		// An abandon copies nothing, so a "pull 0, push 0" summary would misread
		// as a verified in-sync release.
		summary = rep.Action + ": abandoned (lock released, nothing synced)"
	}
	if len(p.applied) == 0 {
		return summary
	}
	return filterHeader(p.opFilter) + appliedBody(p.applied, "", p.opFilter) + "\n\n" + summary
}

// conflictBody renders a stopped-on-conflict action: nothing was written, so the
// conflicting paths are listed instead of an applied change list.
func conflictBody(rep lifecycle.Report) string {
	var b strings.Builder
	b.WriteString("conflicts — nothing written:")
	for _, c := range rep.Conflicts {
		fmt.Fprintf(&b, "\n  ! %s", c)
	}
	return b.String()
}

// appliedBody renders a list of applied changes as status-view rows (verb → side
// path), or placeholder when the list is empty. A non-empty filter keeps only
// the rows of that operation group (opKey).
func appliedBody(events []lifecycle.Event, placeholder, filter string) string {
	if len(events) == 0 {
		return placeholder
	}
	var rows []string
	for _, e := range events {
		v, side := appliedVerb(e.Kind), sideLabel(e.Side)
		if filter != "" && opKey(v.verb, side) != filter {
			continue
		}
		rows = append(rows, changeRow(v, side, e.Path))
	}
	if len(rows) == 0 {
		return placeholder
	}
	return strings.Join(rows, "\n")
}

// appliedVerb maps an applied change's kind to its status-view verb and style.
func appliedVerb(k lifecycle.EventKind) verbStyle {
	switch k {
	case lifecycle.EventAdd:
		return verbStyle{"add", okStyle}
	case lifecycle.EventDelete:
		return verbStyle{"delete", errStyle}
	default:
		return verbStyle{"modify", lipgloss.NewStyle()}
	}
}

// sideLabel is the side token an applied change landed on.
func sideLabel(s lifecycle.Side) string {
	if s == lifecycle.SideRemote {
		return "remote"
	}
	return "local"
}

// statusBody formats a computed ProfileStatus as a scrollable change list: the
// three-way reconcile plan a sync would carry out, grouped per target. Each
// target shows its label followed by one row per pending change, read as a verb
// and the side it lands on ("add → remote", "delete → local", "conflict →
// both"), or "no changes" when that target is in sync. Targets are separated by a
// width-spanning divider. width sizes that divider; titledBox clips overlong rows.
// A non-empty filter keeps only the rows of that operation group (opKey);
// targets with no matching rows are dropped entirely.
func statusBody(st status.ProfileStatus, width int, filter string) string {
	if !st.CheckedOut {
		return "not checked out"
	}
	if !st.HasBaseline {
		return "checked out, but no local baseline on this machine"
	}
	divider := helpTextStyle.Render(strings.Repeat("─", width))
	var sections []string
	for _, t := range st.Targets {
		var b strings.Builder
		keep := func(v verbStyle, side, path string) {
			if filter == "" || opKey(v.verb, side) == filter {
				writeChange(&b, v, side, path)
			}
		}
		// Copies: pushes travel local → remote, pulls remote → local. Deletes
		// mirror or propagate a removal; conflicts changed on both sides.
		for _, c := range t.Push {
			keep(changeVerb(c.Modify), "remote", c.Path)
		}
		for _, c := range t.Pull {
			keep(changeVerb(c.Modify), "local", c.Path)
		}
		for _, p := range t.LocalDeletes {
			keep(verbStyle{"delete", errStyle}, "local", p)
		}
		for _, p := range t.RemoteDeletes {
			keep(verbStyle{"delete", errStyle}, "remote", p)
		}
		for _, p := range t.Conflicts {
			keep(verbStyle{"conflict", errStyle}, "both", p)
		}
		// Ignored paths render dimmed with a "-" side: a sync touches neither side.
		for _, p := range t.Ignored {
			keep(verbStyle{"ignored", helpTextStyle}, "-", p)
		}
		switch {
		case b.Len() > 0:
			sections = append(sections, t.Label()+b.String())
		case filter == "" && t.InSync():
			sections = append(sections, t.Label()+"\n  no changes")
		case filter == "":
			sections = append(sections, t.Label())
		}
		// A filtered-out target (rows exist but none match) is dropped.
	}
	if len(sections) == 0 {
		return helpTextStyle.Render("no changes match this filter")
	}
	return strings.Join(sections, "\n"+divider+"\n")
}

// verbStyle pairs a change's action word with its highlight style.
type verbStyle struct {
	verb  string
	style lipgloss.Style
}

// writeChange appends one change row under the current target, newline-prefixed
// so it hangs off the preceding target label or row.
func writeChange(b *strings.Builder, v verbStyle, side, path string) {
	b.WriteString("\n" + changeRow(v, side, path))
}

// changeRow formats one change row: a coloured verb, an arrow to the side it
// lands on, and the path. The verb and side tokens are padded before styling so
// the ANSI codes never disturb the column alignment.
func changeRow(v verbStyle, side, path string) string {
	return fmt.Sprintf("  %s → %s  %s",
		v.style.Render(fmt.Sprintf("%-8s", v.verb)),
		labelStyle.Render(fmt.Sprintf("%-6s", side)),
		path,
	)
}

// changeVerb maps a copy's add/modify flag to its action word and highlight
// style: a green "add" for a new file, a plain "modify" for an existing one.
func changeVerb(modify bool) verbStyle {
	if modify {
		return verbStyle{"modify", lipgloss.NewStyle()}
	}
	return verbStyle{"add", okStyle}
}
