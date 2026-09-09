package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/candy-tools/dibs/internal/config"
	"github.com/candy-tools/dibs/libs/threewayrsync"
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
	// Set up a server so the form has something to select
	m.form.setServers(map[string]config.Server{"nas": {Host: "nas.local"}})
	fakeListers(&m.form, modules, dirs, nil)
	m = tabToTypeSel(t, m)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // → rsync
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Server selector (nas row)
	m = update(t, m, spaceKey)                       // select nas
	// Reach the Module/path row's Browse button via its input.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Module/path input
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

func TestRemoteBrowseRequiresServer(t *testing.T) {
	m := openAddForm(t)
	m.form.setServers(map[string]config.Server{"nas": {Host: "nas.local"}})
	m = tabToTypeSel(t, m)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // → rsync, server not selected
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Server selector
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Module/path input
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // Browse button
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.form.browsingRemote {
		t.Fatal("browse without a selected server must not open the browser")
	}
	if m.form.err == "" {
		t.Fatal("browse without a selected server should surface a form error")
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
		"":            {"alpha", "beta"},
		"beta":        {"nested"},
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

// browseIntoData opens the browser, lists modules, and descends into "data" so
// the picker is in rpDirs. It installs a makeDir seam recording the created path
// and a listDirs that returns dirsAfter once a folder has been created.
func browseIntoData(t *testing.T, made *string, dirsAfter []string) model {
	t.Helper()
	m, cmd := openRsyncBrowse(t, testModules, map[string][]string{"": {"alpha"}})
	m = deliver(t, m, cmd)                                // modules
	m = driveRemote(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // descend into "data" → rpDirs
	if m.form.remote.phase != rpDirs {
		t.Fatalf("setup: phase = %d, want rpDirs", m.form.remote.phase)
	}
	m.form.makeDir = func(_ context.Context, _ threewayrsync.Daemon, p string) error {
		if made != nil {
			*made = p
		}
		return nil
	}
	if dirsAfter != nil {
		m.form.listDirs = func(_ context.Context, _ threewayrsync.Daemon, _ string) ([]string, error) {
			return dirsAfter, nil
		}
	}
	return m
}

func typeRemote(t *testing.T, m model, s string) model {
	t.Helper()
	for _, r := range s {
		m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// TestRemoteNewFolderCreatesAndHighlights: pressing "n", typing a name, and
// Enter creates the folder on the remote, re-lists, and highlights it.
func TestRemoteNewFolderCreatesAndHighlights(t *testing.T) {
	var made string
	m := browseIntoData(t, &made, []string{"alpha", "newdir"})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}) // open the prompt
	if m.form.remote.phase != rpNewDir {
		t.Fatalf("phase = %d, want rpNewDir after n", m.form.remote.phase)
	}
	m = typeRemote(t, m, "newdir")

	nm, cmd := m.updateForm(tea.KeyMsg{Type: tea.KeyEnter}) // submit → creating
	m = nm.(model)
	if m.form.remote.phase != rpCreating {
		t.Fatalf("phase = %d, want rpCreating after submit", m.form.remote.phase)
	}
	// Deliver the create result, then the re-list it triggers.
	nm, cmd = m.updateForm(cmd())
	m = nm.(model)
	m = deliver(t, m, cmd)

	if made != "newdir" {
		t.Errorf("created path = %q, want newdir", made)
	}
	if m.form.remote.phase != rpDirs {
		t.Fatalf("phase = %d, want rpDirs after create", m.form.remote.phase)
	}
	if got := m.form.remote.entries[m.form.remote.cursor]; got != "newdir" {
		t.Errorf("highlighted entry = %q, want the new folder", got)
	}
}

// TestRemoteNewFolderReadOnlyError: a create rejected by the daemon returns to
// the name prompt with the error shown and the typed name kept.
func TestRemoteNewFolderReadOnlyError(t *testing.T) {
	m := browseIntoData(t, nil, nil)
	m.form.makeDir = func(_ context.Context, _ threewayrsync.Daemon, _ string) error {
		return errors.New("@ERROR: chdir failed")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = typeRemote(t, m, "nope")
	nm, cmd := m.updateForm(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(model)
	m = update(t, m, cmd()) // deliver the failed create result

	if m.form.remote.phase != rpNewDir {
		t.Fatalf("phase = %d, want rpNewDir after a failed create", m.form.remote.phase)
	}
	if !strings.Contains(m.form.remote.errMsg, "chdir failed") {
		t.Errorf("errMsg = %q, want the server error", m.form.remote.errMsg)
	}
	if got := m.form.remote.input.Value(); got != "nope" {
		t.Errorf("input = %q, want the typed name kept", got)
	}
}

// TestRemoteNewFolderRejectsEmptyName: submitting a blank name shows an inline
// error and issues no create.
func TestRemoteNewFolderRejectsEmptyName(t *testing.T) {
	m := browseIntoData(t, nil, nil)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	nm, cmd := m.updateForm(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(model)
	if cmd != nil {
		t.Fatal("a blank name must not issue a create cmd")
	}
	if m.form.remote.phase != rpNewDir || m.form.remote.errMsg == "" {
		t.Fatalf("want to stay on the prompt with an error, phase=%d err=%q", m.form.remote.phase, m.form.remote.errMsg)
	}
}

// TestRemoteNewFolderNotOnModuleList: "n" does nothing on the module list (you
// cannot create a module).
func TestRemoteNewFolderNotOnModuleList(t *testing.T) {
	m, cmd := openRsyncBrowse(t, testModules, map[string][]string{"": {"alpha"}})
	m = deliver(t, m, cmd) // rpModules
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.form.remote.phase != rpModules {
		t.Fatalf("phase = %d, want rpModules (n is a no-op on the module list)", m.form.remote.phase)
	}
}

func TestRemoteBrowseEscKeepsValue(t *testing.T) {
	m := openAddForm(t)
	fakeListers(&m.form, testModules, nil, nil)
	m.form.kind = remoteRsync
	m.form.setServers(map[string]config.Server{"nas": {Host: "nas.local"}})
	m.form.selectServer("nas")
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
	m.form.setServers(map[string]config.Server{"nas": {Host: "nas.local"}})
	fakeListers(&m.form, testModules, map[string][]string{"inner": {"x"}}, &calls)
	m = tabToTypeSel(t, m)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // → rsync
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Server selector (nas row)
	m = update(t, m, spaceKey)                       // select nas
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Module/path field
	m = typeRunes(t, m, "data/inner")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // Browse button
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
// select a server, pick a module, descend, select a folder, save — the
// Server+RemoteModule land in the config file.
func TestRemoteBrowseFullFlowPersists(t *testing.T) {
	pth := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{
		Profiles: map[string]config.Profile{},
		Servers:  map[string]config.Server{"nas": {Host: "nas.local"}},
	}
	m := newModel(pth, cfg)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	fakeListers(&m.form, testModules, map[string][]string{"": {"photos"}}, nil)
	m = typeRunes(t, m, "pics")
	m.form.inputs[idxLocal].SetValue("/home/me/pics")
	m = tabToTypeSel(t, m)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // → rsync
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Server selector (nas row)
	m = update(t, m, spaceKey)                       // select nas
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // Module/path input
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // Browse
	nm, cmd := m.updateForm(tea.KeyMsg{Type: tea.KeyEnter})
	m = deliver(t, nm.(model), cmd)                       // module list
	m = driveRemote(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // open "data"
	m = driveRemote(t, m, spaceKey)                       // select "photos"
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
	got := saved.Profiles["pics"]
	if got.Server != "nas" || got.RemoteModule != "data/photos" {
		t.Fatalf("saved Server=%q RemoteModule=%q, want nas, data/photos", got.Server, got.RemoteModule)
	}
	if got.RemoteRoot != "" {
		t.Fatalf("saved RemoteRoot=%q, want empty (server-backed)", got.RemoteRoot)
	}
}
