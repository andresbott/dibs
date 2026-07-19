package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/libs/threewayrsync"
	tea "github.com/charmbracelet/bubbletea"
)

// fakeListers installs canned module/dir listers on the form: modules is
// returned for every module fetch, dirs answers dir fetches by path (a missing
// key errors). Calls are recorded into *dirCalls when non-nil.
func fakeListers(f *formModel, modules []threewayrsync.Module, dirs map[string][]string, dirCalls *[]string) {
	f.listModules = func(_ context.Context, _ threewayrsync.Daemon) ([]threewayrsync.Module, error) {
		return modules, nil
	}
	f.listDirs = func(_ context.Context, _ threewayrsync.Daemon, path string) ([]string, error) {
		if dirCalls != nil {
			*dirCalls = append(*dirCalls, path)
		}
		out, ok := dirs[path]
		if !ok {
			return nil, errors.New("no such path: " + path)
		}
		return out, nil
	}
}

// openRsyncBrowse opens the add form, switches to the rsync kind, fills the
// host, and activates the Module/path Browse button, returning the model and
// the cmd the browse issued.
func openRsyncBrowse(t *testing.T, modules []threewayrsync.Module, dirs map[string][]string) (model, tea.Cmd) {
	t.Helper()
	m := openAddForm(t)
	fakeListers(&m.form, modules, dirs, nil)
	m = tabToTypeSel(t, m)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // → rsync
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Host input
	m = typeRunes(t, m, "nas")
	// Reach the Module/path row's Browse button via its input.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // Port
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // User
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // Password file
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // Module/path input
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // its Browse button
	if m.form.focusKind() != slotButton || m.form.focusField() != idxModulePath {
		t.Fatalf("setup: want the Module/path Browse button, got slot %d field %d", m.form.focus, m.form.focusField())
	}
	nm, cmd := m.updateForm(tea.KeyMsg{Type: tea.KeyEnter})
	return nm.(model), cmd
}

// deliver runs a cmd (when non-nil) and feeds its msg back through the model,
// mirroring the bubbletea runtime.
func deliver(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	if cmd == nil {
		return m
	}
	return update(t, m, cmd())
}

var testModules = []threewayrsync.Module{{Name: "data", Comment: "first"}, {Name: "backup"}}

func TestRemoteBrowseOpensLoadingAndListsModules(t *testing.T) {
	m, cmd := openRsyncBrowse(t, testModules, nil)
	if !m.form.browsingRemote {
		t.Fatal("Browse on the module-path field should open the remote browser")
	}
	if m.form.remote.phase != rpLoadingModules {
		t.Fatalf("phase = %d, want rpLoadingModules", m.form.remote.phase)
	}
	if cmd == nil {
		t.Fatal("opening the browser must issue the module-list cmd")
	}
	m = deliver(t, m, cmd)
	if m.form.remote.phase != rpModules {
		t.Fatalf("phase = %d, want rpModules after the result", m.form.remote.phase)
	}
	view := m.form.View()
	for _, want := range []string{"data", "first", "backup", "Select rsync module"} {
		if !strings.Contains(view, want) {
			t.Fatalf("module list view missing %q:\n%s", want, view)
		}
	}
}

func TestRemoteBrowseRequiresHost(t *testing.T) {
	m := openAddForm(t)
	m = tabToTypeSel(t, m)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // → rsync, host left empty
	for range 4 {
		m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Module/path input
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // Browse button
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.form.browsingRemote {
		t.Fatal("browse without a host must not open the browser")
	}
	if m.form.err == "" {
		t.Fatal("browse without a host should surface a form error")
	}
}

func TestRemoteBrowseBadPortRejected(t *testing.T) {
	m := openAddForm(t)
	fakeListers(&m.form, testModules, nil, nil)
	m = tabToTypeSel(t, m)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // → rsync
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Host
	m = typeRunes(t, m, "nas")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // Port
	m = typeRunes(t, m, "eight")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // User
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // Password file
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // Module/path
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.form.browsingRemote {
		t.Fatal("browse with a non-numeric port must not open the browser")
	}
	if m.form.err != "port must be a number" {
		t.Fatalf("form err = %q", m.form.err)
	}
}

func TestRemoteBrowseSelectModuleListsDirs(t *testing.T) {
	dirs := map[string][]string{"": {"alpha", "beta"}}
	m, cmd := openRsyncBrowse(t, testModules, dirs)
	m = deliver(t, m, cmd) // module list shown, cursor on "data"

	nm, cmd2 := m.updateForm(tea.KeyMsg{Type: tea.KeyEnter}) // open the module
	m = nm.(model)
	if m.form.remote.phase != rpLoadingDirs || m.form.remote.module != "data" {
		t.Fatalf("phase/module = %d/%q, want rpLoadingDirs/data", m.form.remote.phase, m.form.remote.module)
	}
	m = deliver(t, m, cmd2)
	if m.form.remote.phase != rpDirs {
		t.Fatalf("phase = %d, want rpDirs", m.form.remote.phase)
	}
	view := m.form.View()
	for _, want := range []string{"alpha/", "beta/", "Browse data"} {
		if !strings.Contains(view, want) {
			t.Fatalf("dir list view missing %q:\n%s", want, view)
		}
	}
}

// driveRemote runs one key through the form's remote browser and delivers any
// resulting listing cmd.
func driveRemote(t *testing.T, m model, key tea.KeyMsg) model {
	t.Helper()
	nm, cmd := m.updateForm(key)
	return deliver(t, nm.(model), cmd)
}

func TestRemoteBrowseDescendAndUp(t *testing.T) {
	dirs := map[string][]string{
		"":           {"alpha", "beta"},
		"beta":       {"nested"},
		"beta/nested": {},
	}
	m, cmd := openRsyncBrowse(t, testModules, dirs)
	m = deliver(t, m, cmd)
	m = driveRemote(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // module "data" → root
	m = driveRemote(t, m, tea.KeyMsg{Type: tea.KeyDown})  // cursor → "beta"
	m = driveRemote(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // descend into beta
	if got := m.form.remote.path; got != "beta" {
		t.Fatalf("path = %q, want beta", got)
	}
	m = driveRemote(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // descend into nested
	if got := m.form.remote.path; got != "beta/nested" {
		t.Fatalf("path = %q, want beta/nested", got)
	}
	m = driveRemote(t, m, tea.KeyMsg{Type: tea.KeyLeft}) // up → beta
	if got := m.form.remote.path; got != "beta" {
		t.Fatalf("path after up = %q, want beta", got)
	}
	m = driveRemote(t, m, tea.KeyMsg{Type: tea.KeyLeft}) // up → module root
	if got := m.form.remote.path; got != "" {
		t.Fatalf("path after second up = %q, want module root", got)
	}
	// Up from the module root returns to the cached module list, the just-left
	// module highlighted.
	m = driveRemote(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if m.form.remote.phase != rpModules {
		t.Fatalf("phase = %d, want rpModules after up from module root", m.form.remote.phase)
	}
	if got := m.form.remote.entries[m.form.remote.cursor]; got != "data" {
		t.Fatalf("highlighted module = %q, want data", got)
	}
}

func TestRemoteBrowseSpaceConfirmsIntoField(t *testing.T) {
	dirs := map[string][]string{"": {"alpha", "beta"}}
	m, cmd := openRsyncBrowse(t, testModules, dirs)
	m = deliver(t, m, cmd)
	m = driveRemote(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // module "data"
	m = driveRemote(t, m, tea.KeyMsg{Type: tea.KeyDown})  // cursor → "beta"
	m = driveRemote(t, m, spaceKey)                       // select it
	if m.form.browsingRemote {
		t.Fatal("space should confirm and close the browser")
	}
	if got := m.form.inputs[idxModulePath].Value(); got != "data/beta" {
		t.Fatalf("module-path field = %q, want data/beta", got)
	}
	if m.form.focusKind() != slotInput || m.form.focusField() != idxModulePath {
		t.Fatal("focus should return to the module-path input")
	}
}

func TestRemoteBrowseEscKeepsValue(t *testing.T) {
	m := openAddForm(t)
	fakeListers(&m.form, testModules, nil, nil)
	m.form.kind = remoteRsync
	m.form.inputs[idxHost].SetValue("nas")
	m.form.inputs[idxModulePath].SetValue("data/prior")
	m.form.browseSeq++
	m.form.browsingRemote = true
	m.form.remote = remotePicker{phase: rpModules, entries: []string{"data"}, height: 5}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.form.browsingRemote {
		t.Fatal("esc should close the browser")
	}
	if got := m.form.inputs[idxModulePath].Value(); got != "data/prior" {
		t.Fatalf("module-path field = %q, want the prior value kept", got)
	}
}

func TestRemoteBrowseResumesFromFieldValue(t *testing.T) {
	var calls []string
	m := openAddForm(t)
	fakeListers(&m.form, testModules, map[string][]string{"inner": {"x"}}, &calls)
	m = tabToTypeSel(t, m)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // → rsync
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = typeRunes(t, m, "nas")
	for range 4 { // Port, User, Password file, Module/path
		m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	m = typeRunes(t, m, "data/inner") // Module/path field
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	nm, cmd := m.updateForm(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(model)
	// A pre-filled module skips the module list and lists its path directly.
	if m.form.remote.phase != rpLoadingDirs || m.form.remote.module != "data" {
		t.Fatalf("phase/module = %d/%q, want rpLoadingDirs/data", m.form.remote.phase, m.form.remote.module)
	}
	m = deliver(t, m, cmd)
	if m.form.remote.phase != rpDirs {
		t.Fatalf("phase = %d, want rpDirs (err %q)", m.form.remote.phase, m.form.remote.errMsg)
	}
	if len(calls) != 1 || calls[0] != "inner" {
		t.Fatalf("dir listings = %v, want [inner]", calls)
	}
}

func TestRemoteBrowseStaleResultDropped(t *testing.T) {
	m, cmd := openRsyncBrowse(t, testModules, nil)
	stale := remoteListResultMsg{seq: m.form.browseSeq - 1, modules: testModules}
	m = update(t, m, stale)
	if m.form.remote.phase != rpLoadingModules {
		t.Fatalf("a stale-seq result must be dropped, phase = %d", m.form.remote.phase)
	}
	m = deliver(t, m, cmd) // the current one still lands
	if m.form.remote.phase != rpModules {
		t.Fatalf("phase = %d, want rpModules", m.form.remote.phase)
	}
}

func TestRemoteBrowseErrorShowsRetry(t *testing.T) {
	m, cmd := openRsyncBrowse(t, testModules, nil)
	_ = cmd
	m = update(t, m, remoteListResultMsg{seq: m.form.browseSeq, err: errors.New("connection refused")})
	if m.form.remote.phase != rpError {
		t.Fatalf("phase = %d, want rpError", m.form.remote.phase)
	}
	view := m.form.View()
	if !strings.Contains(view, "connection refused") || !strings.Contains(view, "Retry") {
		t.Fatalf("error view missing the message or Retry:\n%s", view)
	}
	// Retry (focus lands on it) re-issues the module listing.
	nm, retryCmd := m.updateForm(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(model)
	if m.form.remote.phase != rpLoadingModules {
		t.Fatalf("phase = %d, want rpLoadingModules after Retry", m.form.remote.phase)
	}
	m = deliver(t, m, retryCmd)
	if m.form.remote.phase != rpModules {
		t.Fatalf("phase = %d, want rpModules after the retried result", m.form.remote.phase)
	}
}

func TestRemoteBrowseErrorCancelCloses(t *testing.T) {
	m, _ := openRsyncBrowse(t, testModules, nil)
	m = update(t, m, remoteListResultMsg{seq: m.form.browseSeq, err: errors.New("boom")})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // Retry → Cancel
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.form.browsingRemote {
		t.Fatal("Cancel in the error state should close the browser")
	}
}

func TestRemoteBrowseEmptyModuleListPlaceholder(t *testing.T) {
	m, cmd := openRsyncBrowse(t, nil, nil)
	m = deliver(t, m, cmd)
	view := m.form.View()
	if !strings.Contains(view, "no modules listed") {
		t.Fatalf("empty module list should show the manual-entry hint:\n%s", view)
	}
	// Confirming with nothing to choose keeps the field untouched and closes.
	m = driveRemote(t, m, spaceKey)
	if m.form.browsingRemote {
		t.Fatal("space on an empty module list should close the browser")
	}
	if got := m.form.inputs[idxModulePath].Value(); got != "" {
		t.Fatalf("module-path field = %q, want empty (nothing chosen)", got)
	}
}

func TestRemoteBrowseTabToButtonsAndSelect(t *testing.T) {
	dirs := map[string][]string{"": {"alpha"}}
	m, cmd := openRsyncBrowse(t, testModules, dirs)
	m = deliver(t, m, cmd)
	m = driveRemote(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // module "data"
	m = driveRemote(t, m, tea.KeyMsg{Type: tea.KeyTab})   // → Select button
	if m.form.remote.focus != focusSelect {
		t.Fatalf("focus = %d, want focusSelect", m.form.remote.focus)
	}
	m = driveRemote(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // activate Select
	if m.form.browsingRemote {
		t.Fatal("Select should confirm and close the browser")
	}
	if got := m.form.inputs[idxModulePath].Value(); got != "data/alpha" {
		t.Fatalf("module-path field = %q, want data/alpha", got)
	}
}

// TestRemoteBrowseFullFlowPersists drives the whole journey through the model:
// pick a module, descend, select a folder, save — the composed rsync:// URL
// lands in the config file.
func TestRemoteBrowseFullFlowPersists(t *testing.T) {
	pth := filepath.Join(t.TempDir(), "config.yaml")
	m := newModel(pth, &config.Config{Profiles: map[string]config.Profile{}})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	fakeListers(&m.form, testModules, map[string][]string{"": {"photos"}}, nil)
	m = typeRunes(t, m, "pics")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = typeRunes(t, m, "/home/me/pics")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // selector
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // → rsync
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Host
	m = typeRunes(t, m, "nas")
	for range 4 {
		m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // → Module/path input
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // Browse
	nm, cmd := m.updateForm(tea.KeyMsg{Type: tea.KeyEnter})
	m = deliver(t, nm.(model), cmd)                        // module list
	m = driveRemote(t, m, tea.KeyMsg{Type: tea.KeyEnter})  // open "data"
	m = driveRemote(t, m, spaceKey)                        // select "photos"
	if got := m.form.inputs[idxModulePath].Value(); got != "data/photos" {
		t.Fatalf("module-path field = %q, want data/photos", got)
	}
	m = tabToKind(t, m, slotSave)
	m = update(t, m, spaceKey)
	if m.mode != modeMain {
		t.Fatalf("want modeMain after save, got %d (err %q)", m.mode, m.form.err)
	}
	saved, err := config.Load(pth)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Profiles["pics"].RemoteRoot; got != "rsync://nas/data/photos" {
		t.Fatalf("saved remote root = %q, want rsync://nas/data/photos", got)
	}
}
