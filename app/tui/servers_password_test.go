package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andresbott/dibs/internal/config"
)

// TestSubmitServerWritesPasswordFile: typing a password on a new server writes
// it to the default managed location beside the config and points PasswordFile
// there, without persisting the secret in the config.
func TestSubmitServerWritesPasswordFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{Servers: map[string]config.Server{}, Profiles: map[string]config.Profile{}}
	m := newModel(path, cfg)
	m.serverForm = newServerForm("", config.Server{}, path)
	setServerFormFields(&m.serverForm, "nas", "nas.local", "", "bob", "hunter2", "")

	if _, _ = m.submitServer(); m.serverForm.err != "" {
		t.Fatalf("submit error: %s", m.serverForm.err)
	}

	pwPath := filepath.Join(dir, "nas.pw")
	data, err := os.ReadFile(pwPath)
	if err != nil {
		t.Fatalf("password file not written: %v", err)
	}
	if string(data) != "hunter2\n" {
		t.Fatalf("password file content = %q", string(data))
	}
	if got := cfg.Servers["nas"].PasswordFile; got != pwPath {
		t.Fatalf("PasswordFile = %q, want %q", got, pwPath)
	}
}

// TestSubmitServerBlankPasswordKeepsFile: editing with a blank password does not
// touch the existing password file.
func TestSubmitServerBlankPasswordKeepsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	pwPath := config.ServerPasswordPath(path, "nas")
	if err := config.WriteServerPassword(pwPath, "original"); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Servers:  map[string]config.Server{"nas": {Host: "h", PasswordFile: pwPath}},
		Profiles: map[string]config.Profile{},
	}
	m := newModel(path, cfg)
	m.serverForm = newServerForm("nas", cfg.Servers["nas"], path)
	// Re-set fields without touching the (blank) password field.
	setServerFormFields(&m.serverForm, "nas", "h", "", "", "", pwPath)

	if _, _ = m.submitServer(); m.serverForm.err != "" {
		t.Fatalf("submit error: %s", m.serverForm.err)
	}
	data, err := os.ReadFile(pwPath)
	if err != nil || string(data) != "original\n" {
		t.Fatalf("password file changed: data=%q err=%v", string(data), err)
	}
}

// TestDeleteServerRemovesManagedFile: deleting a server removes its managed
// password file but leaves a bring-your-own file untouched.
func TestDeleteServerRemovesManagedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	managed := config.ServerPasswordPath(path, "nas")
	if err := config.WriteServerPassword(managed, "sec"); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(dir, "custom.pw")
	if err := config.WriteServerPassword(custom, "sec"); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Servers: map[string]config.Server{
			"nas": {Host: "h", PasswordFile: managed},
			"byo": {Host: "h", PasswordFile: custom},
		},
		Profiles: map[string]config.Profile{},
	}
	m := newModel(path, cfg)

	m.confirmName = "nas"
	if _, _ = m.deleteConfirmedServer(); m.err != nil {
		t.Fatalf("delete error: %v", m.err)
	}
	if _, err := os.Stat(managed); !os.IsNotExist(err) {
		t.Fatal("managed password file was not removed")
	}

	m.confirmName = "byo"
	if _, _ = m.deleteConfirmedServer(); m.err != nil {
		t.Fatalf("delete error: %v", m.err)
	}
	if _, err := os.Stat(custom); err != nil {
		t.Fatalf("bring-your-own password file should survive delete: %v", err)
	}
}

// TestRenameServerMovesManagedFile: renaming a server whose managed file the
// user did not redirect moves the file to the new name's managed location.
func TestRenameServerMovesManagedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	oldPw := config.ServerPasswordPath(path, "nas")
	if err := config.WriteServerPassword(oldPw, "sec"); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Servers:  map[string]config.Server{"nas": {Host: "h", PasswordFile: oldPw}},
		Profiles: map[string]config.Profile{},
	}
	m := newModel(path, cfg)
	m.serverForm = newServerForm("nas", cfg.Servers["nas"], path)
	// Rename to "backup"; blank password; path field still shows the old managed path.
	setServerFormFields(&m.serverForm, "backup", "h", "", "", "", oldPw)

	if _, _ = m.submitServer(); m.serverForm.err != "" {
		t.Fatalf("submit error: %s", m.serverForm.err)
	}

	newPw := config.ServerPasswordPath(path, "backup")
	if _, err := os.Stat(oldPw); !os.IsNotExist(err) {
		t.Fatal("old managed file was not moved")
	}
	data, err := os.ReadFile(newPw)
	if err != nil || string(data) != "sec\n" {
		t.Fatalf("moved file content = %q err=%v", string(data), err)
	}
	if got := cfg.Servers["backup"].PasswordFile; got != newPw {
		t.Fatalf("PasswordFile = %q, want %q", got, newPw)
	}
	if _, ok := cfg.Servers["nas"]; ok {
		t.Fatal("old server name still present after rename")
	}
}
