package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/candy-tools/dibs/internal/lifecycle"
	"github.com/candy-tools/dibs/libs/threewayrsync"
	tea "github.com/charmbracelet/bubbletea"
)

// TestEscWhileIdleReturnsToList: with no action in flight, Esc keeps the old
// behavior — leave the profile for the list, no confirm.
func TestEscWhileIdleReturnsToList(t *testing.T) {
	m := openActions(t, testConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.sub != subList {
		t.Errorf("Esc while idle should return to the list, got sub=%d", m.sub)
	}
	if m.mode == modeConfirm {
		t.Error("Esc while idle must not open the cancel confirm")
	}
}

// TestEscWhileActingOpensCancelConfirm: Esc during a running mutating action pops
// the cancel confirm instead of silently leaving it running.
func TestEscWhileActingOpensCancelConfirm(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.acting = true
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeConfirm || m.confirmKind != confirmCancel {
		t.Fatalf("Esc while acting should open the cancel confirm; mode=%d kind=%d", m.mode, m.confirmKind)
	}
	if m.sub != subActions {
		t.Errorf("should stay on the profile actions view, got sub=%d", m.sub)
	}
	if m.confirmFocus != confirmFocusCancel {
		t.Errorf("cancel confirm should open on the safe (keep running) button, got focus=%d", m.confirmFocus)
	}
}

// TestEscWhileCheckingOpensCancelConfirm: Status has no process to kill, but Esc
// still offers to abandon it — and confirming shows the Canceled note.
func TestEscWhileCheckingOpensCancelConfirm(t *testing.T) {
	m := openActions(t, testConfig())
	m.profile.checking = true
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeConfirm || m.confirmKind != confirmCancel {
		t.Fatalf("Esc while checking should open the cancel confirm; mode=%d kind=%d", m.mode, m.confirmKind)
	}

	m = update(t, m, keyMsg("y")) // confirm
	if m.profile.checking {
		t.Error("confirming cancel should clear the checking flag")
	}
	if !m.profile.canceled {
		t.Error("profile should be marked canceled")
	}
	if !strings.Contains(m.View(), "Canceled.") {
		t.Errorf("Activity should show Canceled., got:\n%s", m.View())
	}
}

// cancelingModel drives the full entry into the canceling state: a run with a
// live process (spy cancel func), Esc → confirm. Shared by the canceling tests.
func cancelingModel(t *testing.T, spy *bool) model {
	t.Helper()
	m := openActions(t, testConfig())
	m.cancel = func() { *spy = true }
	m.actionSeq = 7
	m.profile.acting = true
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeConfirm || m.confirmKind != confirmCancel {
		t.Fatalf("Esc while acting should open the cancel confirm; mode=%d kind=%d", m.mode, m.confirmKind)
	}
	return update(t, m, keyMsg("y")) // confirm (activates regardless of focus)
}

// TestCancelConfirmEntersCancelingState: confirming the cancel of a run with a
// live process must not pretend the run is over — the SIGTERM→SIGKILL
// escalation can take several seconds. The model calls the stored cancel and
// enters a canceling state: Activity shows Canceling…, the run still counts as
// in flight, and the seq is kept so the dying run's terminal result is
// recognized (it is what ends the canceling state).
func TestCancelConfirmEntersCancelingState(t *testing.T) {
	canceled := false
	m := cancelingModel(t, &canceled)
	if !canceled {
		t.Error("confirming cancel must call the stored cancel func (kills rsync)")
	}
	if m.cancel != nil {
		t.Error("cancel func should be cleared after use")
	}
	if !m.profile.canceling {
		t.Error("profile should be in the canceling state")
	}
	if m.profile.canceled {
		t.Error("profile must not be marked canceled yet — the process is still dying")
	}
	if m.actionSeq != 7 {
		t.Errorf("actionSeq must be kept so the terminal result is recognized, got %d", m.actionSeq)
	}
	if m.mode != modeMain || m.sub != subActions {
		t.Errorf("should return to the profile actions view, got mode=%d sub=%d", m.mode, m.sub)
	}
	if !strings.Contains(m.View(), "Canceling") {
		t.Errorf("Activity should show Canceling…, got:\n%s", m.View())
	}
	if !strings.Contains(m.View(), "Stopping — please wait") {
		t.Errorf("footer should say the app is stopping, got:\n%s", m.View())
	}
	if strings.Contains(m.View(), ": Cancel") {
		t.Errorf("footer must drop the esc: Cancel hint while canceling, got:\n%s", m.View())
	}
}

// TestCancelingBlocksExit: while the canceled run is still dying, every exit
// path is inert — Ctrl+C must not quit or reopen the confirm, and esc/q must
// not leave the profile or reopen the confirm. Quitting here would orphan the
// rsync tree the escalation is about to kill.
func TestCancelingBlocksExit(t *testing.T) {
	canceled := false
	m := cancelingModel(t, &canceled)

	res, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = res.(model)
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Error("Ctrl+C while canceling must not quit")
		}
	}
	if m.mode == modeConfirm {
		t.Error("Ctrl+C while canceling must not reopen the cancel confirm")
	}

	for _, k := range []string{"esc", "q"} {
		m = update(t, m, keyMsg(k))
		if m.mode == modeConfirm {
			t.Errorf("%q while canceling must not reopen the cancel confirm", k)
		}
		if m.sub != subActions {
			t.Errorf("%q while canceling must stay on the profile view, got sub=%d", k, m.sub)
		}
	}
}

// TestTerminalResultFinishesCanceling: the canceled run's terminal result (same
// seq, carrying context.Canceled) ends the canceling state — the profile shows
// Canceled., the run no longer counts as in flight, and the cancellation error
// is not displayed as a failure.
func TestTerminalResultFinishesCanceling(t *testing.T) {
	canceled := false
	m := cancelingModel(t, &canceled)

	m = update(t, m, actionResultMsg{name: "alpha", seq: 7, report: lifecycle.Report{Action: "sync"}, err: context.Canceled})
	if m.profile.canceling {
		t.Error("the terminal result must end the canceling state")
	}
	if !m.profile.canceled {
		t.Error("profile should be marked canceled")
	}
	if m.profile.acting {
		t.Error("the run is over; acting must be cleared")
	}
	if m.profile.actionErr != nil {
		t.Errorf("the cancellation error must not display as a failure, got %v", m.profile.actionErr)
	}
	if !strings.Contains(m.View(), "Canceled.") {
		t.Errorf("Activity should show Canceled., got:\n%s", m.View())
	}
}

// TestCancelingRunThatFinishesCleanlyShowsResult: if the run wins the race and
// completes before the kill lands (terminal result with no error), nothing was
// actually canceled — show the real result, not a Canceled note.
func TestCancelingRunThatFinishesCleanlyShowsResult(t *testing.T) {
	canceled := false
	m := cancelingModel(t, &canceled)

	m = update(t, m, actionResultMsg{name: "alpha", seq: 7, report: lifecycle.Report{Action: "sync"}})
	if m.profile.canceling {
		t.Error("the terminal result must end the canceling state")
	}
	if m.profile.canceled {
		t.Error("a cleanly finished run was not canceled; no Canceled note")
	}
	if m.profile.actionReport == nil {
		t.Fatal("the clean result should be stored and shown")
	}
}

// TestCanceledActionDropsStragglerResult: after a cancel the canceled run's
// terminal message still arrives (carrying context.Canceled). Its stale seq must
// cause it to be dropped so it can't replace the Canceled note with a scary error.
func TestCanceledActionDropsStragglerResult(t *testing.T) {
	m := openActions(t, testConfig())
	m.actionSeq = 3
	m.profile.acting = true
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = update(t, m, keyMsg("y")) // confirm → actionSeq becomes 4, canceled=true

	// The straggler carries the pre-cancel seq (3), not the current 4.
	m = update(t, m, actionResultMsg{name: "alpha", seq: 3, report: lifecycle.Report{Action: "sync"}, err: context.Canceled})
	if m.profile.actionErr != nil {
		t.Error("a straggler from the canceled run must not set actionErr")
	}
	if !m.profile.canceled {
		t.Error("the Canceled note must survive the dropped straggler")
	}
	if !strings.Contains(m.View(), "Canceled.") {
		t.Errorf("Activity should still show Canceled., got:\n%s", m.View())
	}
}

// TestNaturalCompletionReleasesContext: when an action finishes on its own, the
// model must release its cancelable context (call cancel) rather than leak it.
func TestNaturalCompletionReleasesContext(t *testing.T) {
	m := openActions(t, testConfig()) // opens "alpha"
	released := false
	m.cancel = func() { released = true }
	m.actionSeq = 5
	m.profile.acting = true

	m = update(t, m, actionResultMsg{name: "alpha", seq: 5, report: lifecycle.Report{Action: "sync"}})
	if !released {
		t.Error("a naturally completed action should release its context (call cancel)")
	}
	if m.cancel != nil {
		t.Error("cancel should be cleared after release")
	}
}

// blockingSyncer's Sync signals it has started, then blocks until the context is
// canceled — standing in for a long rsync transfer so a test can prove canceling
// the context (what cancelAction does) actually unblocks the run.
type blockingSyncer struct {
	once    sync.Once
	started chan struct{}
}

func (b *blockingSyncer) Sync(ctx context.Context, _, _ threewayrsync.Endpoint, _ threewayrsync.Options) (threewayrsync.Result, error) {
	b.once.Do(func() { close(b.started) })
	<-ctx.Done()
	return threewayrsync.Result{}, ctx.Err()
}

func (*blockingSyncer) Diff(context.Context, threewayrsync.Endpoint, threewayrsync.Endpoint, threewayrsync.Options) (threewayrsync.Plan, error) {
	return threewayrsync.Plan{}, nil
}

func (*blockingSyncer) List(context.Context, threewayrsync.Endpoint, threewayrsync.Options) (threewayrsync.Manifest, error) {
	return threewayrsync.Manifest{}, nil
}

// TestSyncCancelStopsRunningRsync proves the cancelable context reaches the rsync
// call: syncCmd starts the transfer, canceling the context unblocks it, and the
// run returns an error rather than hanging.
func TestSyncCancelStopsRunningRsync(t *testing.T) {
	t.Setenv("DIBS_STATE", t.TempDir())
	name, p, id := tuiHeldFixture(t)
	// A local edit gives the reconcile a push to apply, so it reaches Syncer.Sync.
	if err := os.WriteFile(filepath.Join(p.LocalRoot, "keep.txt"), []byte("EDITED-LONGER"), 0o644); err != nil {
		t.Fatal(err)
	}

	bs := &blockingSyncer{started: make(chan struct{})}
	runner := lifecycle.Runner{NewSyncer: func(threewayrsync.Store) lifecycle.Syncer { return bs }, ToolVersion: "test"}
	ctx, cancel := context.WithCancel(context.Background())
	// syncCmd starts the background goroutine immediately; the fake Sync blocks.
	cmd := syncCmd(ctx, runner, id, name, p, 1, lifecycle.Options{})
	select {
	case <-bs.started:
	case <-time.After(2 * time.Second):
		t.Fatal("sync never reached the rsync call")
	}

	cancel() // exactly what confirming the cancel modal does
	_, res := drainStream(t, cmd())
	if res.err == nil {
		t.Fatal("a canceled sync must return an error, not complete cleanly")
	}
}
