package threewayrsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// runner executes a command (rsync or ssh). It is injectable so tests need not shell out.
type runner func(ctx context.Context, bin string, args []string, tee io.Writer) (runResult, error)

// killGrace is how long a canceled rsync gets to shut down cleanly after the
// group SIGTERM before the whole group is SIGKILLed.
const killGrace = 5 * time.Second

type runResult struct {
	stdout   string
	stderr   string
	exitCode int
}

// Error is returned when rsync (or ssh) exits non-zero, carrying enough detail for an
// actionable message.
type Error struct {
	Op       string // "list" | "pull" | "push" | "delete"
	Args     []string
	Stderr   string
	ExitCode int
	Err      error
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("rsync %s: exit %d", e.Op, e.ExitCode)
	if s := strings.TrimSpace(e.Stderr); s != "" {
		return msg + ": " + s
	}
	// No exit status and no stderr (e.g. the binary failed to start); surface the cause
	// rather than a bare "exit 0".
	if e.ExitCode == 0 && e.Err != nil {
		return fmt.Sprintf("rsync %s: %v", e.Op, e.Err)
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Err }

// execRun runs the command to completion, capturing stdout and stderr and, when tee is
// non-nil, mirroring stdout to it live. Only stdout is teed: the tee parses rsync's
// itemize stream, and interleaving stderr chatter into it would garble lines and produce
// wrong progress events.
func execRun(ctx context.Context, bin string, args []string, tee io.Writer) (runResult, error) {
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // G204: bin and args are built by this package from typed fields, not untrusted external input.
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if tee != nil {
		cmd.Stdout = io.MultiWriter(&out, tee)
	}
	// rsync is never a single process: the daemon and ssh transports fork helpers
	// (the ssh child, rsync's receiver) that hold the connection themselves, and a
	// default cancel only SIGKILLs the parent — the helpers keep transferring and
	// cmd.Wait blocks on the inherited stdout pipe until they finish. Run the tree
	// as its own process group and signal the whole group: SIGTERM first so rsync
	// can tear down cleanly (--partial-dir resume state, daemon connection), then
	// SIGKILL the group after killGrace — an rsync blocked on socket I/O only acts
	// on signals at its safe points and can sit through the SIGTERM indefinitely.
	// Go's own WaitDelay escalation is not enough: it SIGKILLs only the direct
	// child, never the group; it stays on solely to close the inherited pipes so
	// Wait cannot block past the group kill.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	finished := make(chan struct{})
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		go func() {
			select {
			case <-finished: // exited within the grace: the pgid may be reused, do not kill it
			case <-time.After(killGrace):
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		}()
		return err
	}
	cmd.WaitDelay = killGrace + time.Second
	err := cmd.Run()
	close(finished)
	res := runResult{stdout: out.String(), stderr: errb.String()}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.exitCode = exitErr.ExitCode()
	}
	return res, err
}
