package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/internal/ident"
	"github.com/andresbott/dibs/internal/lifecycle"
	"github.com/andresbott/dibs/internal/marker"
	"github.com/andresbott/dibs/internal/sanity"
	"github.com/andresbott/dibs/libs/threewayrsync"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestDeleteConfirmed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"alpha": {LocalRoot: "/l/a", RemoteRoot: "/r/a"},
		"beta":  {LocalRoot: "/l/b", RemoteRoot: "/r/b"},
	}}
	if err := config.Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	m := newModel(p, cfg)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")}) // "alpha" is first
	if m.mode != modeConfirm {
		t.Fatalf("want modeConfirm, got %d", m.mode)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if m.mode != modeMain {
		t.Fatalf("want modeMain after delete, got %d", m.mode)
	}
	if _, exists := m.cfg.Profiles["alpha"]; exists {
		t.Error("alpha should be deleted")
	}
	saved, _ := config.Load(p)
	if _, exists := saved.Profiles["alpha"]; exists {
		t.Error("delete should be persisted")
	}
}

func TestDeleteCancelled(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"alpha": {LocalRoot: "/l/a", RemoteRoot: "/r/a"},
	}}
	m := newModel(p, cfg)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.mode != modeMain {
		t.Fatalf("want modeMain after cancel, got %d", m.mode)
	}
	if _, exists := m.cfg.Profiles["alpha"]; !exists {
		t.Error("alpha should still exist after cancel")
	}
}

// TestDeleteEnterOnDefaultFocusCancelsNotDeletes: the dialog opens with Cancel
// focused (the safe default), so a bare enter right after opening activates
// Cancel — it must never delete on the very first keystroke.
func TestDeleteEnterOnDefaultFocusCancelsNotDeletes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"alpha": {LocalRoot: "/l/a", RemoteRoot: "/r/a"},
	}}
	m := newModel(p, cfg)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if m.confirmFocus != confirmFocusCancel {
		t.Fatalf("dialog should open with Cancel focused, got %d", m.confirmFocus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMain {
		t.Fatalf("enter on the default-focused Cancel should return to main, mode = %d", m.mode)
	}
	if _, exists := m.cfg.Profiles["alpha"]; !exists {
		t.Error("alpha should still exist; a bare enter must not delete")
	}
}

// TestDeleteFocusedButtonDeletes: moving focus to Delete (via Tab) and
// activating it removes the profile and persists it — the new button-driven
// path, held to the same persistence rigor as TestDeleteConfirmed's y/Y path.
func TestDeleteFocusedButtonDeletes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"alpha": {LocalRoot: "/l/a", RemoteRoot: "/r/a"},
	}}
	if err := config.Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	m := newModel(p, cfg)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.confirmFocus != confirmFocusDelete {
		t.Fatalf("tab should move focus to Delete, got %d", m.confirmFocus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMain {
		t.Fatalf("enter on focused Delete should return to main, mode = %d", m.mode)
	}
	if _, exists := m.cfg.Profiles["alpha"]; exists {
		t.Error("alpha should be deleted")
	}
	saved, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := saved.Profiles["alpha"]; exists {
		t.Error("delete via the button path should be persisted")
	}
}

// TestDeleteFocusLeftRight: left/right toggle focus between Delete and Cancel,
// mirroring the add/edit form's Save/Cancel toggle.
func TestDeleteFocusLeftRight(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"alpha": {LocalRoot: "/l/a", RemoteRoot: "/r/a"},
	}}
	m := newModel(p, cfg)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if m.confirmFocus != confirmFocusDelete {
		t.Fatalf("right from Cancel should focus Delete, got %d", m.confirmFocus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if m.confirmFocus != confirmFocusCancel {
		t.Fatalf("left from Delete should focus Cancel, got %d", m.confirmFocus)
	}
}

// TestDeleteFocusTabCycles: tab and shift+tab also toggle focus between the two
// buttons.
func TestDeleteFocusTabCycles(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"alpha": {LocalRoot: "/l/a", RemoteRoot: "/r/a"},
	}}
	m := newModel(p, cfg)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.confirmFocus != confirmFocusDelete {
		t.Fatalf("tab from Cancel should focus Delete, got %d", m.confirmFocus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.confirmFocus != confirmFocusCancel {
		t.Fatalf("shift+tab from Delete should focus Cancel, got %d", m.confirmFocus)
	}
}

// TestConfirmModalHasDeleteCancelButtons guards the new button UI: the modal
// must render both bracketed buttons and the shared hint line.
func TestConfirmModalHasDeleteCancelButtons(t *testing.T) {
	view := confirmModal(confirmDelete, "alpha", confirmParams{focus: confirmFocusCancel}, 80)
	for _, want := range []string{"[ Delete ]", "[ Cancel ]", "Move", "Activate", "Cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("confirm modal missing %q:\n%s", want, view)
		}
	}
}

// TestDeleteSaveFailureKeepsProfile covers spec §9: a save failure on delete
// must not silently diverge in-memory state from disk. The model's path
// points through a regular file so config.Save's MkdirAll fails
// deterministically.
func TestDeleteSaveFailureKeepsProfile(t *testing.T) {
	p := failingSavePath(t)
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"alpha": {LocalRoot: "/l/a", RemoteRoot: "/r/a"},
	}}
	m := newModel(p, cfg)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})

	if m.mode != modeMain {
		t.Fatalf("want modeMain after failed delete save, got %d", m.mode)
	}
	if m.err == nil {
		t.Fatal("expected m.err to be set after a failed save")
	}
	if _, exists := m.cfg.Profiles["alpha"]; !exists {
		t.Error("alpha should still exist in memory when the delete save fails")
	}
}

func TestConfirmModalFitsWidthWithLongName(t *testing.T) {
	longName := strings.Repeat("x", 60)
	cfg := &config.Config{Profiles: map[string]config.Profile{
		longName: {LocalRoot: "/l", RemoteRoot: "/r"},
	}}
	m := newModel("/tmp/x.yaml", cfg)
	m = update(t, m, tea.WindowSizeMsg{Width: 40, Height: 20})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if m.mode != modeConfirm {
		t.Fatalf("want modeConfirm, got %d", m.mode)
	}
	if got := lipgloss.Width(m.View()); got > 40 {
		t.Errorf("confirm view width %d > 40; long name should be capped", got)
	}
}

// TestCheckinModalShowsCleanCheckbox: the check-in dialog renders the "delete
// local copy" checkbox, empty when clean is false and filled when true.
func TestCheckinModalShowsCleanCheckbox(t *testing.T) {
	unchecked := confirmModal(confirmCheckin, "work", confirmParams{focus: confirmFocusCancel}, 80)
	if !strings.Contains(unchecked, "delete local copy") || !strings.Contains(unchecked, "[ ]") {
		t.Errorf("check-in modal should show an unchecked 'delete local copy' box:\n%s", unchecked)
	}
	checked := confirmModal(confirmCheckin, "work", confirmParams{focus: confirmFocusClean, clean: true}, 80)
	if !strings.Contains(checked, "[x]") {
		t.Errorf("clean=true should render a checked box:\n%s", checked)
	}
}

// TestCheckinModalShowsAbandonCheckbox: the check-in dialog renders the
// "abandon" checkbox, empty when abandon is false and filled when true.
func TestCheckinModalShowsAbandonCheckbox(t *testing.T) {
	unchecked := confirmModal(confirmCheckin, "work", confirmParams{focus: confirmFocusCancel}, 80)
	if !strings.Contains(unchecked, "abandon") || !strings.Contains(unchecked, "[ ]") {
		t.Errorf("check-in modal should show an unchecked 'abandon' box:\n%s", unchecked)
	}
	checked := confirmModal(confirmCheckin, "work", confirmParams{focus: confirmFocusAbandon, abandon: true}, 80)
	if !strings.Contains(checked, "[x]") {
		t.Errorf("abandon=true should render a checked box:\n%s", checked)
	}
}

// TestDeleteModalHasNoCleanCheckbox: the checkboxes are check-in only; the
// delete dialog must never show them.
func TestDeleteModalHasNoCleanCheckbox(t *testing.T) {
	view := confirmModal(confirmDelete, "alpha", confirmParams{focus: confirmFocusCancel}, 80)
	if strings.Contains(view, "delete local copy") {
		t.Errorf("delete modal must not show the clean checkbox:\n%s", view)
	}
	if strings.Contains(view, "abandon") {
		t.Errorf("delete modal must not show the abandon checkbox:\n%s", view)
	}
}

// TestCheckinFocusRingIncludesCheckbox: Tab cycles abandon → clean → Check in →
// Cancel → abandon in the check-in dialog (the checkboxes are absent from delete).
func TestCheckinFocusRingIncludesCheckbox(t *testing.T) {
	m := model{mode: modeConfirm, confirmKind: confirmCheckin, confirmFocus: confirmFocusCancel}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.confirmFocus != confirmFocusAbandon {
		t.Fatalf("tab from Cancel should reach the abandon checkbox, got %d", m.confirmFocus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.confirmFocus != confirmFocusClean {
		t.Fatalf("tab from abandon should reach the clean checkbox, got %d", m.confirmFocus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.confirmFocus != confirmFocusDelete {
		t.Fatalf("tab from the clean checkbox should reach Check in, got %d", m.confirmFocus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.confirmFocus != confirmFocusCancel {
		t.Fatalf("tab from Check in should reach Cancel, got %d", m.confirmFocus)
	}
}

// TestCheckinCheckboxTogglesClean: space/enter on the focused checkbox flips the
// checkinClean state without activating the check-in.
func TestCheckinCheckboxTogglesClean(t *testing.T) {
	m := model{mode: modeConfirm, confirmKind: confirmCheckin, confirmFocus: confirmFocusClean}
	m = update(t, m, tea.KeyMsg{Type: tea.KeySpace})
	if !m.checkinClean {
		t.Error("space on the focused checkbox should toggle checkinClean on")
	}
	if m.mode != modeConfirm {
		t.Error("toggling the checkbox must not close the dialog")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.checkinClean {
		t.Error("enter on the focused checkbox should toggle it back off")
	}
}

// TestCheckinCheckboxTogglesAbandon: space/enter on the focused abandon checkbox
// flips the checkinAbandon state without activating the check-in.
func TestCheckinCheckboxTogglesAbandon(t *testing.T) {
	m := model{mode: modeConfirm, confirmKind: confirmCheckin, confirmFocus: confirmFocusAbandon}
	m = update(t, m, tea.KeyMsg{Type: tea.KeySpace})
	if !m.checkinAbandon {
		t.Error("space on the focused checkbox should toggle checkinAbandon on")
	}
	if m.mode != modeConfirm {
		t.Error("toggling the checkbox must not close the dialog")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.checkinAbandon {
		t.Error("enter on the focused checkbox should toggle it back off")
	}
}

// TestCheckinAbandonPassedToRunner: confirming a check-in with the abandon
// checkbox ticked releases the lock despite unsynced local changes — proving
// the checkbox state reaches lifecycle.Options.Abandon (a plain check-in would
// refuse here).
func TestCheckinAbandonPassedToRunner(t *testing.T) {
	t.Setenv("DIBS_STATE", t.TempDir())
	name, p, id := tuiHeldFixture(t)
	// An unsynced local edit that a plain check-in would refuse over.
	if err := os.WriteFile(filepath.Join(p.LocalRoot, "keep.txt"), []byte("EDITED-LONGER"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := model{
		mode:           modeConfirm,
		confirmKind:    confirmCheckin,
		confirmName:    name,
		checkinAbandon: true,
		cfg:            &config.Config{Profiles: map[string]config.Profile{name: p}},
		id:             id,
		runner:         lifecycle.Runner{ToolVersion: "test"},
	}
	m.profile = newProfileView(name)
	m2, cmd := m.activateConfirm()
	if !m2.(model).profile.acting {
		t.Fatal("confirming check-in should mark the profile as acting")
	}
	_, res := drainStream(t, cmd())
	if res.err != nil {
		t.Fatalf("abandon check-in should release despite unsynced changes, got %v", res.err)
	}
	if !res.report.Released || !res.report.Abandoned {
		t.Errorf("report should mark the profile released+abandoned, got %+v", res.report)
	}
}

// TestCheckinUpDownMovesBetweenRows: up/down step focus through the check-in
// dialog's rows (abandon → clean → buttons and back).
func TestCheckinUpDownMovesBetweenRows(t *testing.T) {
	m := model{mode: modeConfirm, confirmKind: confirmCheckin, confirmFocus: confirmFocusAbandon}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.confirmFocus != confirmFocusClean {
		t.Fatalf("down from abandon should move to the clean checkbox, got %d", m.confirmFocus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.confirmFocus != confirmFocusDelete {
		t.Fatalf("down from the clean checkbox should move to the buttons, got %d", m.confirmFocus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.confirmFocus != confirmFocusClean {
		t.Fatalf("up from a button should move to the clean checkbox, got %d", m.confirmFocus)
	}
	// From Cancel, up walks back to the Check in button.
	m.confirmFocus = confirmFocusCancel
	m = update(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.confirmFocus != confirmFocusDelete {
		t.Fatalf("up from Cancel should move to the Check in button, got %d", m.confirmFocus)
	}
}

// TestDeleteUpDownNoop: the delete dialog has no checkbox, so up/down do nothing.
func TestDeleteUpDownNoop(t *testing.T) {
	m := model{mode: modeConfirm, confirmKind: confirmDelete, confirmFocus: confirmFocusCancel}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.confirmFocus != confirmFocusCancel {
		t.Errorf("up/down should be a no-op in the delete dialog, got %d", m.confirmFocus)
	}
}

// TestCheckoutOpensConfirmModal: selecting Checkout no longer runs immediately —
// it opens the confirm modal, focused on Cancel (the safe default), without
// starting the action.
func TestCheckoutOpensConfirmModal(t *testing.T) {
	m := model{
		sub:    subActions,
		cfg:    &config.Config{Profiles: map[string]config.Profile{"work": {}}},
		checks: map[string]*sanity.Result{"work": {CheckedOut: false}},
	}
	m.profile = newProfileView("work")
	m.profile.cursor = actionIndex(visibleActions(m.checks["work"], m.id, ""), "Checkout")
	m2, _ := m.updateProfile(keyMsg("enter"))
	got := m2.(model)
	if got.mode != modeConfirm {
		t.Fatal("Checkout Enter should open the confirm modal")
	}
	if got.confirmKind != confirmCheckout {
		t.Errorf("confirmKind = %v, want confirmCheckout", got.confirmKind)
	}
	if got.confirmFocus != confirmFocusCancel {
		t.Errorf("checkout dialog should open focused on Cancel, got %d", got.confirmFocus)
	}
	if got.profile.acting {
		t.Error("opening the confirm dialog must not start the checkout")
	}
	if got.confirmForeign {
		t.Error("a not-checked-out profile must not read as a foreign lock")
	}
}

// TestCheckoutConfirmedStartsAction: activating the checkout dialog (y) closes it
// and starts the action (acting set), returning to the actions view.
func TestCheckoutConfirmedStartsAction(t *testing.T) {
	m := model{
		mode:        modeConfirm,
		confirmKind: confirmCheckout,
		confirmName: "work",
		cfg:         &config.Config{Profiles: map[string]config.Profile{"work": {}}},
	}
	m.profile = newProfileView("work")
	m2, cmd := m.activateConfirm()
	got := m2.(model)
	if got.mode != modeMain || got.sub != subActions {
		t.Fatalf("after confirming checkout want modeMain/subActions, got mode=%d sub=%d", got.mode, got.sub)
	}
	if !got.profile.acting {
		t.Error("confirming checkout should mark the profile as acting")
	}
	if cmd == nil {
		t.Error("confirming checkout should dispatch the checkout command")
	}
}

// TestCheckoutModalWording: the checkout dialog carries its own title/question/
// activate label and, like delete, shows no clean checkbox. Without a foreign
// lock there is no steal checkbox either.
func TestCheckoutModalWording(t *testing.T) {
	view := confirmModal(confirmCheckout, "work", confirmParams{focus: confirmFocusCancel}, 80)
	for _, want := range []string{"Confirm checkout", "Check out \"work\"?", "[ Check out ]", "[ Cancel ]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("checkout modal missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "delete local copy") {
		t.Errorf("checkout modal must not show the clean checkbox:\n%s", view)
	}
	if strings.Contains(view, "steal the lock") {
		t.Errorf("without a foreign lock the checkout modal must not show the steal checkbox:\n%s", view)
	}
}

// TestCheckoutModalForeignLock: on a foreign lock the checkout dialog names the
// holder and offers the "steal the lock" checkbox; a nil marker (present but
// unreadable) falls back to a generic phrasing, still with the checkbox.
func TestCheckoutModalForeignLock(t *testing.T) {
	holder := foreignLock().Marker
	holder.CheckedOutAt = time.Date(2026, 7, 1, 10, 30, 0, 0, time.UTC)
	view := confirmModal(confirmCheckout, "work", confirmParams{focus: confirmFocusSteal, foreign: true, holder: holder}, 80)
	for _, want := range []string{"other@elsewhere", "elsewhere", "2026-07-01 10:30", "steal the lock", "[ ]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("foreign checkout modal missing %q:\n%s", want, view)
		}
	}
	checked := confirmModal(confirmCheckout, "work", confirmParams{focus: confirmFocusSteal, foreign: true, holder: holder, steal: true}, 80)
	if !strings.Contains(checked, "[x]") {
		t.Errorf("steal=true should render a checked box:\n%s", checked)
	}
	unreadable := confirmModal(confirmCheckout, "work", confirmParams{focus: confirmFocusSteal, foreign: true}, 80)
	for _, want := range []string{"someone else", "steal the lock"} {
		if !strings.Contains(unreadable, want) {
			t.Fatalf("unreadable-marker checkout modal missing %q:\n%s", want, unreadable)
		}
	}
}

// TestCheckoutForeignOpensWithStealUnchecked: opening the checkout dialog on a
// foreign-locked profile flags it foreign and resets the steal checkbox, so a
// stale tick from a previous open can't silently steal a lock.
func TestCheckoutForeignOpensWithStealUnchecked(t *testing.T) {
	m := model{
		sub:           subActions,
		cfg:           &config.Config{Profiles: map[string]config.Profile{"work": {}}},
		checks:        map[string]*sanity.Result{"work": foreignLock()},
		checkoutSteal: true, // stale from a previous open
	}
	m.profile = newProfileView("work")
	m.profile.cursor = actionIndex(visibleActions(m.checks["work"], m.id, ""), "Checkout")
	m2, _ := m.updateProfile(keyMsg("enter"))
	got := m2.(model)
	if got.mode != modeConfirm || got.confirmKind != confirmCheckout {
		t.Fatalf("Checkout Enter should open the checkout dialog, got mode %d kind %d", got.mode, got.confirmKind)
	}
	if !got.confirmForeign {
		t.Error("a foreign lock should open the dialog in foreign mode")
	}
	if got.checkoutSteal {
		t.Error("opening the checkout dialog should reset the steal checkbox to unchecked")
	}
}

// TestCheckoutStealTogglesAndPassesForce: space toggles the steal checkbox, and
// confirming with it ticked carries Force into the checkout options (asserted
// through the runner: a foreign marker is overwritten only under Force).
func TestCheckoutStealTogglesAndPassesForce(t *testing.T) {
	requireRsync(t)
	t.Setenv("DIBS_STATE", t.TempDir())
	root := t.TempDir()
	local := filepath.Join(root, "local")
	remote := filepath.Join(root, "remote")
	_ = os.MkdirAll(remote, 0o755)
	foreign := &marker.Marker{CheckedOutBy: "other@elsewhere", Host: "elsewhere", Profile: "work", Relpaths: []string{"."}}
	if err := marker.Write(remote, foreign); err != nil {
		t.Fatal(err)
	}

	id := ident.Ident{By: "me@host", Host: "host"}
	m := model{
		mode:           modeConfirm,
		confirmKind:    confirmCheckout,
		confirmName:    "work",
		confirmForeign: true,
		confirmFocus:   confirmFocusSteal,
		cfg:            &config.Config{Profiles: map[string]config.Profile{"work": {LocalRoot: local, RemoteRoot: remote}}},
		id:             id,
		runner:         lifecycle.Runner{ToolVersion: "test"},
	}
	m.profile = newProfileView("work")

	m = update(t, m, tea.KeyMsg{Type: tea.KeySpace})
	if !m.checkoutSteal {
		t.Fatal("space on the focused steal checkbox should tick it")
	}
	m2, cmd := m.activateConfirm()
	if !m2.(model).profile.acting {
		t.Fatal("confirming checkout should mark the profile as acting")
	}
	_, res := drainStream(t, cmd())
	if res.err != nil {
		t.Fatalf("steal checkout should override the foreign lock, got %v", res.err)
	}
	if got, ok, _ := marker.Read(remote); !ok || !got.OwnedBy(id.By, id.Host, "") {
		t.Errorf("the marker should now be ours, got %+v", got)
	}
}

// TestSyncModalWording: the sync dialog renders its two option checkboxes
// ("allow deletes" and "local wins conflicts") above Run/Cancel.
func TestSyncModalWording(t *testing.T) {
	view := confirmModal(confirmSync, "work", confirmParams{focus: confirmFocusAllowDeletes}, 80)
	for _, want := range []string{"Confirm sync", "Sync \"work\"?", "allow deletes", "local wins conflicts", "[ Run ]", "[ Cancel ]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("sync modal missing %q:\n%s", want, view)
		}
	}
	checked := confirmModal(confirmSync, "work", confirmParams{focus: confirmFocusAllowDeletes, allowDeletes: true, localWins: true}, 80)
	if !strings.Contains(checked, "[x]") {
		t.Errorf("ticked sync options should render checked boxes:\n%s", checked)
	}
}

// TestSyncDialogFocusRing: Tab cycles allow-deletes → local-wins → Run → Cancel
// and back; space toggles the focused checkbox without starting the run.
func TestSyncDialogFocusRing(t *testing.T) {
	m := model{mode: modeConfirm, confirmKind: confirmSync, confirmFocus: confirmFocusAllowDeletes}
	m = update(t, m, tea.KeyMsg{Type: tea.KeySpace})
	if !m.syncAllowDeletes {
		t.Fatal("space should tick the allow-deletes checkbox")
	}
	if m.mode != modeConfirm {
		t.Fatal("toggling a checkbox must not close the dialog")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.confirmFocus != confirmFocusLocalWins {
		t.Fatalf("tab from allow-deletes should reach local-wins, got %d", m.confirmFocus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeySpace})
	if !m.syncLocalWins {
		t.Fatal("space should tick the local-wins checkbox")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.confirmFocus != confirmFocusDelete {
		t.Fatalf("tab from local-wins should reach Run, got %d", m.confirmFocus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.confirmFocus != confirmFocusCancel {
		t.Fatalf("tab from Run should reach Cancel, got %d", m.confirmFocus)
	}
}

// TestWipeStopOpensDialog: a sync result carrying the engine's wipe-valve stop
// must open the confirmWipe dialog (safe focus on Cancel) instead of leaving
// only a flat error string in the Activity panel.
func TestWipeStopOpensDialog(t *testing.T) {
	m := model{sub: subActions, cfg: &config.Config{Profiles: map[string]config.Profile{"work": {}}}}
	m.profile = newProfileView("work")
	m.profile.acting = true
	wipeErr := userFacingWipe(t)
	m = update(t, m, actionResultMsg{name: "work", report: lifecycle.Report{Action: "sync"}, err: wipeErr})
	if m.mode != modeConfirm || m.confirmKind != confirmWipe {
		t.Fatalf("a wipe stop should open the wipe dialog; mode=%d kind=%d", m.mode, m.confirmKind)
	}
	if m.confirmFocus != confirmFocusCancel {
		t.Errorf("wipe dialog must open focused on Cancel, got %d", m.confirmFocus)
	}
	if m.wipe == nil || m.wipe.Side != "remote" || m.wipe.Deletes != 7 {
		t.Errorf("dialog should carry the wipe details, got %+v", m.wipe)
	}
	// The dialog carries the explanation; the Activity panel keeps only a short
	// note, never the CLI-oriented message with its flag names.
	if got := m.profile.actionErr; got == nil || strings.Contains(got.Error(), "--allow-deletes") {
		t.Errorf("Activity should hold a short note without CLI flags, got %v", got)
	}
	if m.profile.actionReport != nil {
		t.Error("a wipe stop must not leave a report that renders as a success summary")
	}
}

// userFacingWipe builds the error exactly as the lifecycle layer delivers it:
// the engine's WouldWipeError wrapped in the user-facing rewrite, so the test
// exercises the errors.As unwrap in applyActionResult.
func userFacingWipe(t *testing.T) error {
	t.Helper()
	return fmt.Errorf("this sync would delete every previously synced file: %w",
		&threewayrsync.WouldWipeError{Side: "remote", Deletes: 7})
}

// TestWipeDialogWording: the dialog explains the stop and names the only
// deliberate recovery path (abandon + re-checkout) — allow-deletes no longer
// waives the wipe valve and must not be suggested. Informational only: a
// single OK, no destructive button.
func TestWipeDialogWording(t *testing.T) {
	view := confirmModal(confirmWipe, "work",
		confirmParams{focus: confirmFocusCancel, wipe: &threewayrsync.WouldWipeError{Side: "remote", Deletes: 7}}, 80)
	for _, want := range []string{"Sync stopped", "all 7", "remote side", "start", "[ OK ]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("wipe dialog missing %q:\n%s", want, view)
		}
	}
	for _, forbidden := range []string{"allow-deletes", "Delete anyway", "[ Cancel ]", "delete local copy"} {
		if strings.Contains(view, forbidden) {
			t.Errorf("wipe dialog must not show %q:\n%s", forbidden, view)
		}
	}
}

// TestWipeDialogClosesWithoutRunning: the dialog is informational — every
// close key (enter activates OK, esc dismisses) returns to the profile view
// with nothing re-run; the short stop note stays visible in Activity and the
// stored wipe detail is cleared.
func TestWipeDialogClosesWithoutRunning(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyEnter}} {
		m := model{sub: subActions, cfg: &config.Config{Profiles: map[string]config.Profile{"work": {}}}}
		m.profile = newProfileView("work")
		m.profile.acting = true
		m = update(t, m, actionResultMsg{name: "work", report: lifecycle.Report{Action: "sync"}, err: userFacingWipe(t)})
		m = update(t, m, key)
		if m.mode != modeMain || m.sub != subActions {
			t.Fatalf("%v should return to the profile view; mode=%d sub=%d", key, m.mode, m.sub)
		}
		if m.wipe != nil {
			t.Error("closing the dialog must clear the stored wipe detail")
		}
		if m.profile.actionErr == nil {
			t.Error("the stop note must stay visible in the Activity panel")
		}
		if m.profile.acting {
			t.Errorf("%v must not re-run anything", key)
		}
	}
}
