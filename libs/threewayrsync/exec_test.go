package threewayrsync

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExecRunCapturesStdout(t *testing.T) {
	res, err := execRun(context.Background(), "sh", []string{"-c", "printf hello"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.stdout != "hello" {
		t.Errorf("stdout = %q", res.stdout)
	}
}

func TestExecRunCapturesStderrAndExitCode(t *testing.T) {
	res, err := execRun(context.Background(), "sh", []string{"-c", "printf boom >&2; exit 23"}, nil)
	if err == nil {
		t.Fatal("want error")
	}
	if res.exitCode != 23 || strings.TrimSpace(res.stderr) != "boom" {
		t.Errorf("res = %#v", res)
	}
}

// pidCapture is a tee writer that reports the first stdout line, parsed as a
// PID, on ch while the command is still running.
type pidCapture struct {
	ch   chan int
	buf  strings.Builder
	sent bool
}

func (p *pidCapture) Write(b []byte) (int, error) {
	if !p.sent {
		p.buf.Write(b)
		if s := p.buf.String(); strings.Contains(s, "\n") {
			if pid, err := strconv.Atoi(strings.TrimSpace(s[:strings.Index(s, "\n")])); err == nil {
				p.ch <- pid
				p.sent = true
			}
		}
	}
	return len(b), nil
}

// Canceling must kill rsync's whole process tree, not just the parent: for the
// daemon and ssh transports rsync forks helpers that hold the network socket
// themselves and keep transferring after the parent dies. The sh + background
// sleep stands in for that shape.
func TestExecRunCancelKillsProcessGroup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pc := &pidCapture{ch: make(chan int, 1)}
	done := make(chan error, 1)
	go func() {
		_, err := execRun(ctx, "sh", []string{"-c", "sleep 30 & echo $!; wait"}, pc)
		done <- err
	}()
	var child int
	select {
	case child = <-pc.ch:
	case <-time.After(5 * time.Second):
		t.Fatal("never saw the child pid on stdout")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("want an error after cancel")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("execRun did not return after cancel")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(child, 0)
		if errors.Is(err, syscall.ESRCH) {
			return // the forked child is gone too
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(child, syscall.SIGKILL)
			t.Fatalf("forked child %d still alive after cancel (kill err=%v)", child, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A daemon-transport rsync blocked on socket I/O can sit through the group
// SIGTERM (rsync handles signals only at safe points), so cancel must escalate
// to SIGKILLing the whole group — Go's WaitDelay alone only kills the direct
// child. The trap '' TERM tree stands in for that shape: ignored dispositions
// survive exec, so the background sleep ignores the TERM too.
func TestExecRunCancelEscalatesToKillWhenTermIgnored(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pc := &pidCapture{ch: make(chan int, 1)}
	done := make(chan error, 1)
	go func() {
		_, err := execRun(ctx, "sh", []string{"-c", "trap '' TERM; sleep 30 & echo $!; wait"}, pc)
		done <- err
	}()
	var child int
	select {
	case child = <-pc.ch:
	case <-time.After(5 * time.Second):
		t.Fatal("never saw the child pid on stdout")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("want an error after cancel")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("execRun did not return after cancel")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(child, 0)
		if errors.Is(err, syscall.ESRCH) {
			return // the TERM-ignoring forked child was SIGKILLed too
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(child, syscall.SIGKILL)
			t.Fatalf("TERM-ignoring child %d survived cancel (kill err=%v)", child, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestErrorMessageIncludesStderr(t *testing.T) {
	e := &Error{Op: "list", ExitCode: 23, Stderr: "boom"}
	if got := e.Error(); !strings.Contains(got, "list") || !strings.Contains(got, "boom") {
		t.Errorf("Error() = %q", got)
	}
}

func TestErrorSurfacesCauseWhenNoExit(t *testing.T) {
	base := errors.New("exec: not found")
	e := &Error{Op: "list", ExitCode: 0, Err: base}
	if got := e.Error(); strings.Contains(got, "exit 0") {
		t.Errorf("should not say exit 0: %q", got)
	}
	if !errors.Is(e, base) {
		t.Error("Unwrap must expose the cause")
	}
}
