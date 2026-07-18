package threewayrsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// runner executes a command (rsync or ssh). It is injectable so tests need not shell out.
type runner func(ctx context.Context, bin string, args []string, tee io.Writer) (runResult, error)

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
	err := cmd.Run()
	res := runResult{stdout: out.String(), stderr: errb.String()}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.exitCode = exitErr.ExitCode()
	}
	return res, err
}
