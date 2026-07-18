package threewayrsync

import (
	"context"
	"errors"
	"strings"
	"testing"
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
