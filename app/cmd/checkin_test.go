package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andresbott/netcheckout/internal/config"
	"github.com/andresbott/netcheckout/internal/lifecycle"
	"github.com/andresbott/netcheckout/internal/marker"
)

func TestCheckinCommandReleases(t *testing.T) {
	cfgPath, remote := heldCmdFixture(t) // in sync: local == remote == baseline

	cmd := newCheckinCmdWithRunner(&cfgPath, lifecycle.Runner{ToolVersion: "test"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"work"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := marker.Read(remote); ok {
		t.Error("checkin must remove the marker")
	}
}

func TestCheckinCommandRefusesUnsynced(t *testing.T) {
	cfgPath, remote := heldCmdFixture(t)
	cfg, _ := config.Load(cfgPath)
	lroot := cfg.Profiles["work"].LocalRoot
	_ = os.WriteFile(filepath.Join(lroot, "keep.txt"), []byte("EDITED"), 0o644) // unsynced local edit

	cmd := newCheckinCmdWithRunner(&cfgPath, lifecycle.Runner{ToolVersion: "test"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"work"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("checkin must fail when the profile has unsynced changes")
	}
	if _, ok, _ := marker.Read(remote); !ok {
		t.Error("a refused checkin must leave the marker in place")
	}
	if !strings.Contains(buf.String(), "keep.txt") {
		t.Errorf("checkin should list the pending change, got:\n%s", buf.String())
	}
}

func TestCheckinRegisteredOnRoot(t *testing.T) {
	for _, c := range newRootCommand().Commands() {
		if c.Name() == "checkin" {
			return
		}
	}
	t.Fatal("checkin not registered")
}
