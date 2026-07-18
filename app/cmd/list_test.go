package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andresbott/netcheckout/internal/config"
)

func TestListCommandPrintsProfiles(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(p, &config.Config{Profiles: map[string]config.Profile{
		"work": {LocalRoot: "/home/me/work", RemoteRoot: "/mnt/nas/work"},
	}}); err != nil {
		t.Fatal(err)
	}

	root := newRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"list", "--config", p})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "work") || !strings.Contains(out, "/mnt/nas/work") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestResolvePath(t *testing.T) {
	if got, _ := resolvePath("/explicit.yaml"); got != "/explicit.yaml" {
		t.Errorf("flag path: got %q", got)
	}
	t.Setenv("NETCHECKOUT_CONFIG", "/env.yaml")
	if got, _ := resolvePath(""); got != "/env.yaml" {
		t.Errorf("env path: got %q", got)
	}
}
