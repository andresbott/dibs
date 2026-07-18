package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/andresbott/dibs/internal/lifecycle"
	"github.com/andresbott/dibs/internal/marker"
	"github.com/andresbott/dibs/libs/threewayrsync"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// confirmFocus is which control in the confirm dialog currently has focus.
// confirmFocusCancel is the zero value, so a freshly opened dialog is always
// safe by default: reaching the destructive action requires an explicit move,
// guarding against an accidental one-keystroke delete/check-in.
// The remaining entries are the per-dialog checkboxes: "abandon" and "delete
// local copy" (check-in), "steal the lock" (checkout on a foreign lock), and
// "allow deletes" / "local wins conflicts" (sync).
type confirmFocus int

const (
	confirmFocusCancel confirmFocus = iota
	confirmFocusDelete
	confirmFocusClean
	confirmFocusAbandon
	confirmFocusSteal
	confirmFocusAllowDeletes
	confirmFocusLocalWins
)

// confirmFocusRing is the Tab order for a dialog kind. Check-in, sync, and a
// foreign-lock checkout put their checkboxes ahead of the buttons; delete (and
// a checkout without a foreign lock) has buttons only; the informational wipe
// dialog has a single OK, so focus never moves. foreign only matters for
// confirmCheckout: it adds the steal checkbox.
func confirmFocusRing(kind confirmKind, foreign bool) []confirmFocus {
	switch {
	case kind == confirmCheckin:
		return []confirmFocus{confirmFocusAbandon, confirmFocusClean, confirmFocusDelete, confirmFocusCancel}
	case kind == confirmSync:
		return []confirmFocus{confirmFocusAllowDeletes, confirmFocusLocalWins, confirmFocusDelete, confirmFocusCancel}
	case kind == confirmCheckout && foreign:
		return []confirmFocus{confirmFocusSteal, confirmFocusDelete, confirmFocusCancel}
	case kind == confirmWipe:
		return []confirmFocus{confirmFocusCancel}
	default:
		return []confirmFocus{confirmFocusDelete, confirmFocusCancel}
	}
}

// confirmFocusStep moves focus dir steps (+1/-1) around the kind's ring, wrapping.
func confirmFocusStep(kind confirmKind, foreign bool, cur confirmFocus, dir int) confirmFocus {
	ring := confirmFocusRing(kind, foreign)
	idx := 0
	for i, f := range ring {
		if f == cur {
			idx = i
			break
		}
	}
	n := len(ring)
	return ring[((idx+dir)%n+n)%n]
}

// confirmCheckbox renders one of the check-in dialog's checkboxes: a filled
// [x] / empty [ ] box plus its label, accent+bold when focused, dim otherwise.
// Mirrors confirmButton's focus styling.
func confirmCheckbox(label string, checked, focused bool) string {
	box := "[ ]"
	if checked {
		box = "[x]"
	}
	st := lipgloss.NewStyle().Foreground(colDim)
	if focused {
		st = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	}
	return st.Render(box + " " + label)
}

// confirmKind selects which action the confirm modal is guarding: deleting a
// profile or checking one in. It changes the title, question, and
// activate-button label but shares all the layout/sizing and focus logic.
type confirmKind int

const (
	confirmDelete confirmKind = iota
	confirmCheckin
	confirmCheckout
	// confirmSync gathers the sync run's options — "allow deletes" and "local
	// wins conflicts" checkboxes — before Run starts it; Sync never runs
	// directly from the action list.
	confirmSync
	// confirmCancel guards stopping an already-running action (Sync/Checkout/
	// Check-in/Status) rather than starting one, so its "activate" button stops the
	// operation and its dismiss keeps it running.
	confirmCancel
	// confirmWipe opens when a sync with allow-deletes on was stopped by the
	// engine's wipe valve (the plan would delete every previously synced file on
	// one side — never applied, nothing waives it). It is purely informational —
	// a single OK closes it, nothing is re-run: the deliberate start-over path
	// (abandon + re-checkout) is named in the dialog text and taken from the
	// profile view, not from here.
	confirmWipe
)

// confirmButton renders a bracketed [ label ] button: accent+bold when it is the
// focused button, dim otherwise. Mirrors formModel.actionButton.
func confirmButton(label string, focused bool) string {
	st := lipgloss.NewStyle().Foreground(colDim)
	if focused {
		st = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	}
	return st.Render("[ " + label + " ]")
}

// confirmParams carries everything confirmModal renders beyond the dialog
// kind and profile name: which control is focused, the per-kind checkbox
// states, and the context that shapes the wording (the foreign lock holder for
// checkout, the engine stop for the wipe dialog).
type confirmParams struct {
	focus          confirmFocus
	abandon, clean bool           // check-in checkboxes
	steal          bool           // checkout "steal the lock" checkbox
	foreign        bool           // checkout opened on a foreign lock: show holder + steal
	holder         *marker.Marker // the foreign lock's marker; nil if unreadable
	allowDeletes   bool           // sync checkboxes
	localWins      bool
	wipe           *threewayrsync.WouldWipeError // confirmWipe only
}

// confirmParams gathers the model state the open confirm dialog renders from:
// the focus, every dialog kind's checkbox states, and the checkout dialog's
// foreign-lock context (the holder comes from the profile's sanity marker).
func (m model) confirmParams() confirmParams {
	var holder *marker.Marker
	if r := m.checks[m.confirmName]; r != nil {
		holder = r.Marker
	}
	return confirmParams{
		focus:        m.confirmFocus,
		abandon:      m.checkinAbandon,
		clean:        m.checkinClean,
		steal:        m.checkoutSteal,
		foreign:      m.confirmForeign,
		holder:       holder,
		allowDeletes: m.syncAllowDeletes,
		localWins:    m.syncLocalWins,
		wipe:         m.wipe,
	}
}

// confirmModal is the confirmation window shared by the delete, checkout,
// check-in, sync, and wipe flows; kind selects the title/question/
// activate-button wording and which checkboxes appear. termWidth caps the box
// so a long profile name can't push it past the edge of the screen (titledBox
// clips the question text to fit the box).
func confirmModal(kind confirmKind, name string, p confirmParams, termWidth int) string {
	if termWidth <= 0 {
		termWidth = 80 // matches mainView's pre-resize fallback
	}
	title, question, activate, dismiss := "Confirm delete", "Delete profile \""+name+"\"?", "Delete", "Cancel"
	switch kind {
	case confirmCheckin:
		title, question, activate = "Confirm check-in", "Check in and release \""+name+"\"?", "Check in"
	case confirmCheckout:
		title, question, activate = "Confirm checkout", "Check out \""+name+"\"?", "Check out"
		if p.foreign {
			question = checkoutForeignQuestion(name, p.holder)
		}
	case confirmSync:
		title, question, activate = "Confirm sync", "Sync \""+name+"\"?", "Run"
	case confirmCancel:
		// This dialog guards stopping a run, not starting one, so "Cancel" would be
		// ambiguous — the dismiss button keeps the operation running instead.
		title, question, activate, dismiss = "Confirm cancel", "Stop the running operation?", "Stop", "Keep running"
	case confirmWipe:
		title, question = "Sync stopped", wipeQuestion(name, p.wipe)
	}
	sep := helpTextStyle.Render(" · ")
	var buttons, help string
	if kind == confirmWipe {
		// Informational dialog: one OK, always focused, every dismiss key closes it.
		buttons = confirmButton("OK", true)
		help = hint("enter/esc", "Close")
	} else {
		buttons = lipgloss.JoinHorizontal(lipgloss.Top,
			confirmButton(activate, p.focus == confirmFocusDelete), "   ",
			confirmButton(dismiss, p.focus == confirmFocusCancel))
		escHint := "Cancel"
		if kind == confirmCancel {
			escHint = "Keep running"
		}
		help = hint("tab", "Move") + sep + hint("enter/space", "Activate") + sep + hint("esc", escHint)
	}

	// The per-kind option checkboxes sit above the buttons.
	checkbox := ""
	switch {
	case kind == confirmCheckin:
		checkbox = confirmCheckbox("abandon (release without sync check)", p.abandon, p.focus == confirmFocusAbandon) +
			"\n" + confirmCheckbox("delete local copy", p.clean, p.focus == confirmFocusClean)
	case kind == confirmSync:
		checkbox = confirmCheckbox("allow deletes (off: deletes are skipped, reported pending)", p.allowDeletes, p.focus == confirmFocusAllowDeletes) +
			"\n" + confirmCheckbox("local wins conflicts (off: sync stops on conflicts)", p.localWins, p.focus == confirmFocusLocalWins)
	case kind == confirmCheckout && p.foreign:
		checkbox = confirmCheckbox("steal the lock (take over their checkout)", p.steal, p.focus == confirmFocusSteal)
	}

	// The dialog gets web-style inner padding: dialogPad columns of margin on
	// each side of the content, plus one blank row above and below (added when
	// the body is assembled). Content width is the widest element; the box adds
	// borders (2) and both margins on top of that.
	contentW := lipgloss.Width(question)
	for _, el := range []string{help, checkbox, buttons} {
		if w := lipgloss.Width(el); w > contentW {
			contentW = w
		}
	}
	width := contentW + 2 + 2*dialogPad
	if max := termWidth - 8; width > max {
		width = max
	}
	if width < 30 {
		width = 30
	}
	contentW = width - 2 - 2*dialogPad

	// Buttons sit bottom-right, as in a web dialog; the rest is left-aligned.
	buttonRow := lipgloss.NewStyle().Width(contentW).Align(lipgloss.Right).Render(buttons)
	body := question
	if checkbox != "" {
		body += "\n\n" + checkbox
	}
	body += "\n\n" + buttonRow + "\n\n" + help
	body = padDialogBody(body, dialogPad)
	return titledBox(title, body, width, lipgloss.Height(body)+2, true)
}

// dialogPad is the confirm dialog's inner horizontal padding: columns between
// each side border and the content.
const dialogPad = 3

// padDialogBody indents every line of body by pad columns and adds one blank
// row above and below, so the dialog content floats inside its border instead
// of touching it (titledBox itself renders the body flush left).
func padDialogBody(body string, pad int) string {
	margin := strings.Repeat(" ", pad)
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = margin + l
		}
	}
	return "\n" + strings.Join(lines, "\n") + "\n"
}

// checkoutForeignQuestion renders the checkout dialog's question when the
// profile is locked by someone else: who holds it (mirroring the CLI refusal
// in lifecycle.Checkout) and what checking it out anyway means. A nil holder
// (marker present but unreadable) falls back to a generic phrasing.
func checkoutForeignQuestion(name string, holder *marker.Marker) string {
	if holder == nil {
		return fmt.Sprintf("\"%s\" is checked out by someone else\n(the lock marker cannot be read).\n\n"+
			"Check out requires stealing their lock.", name)
	}
	return fmt.Sprintf("\"%s\" is checked out by %s on %s\nsince %s.\n\n"+
		"Check out requires stealing their lock; their\nunsynced work would be overrun by the next sync.",
		name, holder.CheckedOutBy, holder.Host, holder.CheckedOutAt.Format("2006-01-02 15:04"))
}

// wipeQuestion renders the confirmWipe dialog's body: what the engine refused
// and how to proceed deliberately. The wording mirrors the CLI's wipe-refusal
// message, split across lines so it reads as a dialog rather than a wall of
// text. A nil wipe (defensive: the dialog should never open without one)
// falls back to a generic phrasing.
func wipeQuestion(name string, wipe *threewayrsync.WouldWipeError) string {
	side, other := "remote", "local"
	count := ""
	if wipe != nil {
		if wipe.Side == "local" {
			side, other = "local", "remote"
		}
		count = fmt.Sprintf("all %d", wipe.Deletes)
	} else {
		count = "every"
	}
	return fmt.Sprintf(
		"Syncing \"%s\" would delete %s previously synced file(s)\non the %s side — the %s copy lists none of them.\n\n"+
			"Nothing was written; a full wipe is never applied.\nIf you emptied the %s copy to start over, use Check-in\nwith abandon, then check out again.",
		name, count, side, other, other)
}

// updateConfirm handles the shared confirm modal (delete, checkout, check-in,
// or sync, per m.confirmKind). Tab/Shift+Tab cycle focus around the dialog's
// ring (default: Cancel); ←→ toggle between the two buttons. enter/space
// toggles the focused checkbox or activates the focused button.
// y/Y always activates and n/N/esc always cancel, regardless of focus — the
// original direct shortcuts stay live alongside the controls.
func (m model) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	// The wipe dialog is informational: any activate/dismiss key closes it and
	// nothing runs. Focus-movement keys fall through harmlessly (one-entry ring).
	if m.confirmKind == confirmWipe {
		switch key.String() {
		case "enter", " ", "esc", "y", "Y", "n", "N":
			m.mode = modeMain
			m.wipe = nil
		}
		return m, nil
	}
	switch key.String() {
	case "y", "Y":
		return m.activateConfirm()
	case "n", "N", "esc":
		m.mode = modeMain
		m.wipe = nil
		return m, nil
	case "tab":
		m.confirmFocus = confirmFocusStep(m.confirmKind, m.confirmForeign, m.confirmFocus, 1)
		return m, nil
	case "shift+tab":
		m.confirmFocus = confirmFocusStep(m.confirmKind, m.confirmForeign, m.confirmFocus, -1)
		return m, nil
	case "left", "right":
		// Toggle between the two buttons; ignored while on a checkbox.
		switch m.confirmFocus {
		case confirmFocusCancel:
			m.confirmFocus = confirmFocusDelete
		case confirmFocusDelete:
			m.confirmFocus = confirmFocusCancel
		}
		return m, nil
	case "up", "down":
		// Step through the dialog's rows, in the dialogs that have checkbox rows
		// above the buttons: down walks the ring forward, up walks it back.
		if len(confirmFocusRing(m.confirmKind, m.confirmForeign)) <= 2 {
			return m, nil
		}
		dir := 1
		if key.String() == "up" {
			dir = -1
		}
		m.confirmFocus = confirmFocusStep(m.confirmKind, m.confirmForeign, m.confirmFocus, dir)
		return m, nil
	case "enter", " ":
		if toggled, ok := m.toggleConfirmCheckbox(); ok {
			return toggled, nil
		}
		if m.confirmFocus == confirmFocusDelete {
			return m.activateConfirm()
		}
		m.mode = modeMain
		m.wipe = nil
		return m, nil
	}
	return m, nil
}

// toggleConfirmCheckbox flips the checkbox the confirm dialog's focus sits on,
// reporting whether focus was on a checkbox at all (false means a button holds
// the focus and enter/space should activate/dismiss instead).
func (m model) toggleConfirmCheckbox() (model, bool) {
	switch m.confirmFocus {
	case confirmFocusAbandon:
		m.checkinAbandon = !m.checkinAbandon
	case confirmFocusClean:
		m.checkinClean = !m.checkinClean
	case confirmFocusSteal:
		m.checkoutSteal = !m.checkoutSteal
	case confirmFocusAllowDeletes:
		m.syncAllowDeletes = !m.syncAllowDeletes
	case confirmFocusLocalWins:
		m.syncLocalWins = !m.syncLocalWins
	default:
		return m, false
	}
	return m, true
}

// activateConfirm runs whichever mutating action the open confirm modal
// guards, branching on m.confirmKind. (confirmWipe never reaches here: its
// keys are handled up front in updateConfirm — there is nothing to activate.)
func (m model) activateConfirm() (tea.Model, tea.Cmd) {
	switch m.confirmKind {
	case confirmCheckin:
		return m.checkinConfirmed()
	case confirmCheckout:
		return m.checkoutConfirmed()
	case confirmSync:
		return m.syncConfirmed()
	case confirmCancel:
		return m.cancelAction()
	}
	return m.deleteConfirmedProfile()
}

// cancelAction stops the in-flight action the confirm modal was guarding. For a
// streaming mutation it cancels the context — killing the live rsync via
// exec.CommandContext; for Status there is nothing to kill (m.cancel is nil), so
// it only abandons the result. Either way it bumps actionSeq so the run's
// straggler messages are dropped, marks the profile canceled for the "Canceled."
// note, and returns to the profile's actions view.
func (m model) cancelAction() (tea.Model, tea.Cmd) {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.actionSeq++
	m.profile.acting = false
	m.profile.checking = false
	m.profile.scanning = false
	m.profile.canceled = true
	m.mode = modeMain
	m.sub = subActions
	return m, nil
}

// deleteConfirmedProfile removes m.confirmName from the config and persists it,
// rolling back in memory if the save fails. Shared by the y/Y shortcut and the
// focused-Delete-button path.
func (m model) deleteConfirmedProfile() (tea.Model, tea.Cmd) {
	prev := cloneProfiles(m.cfg.Profiles)
	delete(m.cfg.Profiles, m.confirmName)
	if err := commitProfiles(m.path, m.cfg, prev); err != nil {
		m.err = err
		m.mode = modeMain
		return m, nil
	}
	m.refreshList()
	delete(m.checks, m.confirmName)
	m.mode = modeMain
	m.err = nil
	return m, nil
}

// checkinConfirmed runs lifecycle.Runner.Checkin for m.confirmName once the
// user has confirmed, returning to the profile's actions view with acting
// state reset so the Activity box shows a fresh in-progress state.
func (m model) checkinConfirmed() (tea.Model, tea.Cmd) {
	name := m.confirmName
	m.mode = modeMain
	m.sub = subActions
	m.profile.acting = true
	m.profile.actionErr = nil
	m.profile.actionReport = nil
	m.profile.applied = nil
	m.profile.canceled = false
	m.profile.statusScroll = 0
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.actionSeq++
	// checkin has no --force: it only releases a this-machine-owned, fully-synced
	// profile. The footer force toggle applies to checkout/sync, not here.
	// Abandon skips the in-sync verification (the "start over" path).
	opts := lifecycle.Options{Clean: m.checkinClean, Abandon: m.checkinAbandon}
	return m, checkinCmd(ctx, m.runner, m.id, name, m.cfg.Profiles[name], m.actionSeq, opts)
}

// checkoutConfirmed runs lifecycle.Runner.Checkout for m.confirmName once the
// user has confirmed, returning to the profile's actions view with acting state
// reset so the Activity box shows a fresh in-progress state. Mirrors
// checkinConfirmed. Force is the dialog's "steal the lock" checkbox — only
// offered when the dialog opened on a foreign lock; left unchecked, a foreign
// lock still refuses in the runner and the refusal shows in Activity.
func (m model) checkoutConfirmed() (tea.Model, tea.Cmd) {
	name := m.confirmName
	m.mode = modeMain
	m.sub = subActions
	m.profile.acting = true
	m.profile.actionErr = nil
	m.profile.actionReport = nil
	m.profile.applied = nil
	m.profile.canceled = false
	m.profile.statusScroll = 0
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.actionSeq++
	opts := lifecycle.Options{Force: m.checkoutSteal}
	return m, checkoutCmd(ctx, m.runner, m.id, name, m.cfg.Profiles[name], m.actionSeq, opts)
}

// syncConfirmed runs lifecycle.Runner.Sync for m.confirmName once the user hit
// Run in the sync dialog, carrying its two checkboxes into the run's options:
// "allow deletes" (AllowDeletes) and "local wins conflicts" (Force — conflicts
// resolve keeping the local file instead of stopping the sync). Mirrors
// checkoutConfirmed.
func (m model) syncConfirmed() (tea.Model, tea.Cmd) {
	name := m.confirmName
	m.mode = modeMain
	m.sub = subActions
	m.profile.acting = true
	m.profile.actionErr = nil
	m.profile.actionReport = nil
	m.profile.applied = nil
	m.profile.canceled = false
	m.profile.statusScroll = 0
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.actionSeq++
	opts := lifecycle.Options{Force: m.syncLocalWins, AllowDeletes: m.syncAllowDeletes}
	return m, syncCmd(ctx, m.runner, m.id, name, m.cfg.Profiles[name], m.actionSeq, opts)
}
