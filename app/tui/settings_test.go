package tui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andresbott/netcheckout/internal/config"
	"github.com/andresbott/netcheckout/internal/ident"
	tea "github.com/charmbracelet/bubbletea"
)

// errFake stands in for a failed rsync binary check in the tests below.
var errFake = errors.New("openrsync detected (fake)")

// TestIdentityShortcutOpensSettings: "i" from the main list opens the client
// settings modal with the Identity input focused.
func TestIdentityShortcutOpensSettings(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if m.mode != modeSettings {
		t.Fatalf("want modeSettings after i, got %d", m.mode)
	}
	if !m.settings.onInput() {
		t.Errorf("settings should open with an input focused, focus = %d", m.settings.focus)
	}
}

// TestStartupOpensSettingsWhenIdentityUnset: withStartupSettings opens the modal
// only when no identity is configured; a configured identity starts on main.
func TestStartupOpensSettingsWhenIdentityUnset(t *testing.T) {
	unset := newModel("/tmp/x.yaml", testConfig()).withStartupSettings()
	if unset.mode != modeSettings {
		t.Fatalf("empty identity should auto-open settings, got mode %d", unset.mode)
	}

	cfg := testConfig()
	cfg.Identity = "alice@laptop"
	set := newModel("/tmp/x.yaml", cfg).withStartupSettings()
	if set.mode != modeMain {
		t.Fatalf("configured identity should start on main, got mode %d", set.mode)
	}
}

// TestSettingsSavePersistsIdentity: typing an identity and pressing enter writes
// it to disk, returns to main, and refreshes both the header text and the
// resolved lifecycle identity.
func TestSettingsSavePersistsIdentity(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := testConfig()
	if err := config.Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	m := newModel(p, cfg)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	// The input opens pre-filled with the default; replace it with a custom value.
	m.settings.inputs[0].SetValue("alice@laptop")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.mode != modeMain {
		t.Fatalf("want modeMain after save, got %d", m.mode)
	}
	if m.cfg.Identity != "alice@laptop" {
		t.Errorf("cfg identity = %q, want alice@laptop", m.cfg.Identity)
	}
	if m.identity != "alice@laptop" {
		t.Errorf("header identity = %q, want alice@laptop", m.identity)
	}
	if m.id.By != "alice@laptop" {
		t.Errorf("resolved id.By = %q, want alice@laptop", m.id.By)
	}
	saved, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Identity != "alice@laptop" {
		t.Errorf("persisted identity = %q, want alice@laptop", saved.Identity)
	}
}

// TestSettingsSaveViaButton: tab to Save and pressing enter also submits.
func TestSettingsSaveViaButton(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	m := newModel(p, testConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m.settings.inputs[0].SetValue("bob@nas")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // Identity -> Rsync path
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // Rsync path -> Save
	if m.settings.focus != m.settings.saveSlot() {
		t.Fatalf("tab from the last input should focus Save, got %d", m.settings.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMain {
		t.Fatalf("want modeMain after Save, got %d", m.mode)
	}
	if m.cfg.Identity != "bob@nas" {
		t.Errorf("cfg identity = %q, want bob@nas", m.cfg.Identity)
	}
}

// TestSettingsFocusNav: tab cycles inputs->Save->Cancel->input, and left/right
// toggle Save<->Cancel on the action row.
func TestSettingsFocusNav(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	for range m.settings.inputs {
		m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	}
	if m.settings.focus != m.settings.saveSlot() {
		t.Fatalf("tabbing past the inputs should focus Save, got %d", m.settings.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if m.settings.focus != m.settings.cancelSlot() {
		t.Fatalf("right from Save should focus Cancel, got %d", m.settings.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if m.settings.focus != m.settings.saveSlot() {
		t.Fatalf("left from Cancel should focus Save, got %d", m.settings.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // Save -> Cancel
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // Cancel -> input (wrap)
	if !m.settings.onInput() {
		t.Fatalf("tab should wrap back to the input, focus = %d", m.settings.focus)
	}
}

// TestSettingsEscCancels: esc closes the modal without writing anything.
func TestSettingsEscCancels(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	m := newModel(p, testConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ghost")})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeMain {
		t.Fatalf("esc should return to main, got %d", m.mode)
	}
	if m.cfg.Identity != "" {
		t.Errorf("esc must not apply edits, identity = %q", m.cfg.Identity)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("esc must not write a config file at %s (stat err = %v)", p, err)
	}
}

// TestSettingsSaveFailureKeepsModal: a save failure surfaces an error, keeps the
// modal open, and leaves the in-memory config unchanged (no divergence from disk).
func TestSettingsSaveFailureKeepsModal(t *testing.T) {
	p := failingSavePath(t)
	m := newModel(p, testConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("alice@laptop")})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.mode != modeSettings {
		t.Fatalf("failed save should keep the modal open, got mode %d", m.mode)
	}
	if m.settings.err == "" {
		t.Error("expected settings.err to be set after a failed save")
	}
	if m.cfg.Identity != "" {
		t.Errorf("failed save must restore identity to empty, got %q", m.cfg.Identity)
	}
}

// TestSettingsViewShowsTitleAndField guards the modal UI: it renders the title
// and the Identity label.
func TestSettingsViewShowsTitleAndField(t *testing.T) {
	s := newSettings(&config.Config{})
	s.setWidth(80)
	view := s.View()
	for _, want := range []string{"Client settings", "Identity", "[ Save ]", "[ Cancel ]"} {
		if !strings.Contains(view, want) {
			t.Errorf("settings view missing %q:\n%s", want, view)
		}
	}
}

// TestSettingsPrefillsDefaultIdentity: an unset identity opens the modal with the
// input pre-populated with the resolved $USER@$HOSTNAME default (not blank), so a
// single Save finalizes a valid config.
func TestSettingsPrefillsDefaultIdentity(t *testing.T) {
	cfg := &config.Config{}
	want, err := ident.Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := newSettings(cfg)
	if got := s.inputs[0].Value(); got != want.By {
		t.Errorf("Identity input = %q, want prefilled default %q", got, want.By)
	}
	if s.inputs[0].Value() == "" {
		t.Error("prefilled identity must not be empty")
	}
}

// TestSettingsSaveRejectsBlankIdentity: clearing the Identity and pressing Save is
// rejected with an inline error, keeps the modal open, and writes nothing.
func TestSettingsSaveRejectsBlankIdentity(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	m := newModel(p, testConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m.settings.inputs[0].SetValue("")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.mode != modeSettings {
		t.Fatalf("blank Save should keep the modal open, got mode %d", m.mode)
	}
	if m.settings.err == "" {
		t.Error("expected an inline error after a blank-identity Save")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("blank Save must not write a config file (stat err = %v)", err)
	}
}

// TestStartupSettingsEscQuits: the mandatory first-run dialog quits the app on esc
// rather than dropping into an unconfigured main view.
func TestStartupSettingsEscQuits(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig()).withStartupSettings()
	if m.mode != modeSettings || !m.settings.mandatory {
		t.Fatalf("want a mandatory settings modal, mode=%d mandatory=%v", m.mode, m.settings.mandatory)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected a command from esc on the mandatory modal")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected QuitMsg from esc on the mandatory modal")
	}
}

// TestStartupSettingsQuitButtonQuits: activating the Quit button (the mandatory
// dialog's relabelled Cancel) also exits the app.
func TestStartupSettingsQuitButtonQuits(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig()).withStartupSettings()
	for range m.settings.inputs {
		m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // through the inputs to Save
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}) // Save -> Quit
	if m.settings.focus != m.settings.cancelSlot() {
		t.Fatalf("want focus on the quit slot, got %d", m.settings.focus)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a command from the Quit button")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected QuitMsg from the Quit button")
	}
}

// TestStartupSettingsSaveClosesAndPersists: accepting the prefilled default on the
// mandatory dialog writes a concrete identity and lands on the main view, so a
// later launch no longer re-prompts.
func TestStartupSettingsSaveClosesAndPersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	m := newModel(p, testConfig()).withStartupSettings()
	if m.mode != modeSettings {
		t.Fatalf("want mandatory settings modal, got mode %d", m.mode)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // accept prefilled default

	if m.mode != modeMain {
		t.Fatalf("accepting the default should close to main, got mode %d", m.mode)
	}
	if m.cfg.Identity == "" {
		t.Error("save must persist a concrete (non-empty) identity")
	}
	saved, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Identity != m.cfg.Identity || saved.Identity == "" {
		t.Errorf("persisted identity = %q, want non-empty and equal to in-memory %q", saved.Identity, m.cfg.Identity)
	}
}

// TestRsyncCheckFailureOpensSettings: a failed startup rsync check must surface
// as the (dismissible) settings dialog carrying the check error, so the user can
// point the Rsync path override at a working binary.
func TestRsyncCheckFailureOpensSettings(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	m = update(t, m, rsyncCheckMsg{err: errFake})
	if m.mode != modeSettings {
		t.Fatalf("a failed rsync check should open settings, got mode %d", m.mode)
	}
	if m.settings.mandatory {
		t.Error("the rsync-check dialog must be dismissible, not mandatory")
	}
	if !strings.Contains(m.settings.err, errFake.Error()) {
		t.Errorf("settings.err = %q, want the check error", m.settings.err)
	}
}

// TestRsyncCheckFailureKeepsOpenSettings: when the mandatory first-run dialog is
// already open, a failed rsync check only adds its error — it must not replace
// the dialog and drop the mandatory flag (which would let esc skip identity setup).
func TestRsyncCheckFailureKeepsOpenSettings(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig()).withStartupSettings()
	m = update(t, m, rsyncCheckMsg{err: errFake})
	if m.mode != modeSettings || !m.settings.mandatory {
		t.Fatalf("mandatory dialog must survive the check, mode=%d mandatory=%v", m.mode, m.settings.mandatory)
	}
	if !strings.Contains(m.settings.err, errFake.Error()) {
		t.Errorf("settings.err = %q, want the check error", m.settings.err)
	}
}

// TestRsyncCheckPassLeavesMainAlone: a passing check must not touch the UI.
func TestRsyncCheckPassLeavesMainAlone(t *testing.T) {
	m := newModel("/tmp/x.yaml", testConfig())
	m = update(t, m, rsyncCheckMsg{err: nil})
	if m.mode != modeMain {
		t.Fatalf("a passing rsync check must not open settings, got mode %d", m.mode)
	}
}

// TestSettingsSaveRejectsBadRsyncPath: a non-empty rsync path that is not a
// usable GNU rsync is rejected inline and never persisted.
func TestSettingsSaveRejectsBadRsyncPath(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	m := newModel(p, testConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m.settings.inputs[0].SetValue("alice@laptop")
	m.settings.inputs[1].SetValue(filepath.Join(t.TempDir(), "no-such-rsync"))
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.mode != modeSettings {
		t.Fatalf("a bad rsync path should keep the modal open, got mode %d", m.mode)
	}
	if m.settings.err == "" {
		t.Error("expected an inline error for an unusable rsync path")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("a rejected save must not write a config file (stat err = %v)", err)
	}
}

// TestSettingsSavePersistsRsyncPath: saving with a working rsync override writes
// rsync_path to disk and rewires the lifecycle runner to use it.
func TestSettingsSavePersistsRsyncPath(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not on PATH")
	}
	rsyncAbs, err := exec.LookPath("rsync")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "config.yaml")
	m := newModel(p, testConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m.settings.inputs[0].SetValue("alice@laptop")
	m.settings.inputs[1].SetValue(rsyncAbs)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.mode != modeMain {
		t.Fatalf("want modeMain after save, got %d (err %q)", m.mode, m.settings.err)
	}
	if m.runner.RsyncBin != rsyncAbs {
		t.Errorf("runner.RsyncBin = %q, want %q", m.runner.RsyncBin, rsyncAbs)
	}
	saved, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RsyncPath != rsyncAbs {
		t.Errorf("persisted rsync_path = %q, want %q", saved.RsyncPath, rsyncAbs)
	}
}

// TestSettingsMandatoryViewShowsQuit: the mandatory dialog labels its exit button
// "Quit" instead of "Cancel".
func TestSettingsMandatoryViewShowsQuit(t *testing.T) {
	s := newSettings(&config.Config{})
	s.mandatory = true
	s.setWidth(80)
	view := s.View()
	if !strings.Contains(view, "[ Quit ]") {
		t.Errorf("mandatory view should show a Quit button:\n%s", view)
	}
	if strings.Contains(view, "[ Cancel ]") {
		t.Errorf("mandatory view should not show Cancel:\n%s", view)
	}
}
