package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andresbott/netcheckout/internal/config"
	"github.com/spf13/cobra"
)

func TestRunRootNonInteractivePrintsList(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(p, &config.Config{Profiles: map[string]config.Profile{
		"work": {LocalRoot: "/l", RemoteRoot: "/r"},
	}}); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := runRoot(cmd, p, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "work") {
		t.Fatalf("expected plain list, got:\n%s", buf.String())
	}
}

func TestRunRootNonInteractiveEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := runRoot(cmd, p, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "No profiles configured") {
		t.Fatalf("expected empty-state message, got:\n%s", buf.String())
	}
}
