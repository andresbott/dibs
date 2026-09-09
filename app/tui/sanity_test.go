package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/candy-tools/dibs/internal/config"
	"github.com/candy-tools/dibs/internal/sanity"
	tea "github.com/charmbracelet/bubbletea"
)

func TestInitDispatchesChecks(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init should return a command batch when profiles exist")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init's cmd produced %T, want tea.BatchMsg", msg)
	}
	if len(batch) != 3 {
		t.Errorf("batch has %d commands, want 3 (one sanityCmd per profile + the rsync check)", len(batch))
	}
}

// TestInitEmptyConfigStillChecksRsync guards Init's zero-profile edge: even with
// no profiles the rsync binary check must run, so a broken system rsync surfaces
// before the first profile is ever created.
func TestInitEmptyConfigStillChecksRsync(t *testing.T) {
	m := newModel("/tmp/x.yaml", &config.Config{Profiles: map[string]config.Profile{}})
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init should return the rsync check command even with no profiles")
	}
	if _, ok := cmd().(rsyncCheckMsg); !ok {
		t.Error("Init's only command with no profiles should be the rsync check")
	}
}

func TestSanityCmdChecksFilesystem(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{"p": {LocalRoot: t.TempDir(), RemoteRoot: t.TempDir()}}}
	msg := sanityCmd(cfg, "p", "")()
	res, ok := msg.(sanityResultMsg)
	if !ok {
		t.Fatalf("sanityCmd produced %T, want sanityResultMsg", msg)
	}
	if !res.result.LocalRoot || !res.result.RemoteRoot {
		t.Errorf("result = %+v, want both roots present", res.result)
	}
}

func TestSanityResultStored(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	m = update(t, m, sanityResultMsg{name: "alpha", result: sanity.Result{LocalRoot: true, RemoteRoot: true}})
	got, ok := m.checks["alpha"]
	if !ok || got == nil {
		t.Fatal("sanityResultMsg should store a result for alpha")
	}
	if !got.LocalRoot || !got.RemoteRoot {
		t.Errorf("stored result = %+v, want both roots true", *got)
	}
}

func TestDetailsShowsCheckingThenResult(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if !strings.Contains(m.View(), "…") {
		t.Errorf("before results, Details should show '…':\n%s", m.View())
	}
	m = update(t, m, sanityResultMsg{name: "alpha", result: sanity.Result{LocalRoot: true, RemoteRoot: true}})
	if !strings.Contains(m.View(), "✓") {
		t.Errorf("after result, Details should show a ✓ mark:\n%s", m.View())
	}
}

func TestDeleteDropsCheck(t *testing.T) {
	m := newModel(filepath.Join(t.TempDir(), "c.yaml"), testConfig())
	m.checks["alpha"] = &sanity.Result{LocalRoot: true}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")}) // delete-confirm on alpha
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}) // confirm
	if _, ok := m.checks["alpha"]; ok {
		t.Error("deleting a profile should drop its sanity cache entry")
	}
}

// TestSubmitFormReturnsSanityRecheckCmd covers submitForm's post-save re-check:
// a successful save must return sanityCmd for the freshly saved profile, not a
// nil command, or the Details pane goes stale (still showing the pre-edit
// result) after an edit.
func TestSubmitFormReturnsSanityRecheckCmd(t *testing.T) {
	m := newModel(filepath.Join(t.TempDir(), "c.yaml"), testConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")}) // edit "alpha" (sorts first)
	if m.mode != modeForm {
		t.Fatalf("want modeForm after e, got %d", m.mode)
	}
	m = tabToKind(t, m, slotSave)

	nm, cmd := m.Update(spaceKey) // drive Save directly to capture the returned cmd
	m, ok := nm.(model)
	if !ok {
		t.Fatalf("Update returned %T, want model", nm)
	}
	if m.mode != modeMain {
		t.Fatalf("a valid save should return to modeMain, got %d", m.mode)
	}
	if cmd == nil {
		t.Fatal("a successful save should return the sanity re-check command, not nil")
	}
	msg := cmd()
	res, ok := msg.(sanityResultMsg)
	if !ok {
		t.Fatalf("save's returned cmd produced %T, want sanityResultMsg", msg)
	}
	if res.name != "alpha" {
		t.Errorf("re-check result name = %q, want alpha", res.name)
	}
}

// TestSubmitFormRenameDropsOldCheck covers submitForm's rename branch: renaming
// a profile must drop the old name's cached sanity result, or a stale check for
// a name that no longer exists in the config would linger in m.checks forever.
func TestSubmitFormRenameDropsOldCheck(t *testing.T) {
	m := newModel(filepath.Join(t.TempDir(), "c.yaml"), testConfig())
	m.checks["alpha"] = &sanity.Result{LocalRoot: true, RemoteRoot: true}

	// White-box setup (mirrors form_test.go's TestEditRenameReplacesKey): build
	// the edit form with newForm, the same constructor the "e" key uses, and
	// mutate its name input directly rather than typing over the prefilled value.
	m.mode = modeForm
	m.form = newForm("alpha", m.cfg.Profiles["alpha"], m.cfg.Servers)
	m.form.inputs[0].SetValue("gamma")
	m = tabToKind(t, m, slotSave)
	m = update(t, m, spaceKey)

	if m.mode != modeMain {
		t.Fatalf("a valid save should return to modeMain, got %d", m.mode)
	}
	if _, exists := m.cfg.Profiles["alpha"]; exists {
		t.Error("old profile key should be gone after rename")
	}
	if _, exists := m.cfg.Profiles["gamma"]; !exists {
		t.Error("new profile key should exist after rename")
	}
	if _, ok := m.checks["alpha"]; ok {
		t.Error("renaming a profile should drop the old name's sanity cache entry")
	}
}
