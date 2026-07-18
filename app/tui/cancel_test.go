package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andresbott/netcheckout/internal/lifecycle"
	"github.com/andresbott/netcheckout/libs/threewayrsync"
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

// TestCancelConfirmStopsActionAndShowsCanceled drives the whole flow with a spy
// cancel func: Esc opens the confirm, confirming calls the stored cancel (which
// is what kills the live rsync), bumps the generation, and lands back on the
// profile with a Canceled note.
func TestCancelConfirmStopsActionAndShowsCanceled(t *testing.T) {
	m := openActions(t, testConfig())
	canceled := false
	m.cancel = func() { canceled = true }
	m.actionSeq = 7
	m.profile.acting = true

	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeConfirm || m.confirmKind != confirmCancel {
		t.Fatalf("Esc while acting should open the cancel confirm; mode=%d kind=%d", m.mode, m.confirmKind)
	}

	m = update(t, m, keyMsg("y")) // confirm (activates regardless of focus)
	if !canceled {
		t.Error("confirming cancel must call the stored cancel func (kills rsync)")
	}
	if m.cancel != nil {
		t.Error("cancel func should be cleared after use")
	}
	if !m.profile.canceled {
		t.Error("profile should be marked canceled")
	}
	if m.actionSeq != 8 {
		t.Errorf("actionSeq should be bumped to invalidate stragglers, got %d", m.actionSeq)
	}
	if m.mode != modeMain || m.sub != subActions {
		t.Errorf("should return to the profile actions view, got mode=%d sub=%d", m.mode, m.sub)
	}
	if !strings.Contains(m.View(), "Canceled.") {
		t.Errorf("Activity should show Canceled., got:\n%s", m.View())
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

// TestSyncCancelStopsRunningRsync proves the cancelable context reaches the rsync
// call: syncCmd starts the transfer, canceling the context unblocks it, and the
// run returns an error rather than hanging.
func TestSyncCancelStopsRunningRsync(t *testing.T) {
	t.Setenv("NETCHECKOUT_STATE", t.TempDir())
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
