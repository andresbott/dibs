package tui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/internal/ident"
	"github.com/andresbott/dibs/internal/lifecycle"
	"github.com/andresbott/dibs/internal/localstat"
	"github.com/andresbott/dibs/internal/marker"
	"github.com/andresbott/dibs/internal/sanity"
	tea "github.com/charmbracelet/bubbletea"
)

// requireRsync skips engine-driving tests when rsync is not on PATH.
func requireRsync(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not on PATH")
	}
}

func TestCheckoutCmdProducesMarker(t *testing.T) {
	requireRsync(t)
	t.Setenv("DIBS_STATE", t.TempDir())
	root := t.TempDir()
	local := filepath.Join(root, "local")
	remote := filepath.Join(root, "remote")
	_ = os.MkdirAll(remote, 0o755)
	_ = os.WriteFile(filepath.Join(remote, "f"), []byte("x"), 0o644)

	runner := lifecycle.Runner{ToolVersion: "test"}
	p := config.Profile{LocalRoot: local, RemoteRoot: remote}
	_, res := drainStream(t, checkoutCmd(context.Background(), runner, ident.Ident{By: "me@host", Host: "host"}, "work", p, 0, lifecycle.Options{})())
	if res.err != nil {
		t.Fatalf("checkout cmd err: %v", res.err)
	}
	if _, ok, _ := marker.Read(remote); !ok {
		t.Error("checkout cmd did not write a marker")
	}
}

// TestSyncOpensConfirmModal: Enter on Sync no longer runs it directly — it
// opens the sync dialog, focused on the first checkbox with both options reset
// to unchecked (a stale value from a previous open must not carry over).
func TestSyncOpensConfirmModal(t *testing.T) {
	m := model{
		sub:              subActions,
		cfg:              &config.Config{Profiles: map[string]config.Profile{"work": {}}},
		syncAllowDeletes: true, // stale from a previous open
		syncLocalWins:    true, // stale from a previous open
	}
	m.checks = map[string]*sanity.Result{"work": ownLock(m)}
	m.profile = newProfileView("work")
	m.profile.cursor = actionIndex(visibleActions(m.checks["work"], m.id, ""), "Sync")
	m2, _ := m.updateProfile(keyMsg("enter"))
	got := m2.(model)
	if got.mode != modeConfirm || got.confirmKind != confirmSync {
		t.Fatalf("Sync Enter should open the sync confirm modal, got mode %d kind %d", got.mode, got.confirmKind)
	}
	if got.profile.acting {
		t.Error("Sync must not start before the dialog's Run")
	}
	if got.confirmFocus != confirmFocusAllowDeletes {
		t.Errorf("sync dialog should open focused on the allow-deletes checkbox, got %d", got.confirmFocus)
	}
	if got.syncAllowDeletes || got.syncLocalWins {
		t.Error("opening the sync dialog should reset both checkboxes to unchecked")
	}
}

// TestSyncDialogRunPassesOptions: the dialog's checkboxes reach the runner —
// with both ticked, a run over a local delete plus a same-file conflict
// carries the delete out (AllowDeletes) and resolves the conflict local-wins
// instead of stopping (Force). Unticked, that same run would stop on the
// conflict and only report the delete pending.
func TestSyncDialogRunPassesOptions(t *testing.T) {
	t.Setenv("DIBS_STATE", t.TempDir())
	name, p, id := tuiHeldFixture(t)
	// A second synced file to delete locally; keep.txt becomes the conflict.
	if err := os.WriteFile(filepath.Join(p.RemoteRoot, "gone.txt"), []byte("bye"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := lifecycle.Runner{ToolVersion: "test"}
	if _, err := r.Sync(context.Background(), name, p, id, "", lifecycle.Options{}); err != nil {
		t.Fatalf("fixture second sync: %v", err)
	}
	if err := os.Remove(filepath.Join(p.LocalRoot, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.LocalRoot, "keep.txt"), []byte("LOCAL-version"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.RemoteRoot, "keep.txt"), []byte("RR"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := model{
		mode:             modeConfirm,
		confirmKind:      confirmSync,
		confirmName:      name,
		syncAllowDeletes: true,
		syncLocalWins:    true,
		cfg:              &config.Config{Profiles: map[string]config.Profile{name: p}},
		id:               id,
		runner:           r,
	}
	m.profile = newProfileView(name)
	m2, cmd := m.activateConfirm()
	if !m2.(model).profile.acting {
		t.Fatal("Run should mark the profile as acting")
	}
	_, res := drainStream(t, cmd())
	if res.err != nil {
		t.Fatalf("local-wins sync should not stop on the conflict, got %v", res.err)
	}
	if len(res.report.RemovedRemote) == 0 {
		t.Errorf("allow-deletes should propagate the local delete, report %+v", res.report)
	}
	if len(res.report.Pushed) == 0 {
		t.Errorf("local-wins should push the conflicting file, report %+v", res.report)
	}
}

func keyMsg(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// actionIndex returns the position of name within actions, so tests can
// position the cursor without hardcoding row indices.
func actionIndex(actions []string, name string) int {
	for i, a := range actions {
		if a == name {
			return i
		}
	}
	return -1
}

// tuiHeldFixture builds a held checkout with a real engine round trip: a
// checkout followed by a first sync, so the base manifest comes from rsync
// listings exactly as production does.
func tuiHeldFixture(t *testing.T) (name string, p config.Profile, id ident.Ident) {
	t.Helper()
	requireRsync(t)
	root := t.TempDir()
	local := filepath.Join(root, "local")
	remote := filepath.Join(root, "remote")
	_ = os.MkdirAll(remote, 0o755)
	_ = os.WriteFile(filepath.Join(remote, "keep.txt"), []byte("base"), 0o644)
	id = ident.Ident{By: "me@host", Host: "host"}
	p = config.Profile{LocalRoot: local, RemoteRoot: remote}
	r := lifecycle.Runner{ToolVersion: "test"}
	if _, err := r.Checkout(context.Background(), "work", p, id, "", lifecycle.Options{}); err != nil {
		t.Fatalf("fixture checkout: %v", err)
	}
	if _, err := r.Sync(context.Background(), "work", p, id, "", lifecycle.Options{}); err != nil {
		t.Fatalf("fixture first sync: %v", err)
	}
	return "work", p, id
}

// drainStream drains a streaming action command (syncCmd/checkinCmd) to
// completion, collecting the live events and returning the terminal result.
func drainStream(t *testing.T, first tea.Msg) ([]lifecycle.Event, actionResultMsg) {
	t.Helper()
	var events []lifecycle.Event
	msg := first
	for {
		switch v := msg.(type) {
		case tea.BatchMsg:
			// syncConfirmed now batches syncCmd with cylonTick; we only care
			// about the streaming sync events, so execute all commands and
			// find the first syncEventMsg (the cylonTickMsg is ignored).
			for _, cmd := range v {
				if m := cmd(); m != nil {
					if _, ok := m.(syncEventMsg); ok {
						msg = m
						break
					}
				}
			}
		case syncEventMsg:
			events = append(events, v.event)
			msg = waitForMsg(v.ch)()
		case actionResultMsg:
			return events, v
		default:
			t.Fatalf("unexpected message in stream: %T", msg)
		}
	}
}

func TestSyncCmdProducesResult(t *testing.T) {
	t.Setenv("DIBS_STATE", t.TempDir())
	name, p, id := tuiHeldFixture(t)
	// Edit locally after checkout so Sync has something to push.
	_ = os.WriteFile(filepath.Join(p.LocalRoot, "keep.txt"), []byte("EDITED-LONGER"), 0o644)

	runner := lifecycle.Runner{ToolVersion: "test"}
	events, res := drainStream(t, syncCmd(context.Background(), runner, id, name, p, 0, lifecycle.Options{})())
	if res.err != nil {
		t.Fatalf("sync cmd err: %v", res.err)
	}
	if len(res.report.Pushed) == 0 {
		t.Error("want a non-empty Pushed list after editing a local file")
	}
	// The push must have streamed a live event for the edited file.
	if len(events) == 0 {
		t.Fatal("want at least one streamed apply event")
	}
	last := events[len(events)-1]
	if last.Side != lifecycle.SideRemote || last.Path != "keep.txt" {
		t.Errorf("streamed event = %+v, want a remote change to keep.txt", last)
	}
}

// TestSyncConflictShowsConflictingPathInActivity is the I2 regression: a
// stopped-on-conflict Sync must render the conflicting path in the Activity
// box, not just an empty body plus a count-only error string. It drives a
// conflict through syncCmd (as the real Sync action would), feeds the
// resulting actionResultMsg through the model's Update/applyActionResult, and
// asserts the rendered view names the conflicting file.
func TestSyncConflictShowsConflictingPathInActivity(t *testing.T) {
	t.Setenv("DIBS_STATE", t.TempDir())
	name, p, id := tuiHeldFixture(t)
	// Same-file conflict: both sides changed since checkout.
	_ = os.WriteFile(filepath.Join(p.LocalRoot, "keep.txt"), []byte("LOCAL-version"), 0o644)
	_ = os.WriteFile(filepath.Join(p.RemoteRoot, "keep.txt"), []byte("RR"), 0o644)

	runner := lifecycle.Runner{ToolVersion: "test"}
	msg := syncCmd(context.Background(), runner, id, name, p, 0, lifecycle.Options{})()
	res, ok := msg.(actionResultMsg)
	if !ok {
		t.Fatalf("want actionResultMsg, got %T", msg)
	}
	if res.err == nil {
		t.Fatal("want a conflict error from syncCmd")
	}
	if len(res.report.Conflicts) == 0 {
		t.Fatal("want the report to list the conflicting path")
	}

	cfg := &config.Config{Profiles: map[string]config.Profile{name: p}}
	m := newModel("/tmp/x.yaml", cfg)
	m.runner = runner
	m.id = id
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // reveal Actions for name
	if m.profile.name != name {
		t.Fatalf("want profile %q open, got %q", name, m.profile.name)
	}

	m = update(t, m, msg)

	if m.profile.actionErr == nil {
		t.Error("actionErr should still be set on a conflict stop")
	}
	if m.profile.actionReport == nil || len(m.profile.actionReport.Conflicts) == 0 {
		t.Fatal("actionReport with the conflicting paths must be stored even on error")
	}

	view := m.View()
	if !strings.Contains(view, "keep.txt") {
		t.Errorf("Activity view should show the conflicting path %q, got:\n%s", "keep.txt", view)
	}
}

// TestSyncStreamsAppliedChangesLive drives live syncEventMsgs through the model
// and asserts the Activity view fills with status-style rows while the action is
// still in flight (acting), before the terminal actionResultMsg arrives.
func TestSyncStreamsAppliedChangesLive(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{"work": {}}}
	m := newModel("/tmp/x.yaml", cfg)
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // open "work" actions
	m.profile.acting = true

	ch := make(chan tea.Msg, 4)
	m = update(t, m, syncEventMsg{name: "work", event: lifecycle.Event{Kind: lifecycle.EventAdd, Side: lifecycle.SideRemote, Path: "new.txt"}, ch: ch})
	m = update(t, m, syncEventMsg{name: "work", event: lifecycle.Event{Kind: lifecycle.EventDelete, Side: lifecycle.SideLocal, Path: "old.txt"}, ch: ch})

	if len(m.profile.applied) != 2 {
		t.Fatalf("want 2 streamed events, got %d", len(m.profile.applied))
	}
	view := m.View()
	for _, want := range []string{"new.txt", "old.txt", "add", "delete"} {
		if !strings.Contains(view, want) {
			t.Errorf("live Activity view missing %q, got:\n%s", want, view)
		}
	}
}

// TestSyncRefreshesContentsAfterCompletion: a successful sync changed the local
// tree, so the model must re-scan it (scanning=true) and, once the scan result
// arrives, show the refreshed Contents block in the Details box.
func TestSyncRefreshesContentsAfterCompletion(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{"work": {}}}
	m := newModel("/tmp/x.yaml", cfg)
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // open "work"
	m.profile.acting = true

	m = update(t, m, actionResultMsg{name: "work", report: lifecycle.Report{Action: "sync", Pushed: []string{"keep.txt"}}})
	if m.profile.acting {
		t.Error("acting should be cleared after the sync result")
	}
	if !m.profile.scanning {
		t.Error("a completed sync should trigger a Contents re-scan (scanning=true)")
	}
	if view := m.View(); !strings.Contains(view, "scanning") {
		t.Errorf("Details should show the pending Contents scan, got:\n%s", view)
	}

	m = update(t, m, localStatResultMsg{name: "work", stats: localstat.Stats{Dirs: 2, Files: 5, Bytes: 1024}})
	if m.profile.scanning {
		t.Error("scanning should clear once the scan result arrives")
	}
	if view := m.View(); !strings.Contains(view, "Contents") || !strings.Contains(view, "Files") {
		t.Errorf("Details should show the refreshed Contents block, got:\n%s", view)
	}
}

// TestDryRunSyncSkipsContentsRescan: a dry-run wrote nothing, so it must not
// kick off a Contents re-scan.
func TestDryRunSyncSkipsContentsRescan(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{"work": {}}}
	m := newModel("/tmp/x.yaml", cfg)
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = update(t, m, actionResultMsg{name: "work", report: lifecycle.Report{Action: "sync", DryRun: true}})
	if m.profile.scanning {
		t.Error("a dry-run sync must not trigger a Contents re-scan")
	}
}

// TestSyncCompletionFocusesStatus: after a sync completes successfully the
// actions cursor moves back to Status, so verifying the synced state is one
// Enter away. A failed sync keeps the cursor where it was.
func TestSyncCompletionFocusesStatus(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{"work": {}}}
	m := newModel("/tmp/x.yaml", cfg)
	m.checks["work"] = ownLock(m)                    // actions: Status, Sync, Check-in
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // open "work"
	m.profile.cursor = actionIndex(visibleActions(m.checks["work"], m.id, ""), "Sync")
	m.profile.acting = true

	m = update(t, m, actionResultMsg{name: "work", report: lifecycle.Report{Action: "sync"}})
	if want := actionIndex(visibleActions(m.checks["work"], m.id, ""), "Status"); m.profile.cursor != want {
		t.Errorf("a completed sync should move the cursor to Status (%d), got %d", want, m.profile.cursor)
	}

	// A failed sync leaves the cursor on Sync so retrying stays one Enter away.
	m.profile.cursor = actionIndex(visibleActions(m.checks["work"], m.id, ""), "Sync")
	m.profile.acting = true
	m = update(t, m, actionResultMsg{name: "work", report: lifecycle.Report{Action: "sync"}, err: errors.New("boom")})
	if want := actionIndex(visibleActions(m.checks["work"], m.id, ""), "Sync"); m.profile.cursor != want {
		t.Errorf("a failed sync should keep the cursor on Sync (%d), got %d", want, m.profile.cursor)
	}
}

// TestCheckinCompletionReturnsToList: once a check-in completes successfully the
// profile is released, so the model returns to the profile list rather than
// lingering on that profile's (now-inapplicable) action view.
func TestCheckinCompletionReturnsToList(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{"work": {}}}
	m := newModel("/tmp/x.yaml", cfg)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // reveal Actions for "work"
	if m.sub != subActions {
		t.Fatalf("want subActions after entering the profile, got %d", m.sub)
	}
	m = update(t, m, actionResultMsg{name: "work", report: lifecycle.Report{Action: "checkin", Released: true}})
	if m.sub != subList {
		t.Errorf("a completed check-in should return to the profile list, got sub %d", m.sub)
	}
}

// TestCheckinErrorStaysOnProfile: a failed check-in (e.g. a conflict stop) keeps
// the action view open so the error stays on screen.
func TestCheckinErrorStaysOnProfile(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{"work": {}}}
	m := newModel("/tmp/x.yaml", cfg)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = update(t, m, actionResultMsg{name: "work", report: lifecycle.Report{Action: "checkin"}, err: errors.New("boom")})
	if m.sub != subActions {
		t.Errorf("a failed check-in should stay on the profile view, got sub %d", m.sub)
	}
}

// TestActionGatingByCheckoutState: the visible action list is filtered by the
// profile's known checkout state — inapplicable actions are hidden, not greyed.
// A not-checked-out profile offers only Checkout; one checked out by THIS
// machine offers Status, Sync, Check-in in that order; a foreign lock (held
// elsewhere, or a marker too corrupt to attribute) offers only Checkout, where
// the lock can be stolen.
func TestActionGatingByCheckoutState(t *testing.T) {
	id := ident.Ident{By: "me@host", Host: "host"}
	own := &marker.Marker{CheckedOutBy: id.By, Host: id.Host}
	if got := visibleActions(&sanity.Result{CheckedOut: false}, id, ""); !equalStrings(got, []string{"Checkout"}) {
		t.Errorf("not-checked-out actions = %v, want [Checkout]", got)
	}
	if got := visibleActions(&sanity.Result{CheckedOut: true, Marker: own}, id, ""); !equalStrings(got, []string{"Status", "Sync", "Check-in"}) {
		t.Errorf("own-lock actions = %v, want [Status Sync Check-in]", got)
	}
	if got := visibleActions(foreignLock(), id, ""); !equalStrings(got, []string{"Checkout"}) {
		t.Errorf("foreign-lock actions = %v, want [Checkout]", got)
	}
	if got := visibleActions(&sanity.Result{CheckedOut: true}, id, ""); !equalStrings(got, []string{"Checkout"}) {
		t.Errorf("unreadable-marker actions = %v, want [Checkout]", got)
	}
	if got := visibleActions(nil, id, ""); len(got) != 0 {
		t.Errorf("unknown-state actions = %v, want none", got)
	}
	// A marker stamped with another profile's ID is a foreign lock even when
	// identity and host match (two profiles on one machine, same remote root).
	otherProfile := &marker.Marker{CheckedOutBy: id.By, Host: id.Host, ProfileID: "uuid-other"}
	if got := visibleActions(&sanity.Result{CheckedOut: true, Marker: otherProfile}, id, "uuid-mine"); !equalStrings(got, []string{"Checkout"}) {
		t.Errorf("other-profile-lock actions = %v, want [Checkout]", got)
	}

	// A checked-out profile has no Checkout row, so a stray cursor past the list
	// makes Enter a no-op rather than starting a checkout.
	m := model{
		sub: subActions,
		cfg: &config.Config{Profiles: map[string]config.Profile{"work": {}}},
	}
	m.checks = map[string]*sanity.Result{"work": ownLock(m)}
	m.profile = newProfileView("work")
	m.profile.cursor = len(visibleActions(m.checks["work"], m.id, "")) // out of range
	m2, _ := m.updateProfile(keyMsg("enter"))
	if m2.(model).profile.acting {
		t.Error("Enter past the end of the action list must not start an action")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCheckinOpensConfirmModal(t *testing.T) {
	m := model{
		sub: subActions,
		cfg: &config.Config{Profiles: map[string]config.Profile{"work": {}}},
	}
	m.checks = map[string]*sanity.Result{"work": ownLock(m)}
	m.profile = newProfileView("work")
	m.profile.cursor = actionIndex(visibleActions(m.checks["work"], m.id, ""), "Check-in")
	m2, _ := m.updateProfile(keyMsg("enter"))
	if m2.(model).mode != modeConfirm {
		t.Fatal("Check-in Enter should open the confirm modal")
	}
	if m2.(model).confirmKind != confirmCheckin {
		t.Errorf("confirmKind = %v, want confirmCheckin", m2.(model).confirmKind)
	}
}

// TestCheckinOpenResetsCleanCheckbox: each time the check-in dialog opens both
// checkboxes default to unchecked, so a stale value from a previous open can't
// silently carry into a new check-in.
func TestCheckinOpenResetsCleanCheckbox(t *testing.T) {
	m := model{
		sub:            subActions,
		cfg:            &config.Config{Profiles: map[string]config.Profile{"work": {}}},
		checkinClean:   true, // stale from a previous open
		checkinAbandon: true, // stale from a previous open
	}
	m.checks = map[string]*sanity.Result{"work": ownLock(m)}
	m.profile = newProfileView("work")
	m.profile.cursor = actionIndex(visibleActions(m.checks["work"], m.id, ""), "Check-in")
	m2, _ := m.updateProfile(keyMsg("enter"))
	if m2.(model).checkinClean {
		t.Error("opening the check-in dialog should reset the clean checkbox to unchecked")
	}
	if m2.(model).checkinAbandon {
		t.Error("opening the check-in dialog should reset the abandon checkbox to unchecked")
	}
}

// TestCheckinOpensFocusedOnCheckbox: the check-in dialog opens with the first
// checkbox (abandon) focused (a bare enter/space only toggles it, so this is
// still safe against an accidental one-key check-in).
func TestCheckinOpensFocusedOnCheckbox(t *testing.T) {
	m := model{
		sub: subActions,
		cfg: &config.Config{Profiles: map[string]config.Profile{"work": {}}},
	}
	m.checks = map[string]*sanity.Result{"work": ownLock(m)}
	m.profile = newProfileView("work")
	m.profile.cursor = actionIndex(visibleActions(m.checks["work"], m.id, ""), "Check-in")
	m2, _ := m.updateProfile(keyMsg("enter"))
	if m2.(model).confirmFocus != confirmFocusAbandon {
		t.Errorf("check-in dialog should open focused on the abandon checkbox, got %d", m2.(model).confirmFocus)
	}
}
