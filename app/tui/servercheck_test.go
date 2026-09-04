package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/libs/threewayrsync"
)

// stubChecker installs a connection checker on the server form that records the
// daemon it was called with into *got (when non-nil) and returns err.
func stubChecker(f *serverFormModel, got *threewayrsync.Daemon, err error) {
	f.checkConn = func(_ context.Context, d threewayrsync.Daemon) error {
		if got != nil {
			*got = d
		}
		return err
	}
}

func newCheckModel(t *testing.T) (model, string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{Servers: map[string]config.Server{}, Profiles: map[string]config.Profile{}}
	m := newModel(path, cfg)
	m.mode = modeServerForm
	m.serverForm = newServerForm("", config.Server{}, path)
	return m, path, dir
}

// TestSubmitServerKicksOffCheck: pressing Save runs the connection check against
// the form's host/user with the typed password, and does NOT persist the server
// until the check comes back.
func TestSubmitServerKicksOffCheck(t *testing.T) {
	m, _, _ := newCheckModel(t)
	setServerFormFields(&m.serverForm, "nas", "nas.local", "8730", "bob", "hunter2", "")
	var got threewayrsync.Daemon
	stubChecker(&m.serverForm, &got, nil)

	mm, cmd := m.submitServer()
	m = mm.(model)
	if cmd == nil {
		t.Fatal("Save should issue a connection-check cmd")
	}
	if !m.serverForm.checking {
		t.Fatal("form should be in the checking state")
	}
	if _, ok := m.cfg.Servers["nas"]; ok {
		t.Fatal("server must not be persisted before the check completes")
	}
	// Run the cmd so the checker records the daemon it was probed with.
	_ = cmd()
	if got.Host != "nas.local" || got.User != "bob" || got.Port != 8730 {
		t.Fatalf("checked daemon = %+v, want host nas.local / user bob / port 8730", got)
	}
	if got.PasswordFile == "" {
		t.Fatal("checker should receive a password file holding the typed secret")
	}
}

// TestSubmitServerSuccessSaves: a passing check persists the server and its
// password file and returns to the servers list.
func TestSubmitServerSuccessSaves(t *testing.T) {
	m, _, dir := newCheckModel(t)
	setServerFormFields(&m.serverForm, "nas", "nas.local", "", "bob", "hunter2", "")
	stubChecker(&m.serverForm, nil, nil)

	mm, cmd := m.submitServer()
	m = mm.(model)
	m = deliver(t, m, cmd) // run the check, feed the result back

	if m.serverForm.err != "" {
		t.Fatalf("unexpected error: %s", m.serverForm.err)
	}
	if m.mode != modeServers {
		t.Fatalf("mode = %d, want modeServers after a successful save", m.mode)
	}
	if _, ok := m.cfg.Servers["nas"]; !ok {
		t.Fatal("server was not persisted after a passing check")
	}
	data, err := os.ReadFile(filepath.Join(dir, "nas.pw"))
	if err != nil || string(data) != "hunter2\n" {
		t.Fatalf("password file = %q err=%v", string(data), err)
	}
}

// TestSubmitServerFailureStaysInForm: a failing check keeps the form open with
// the error and persists nothing.
func TestSubmitServerFailureStaysInForm(t *testing.T) {
	m, _, dir := newCheckModel(t)
	setServerFormFields(&m.serverForm, "nas", "nas.local", "", "bob", "hunter2", "")
	stubChecker(&m.serverForm, nil, errors.New("rsync list-dirs: exit 5: @ERROR: auth failed on module mainData"))

	mm, cmd := m.submitServer()
	m = mm.(model)
	m = deliver(t, m, cmd)

	if m.serverForm.checking {
		t.Fatal("checking flag should be cleared after the result")
	}
	if m.serverForm.err == "" {
		t.Fatal("a failing check must surface an error on the form")
	}
	if m.mode != modeServerForm {
		t.Fatalf("mode = %d, want modeServerForm (back to editing)", m.mode)
	}
	if _, ok := m.cfg.Servers["nas"]; ok {
		t.Fatal("server must not be persisted when the check fails")
	}
	if _, err := os.Stat(filepath.Join(dir, "nas.pw")); !os.IsNotExist(err) {
		t.Fatal("no managed password file should be written when the check fails")
	}
}

// TestSubmitServerValidationErrorSkipsCheck: an invalid form never reaches the
// connection check.
func TestSubmitServerValidationErrorSkipsCheck(t *testing.T) {
	m, _, _ := newCheckModel(t)
	setServerFormFields(&m.serverForm, "nas", "", "", "", "", "") // blank host
	called := false
	m.serverForm.checkConn = func(_ context.Context, _ threewayrsync.Daemon) error {
		called = true
		return nil
	}
	mm, cmd := m.submitServer()
	m = mm.(model)
	if cmd != nil {
		t.Fatal("an invalid form should not issue a check cmd")
	}
	if called || m.serverForm.checking {
		t.Fatal("an invalid form should not enter the checking state")
	}
	if m.serverForm.err == "" {
		t.Fatal("validation error should be shown")
	}
}
