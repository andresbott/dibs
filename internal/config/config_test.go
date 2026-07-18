package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/andresbott/netcheckout/internal/config"
)

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil || cfg.Profiles == nil || len(cfg.Profiles) != 0 {
		t.Fatalf("want empty non-nil config, got %#v", cfg)
	}
}

func TestLoadParsesProfiles(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	data := "identity: me@host\nprofiles:\n  photos:\n    local_root: /home/me/pics\n    remote_root: /mnt/nas/pics\n"
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Identity != "me@host" {
		t.Errorf("identity = %q", cfg.Identity)
	}
	got := cfg.Profiles["photos"]
	if got.LocalRoot != "/home/me/pics" || got.RemoteRoot != "/mnt/nas/pics" {
		t.Errorf("photos = %#v", got)
	}
}

func TestLoadMalformedYAMLErrors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("profiles: [unterminated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(p); err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

func TestLoadValidNoProfilesKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("identity: me@host\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profiles == nil {
		t.Fatal("Profiles should be non-nil after loading YAML without a profiles key")
	}
	if len(cfg.Profiles) != 0 {
		t.Fatalf("Profiles should be empty, got %d entries", len(cfg.Profiles))
	}
}

func TestSaveRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nested", "config.yaml")
	in := &config.Config{
		Identity: "me@host",
		Profiles: map[string]config.Profile{
			"work": {LocalRoot: "/home/me/work", RemoteRoot: "/mnt/nas/work"},
		},
	}
	if err := config.Save(p, in); err != nil {
		t.Fatal(err)
	}
	out, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if out.Identity != in.Identity || !reflect.DeepEqual(out.Profiles["work"], in.Profiles["work"]) {
		t.Fatalf("round trip mismatch: %#v", out)
	}
}

func TestSaveRoundTripSubpaths(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	in := &config.Config{
		Profiles: map[string]config.Profile{
			"work": {
				LocalRoot:  "/home/me/work",
				RemoteRoot: "/mnt/nas/work",
				Subpaths:   []string{"reports", "notes/2024"},
			},
		},
	}
	if err := config.Save(p, in); err != nil {
		t.Fatal(err)
	}
	out, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.Profiles["work"], in.Profiles["work"]) {
		t.Fatalf("round trip mismatch:\n got %#v\nwant %#v", out.Profiles["work"], in.Profiles["work"])
	}
}

func TestSaveOmitsEmptySubpaths(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	in := &config.Config{Profiles: map[string]config.Profile{
		"work": {LocalRoot: "/l", RemoteRoot: "/r"},
	}}
	if err := config.Save(p, in); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "subpaths") {
		t.Fatalf("empty subpaths must be omitted from YAML, got:\n%s", data)
	}
}

func TestSaveFileMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(p, &config.Config{Profiles: map[string]config.Profile{}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestDefaultPathEnvOverride(t *testing.T) {
	t.Setenv("NETCHECKOUT_CONFIG", "/custom/path.yaml")
	got, err := config.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/custom/path.yaml" {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultPathFallback(t *testing.T) {
	t.Setenv("NETCHECKOUT_CONFIG", "")
	got, err := config.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("netcheckout", "config.yaml")
	if !strings.HasSuffix(got, want) {
		t.Fatalf("got %q, want suffix %q", got, want)
	}
}

func TestValidateName(t *testing.T) {
	if err := config.ValidateName(""); err == nil {
		t.Error("empty name should be invalid")
	}
	if err := config.ValidateName("  "); err == nil {
		t.Error("blank name should be invalid")
	}
	if err := config.ValidateName("work"); err != nil {
		t.Errorf("valid name rejected: %v", err)
	}
}

func TestExpandRoot(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	if got := config.ExpandRoot("~/pics"); got != "/home/tester/pics" {
		t.Errorf("tilde: got %q", got)
	}
	if got := config.ExpandRoot("$HOME/pics"); got != "/home/tester/pics" {
		t.Errorf("env: got %q", got)
	}
	if got := config.ExpandRoot("/abs/path"); got != "/abs/path" {
		t.Errorf("abs: got %q", got)
	}
}

func TestValidateRoot(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	if err := config.ValidateRoot(""); err == nil {
		t.Error("empty root should be invalid")
	}
	if err := config.ValidateRoot("relative/path"); err == nil {
		t.Error("relative root should be invalid")
	}
	if err := config.ValidateRoot("/mnt/nas"); err != nil {
		t.Errorf("absolute root rejected: %v", err)
	}
	if err := config.ValidateRoot("~/work"); err != nil {
		t.Errorf("tilde root rejected: %v", err)
	}
}

func TestValidateSubpath(t *testing.T) {
	valid := []string{"a", "a/2024", "nested/deep/path", "./2025/jan", "a/./b"}
	for _, s := range valid {
		if err := config.ValidateSubpath(s); err != nil {
			t.Errorf("ValidateSubpath(%q) = %v, want nil", s, err)
		}
	}
	invalid := []string{"", "   ", "/abs", "/abs/path", "..", "../escape", "a/../../b", "."}
	for _, s := range invalid {
		if err := config.ValidateSubpath(s); err == nil {
			t.Errorf("ValidateSubpath(%q) = nil, want error", s)
		}
	}
}

func TestTargetsWholeRoot(t *testing.T) {
	p := config.Profile{LocalRoot: "/l", RemoteRoot: "/r"}
	got, err := p.Targets()
	if err != nil {
		t.Fatal(err)
	}
	want := []config.Target{{Local: "/l", Remote: "/r"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestTargetsSubpaths(t *testing.T) {
	p := config.Profile{LocalRoot: "/l", RemoteRoot: "/r", Subpaths: []string{"a", "b/c"}}
	got, err := p.Targets()
	if err != nil {
		t.Fatal(err)
	}
	want := []config.Target{
		{Subpath: "a", Local: "/l/a", Remote: "/r/a"},
		{Subpath: "b/c", Local: "/l/b/c", Remote: "/r/b/c"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestTargetsExpandsRoots(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	p := config.Profile{LocalRoot: "~/work", RemoteRoot: "$HOME/nas", Subpaths: []string{"a"}}
	got, err := p.Targets()
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Local != "/home/tester/work/a" {
		t.Errorf("local = %q, want /home/tester/work/a", got[0].Local)
	}
	if got[0].Remote != "/home/tester/nas/a" {
		t.Errorf("remote = %q, want /home/tester/nas/a", got[0].Remote)
	}
}

func TestTargetsInvalidSubpath(t *testing.T) {
	p := config.Profile{LocalRoot: "/l", RemoteRoot: "/r", Subpaths: []string{"../escape"}}
	if _, err := p.Targets(); err == nil {
		t.Fatal("expected error for escaping subpath, got nil")
	}
}

func TestSampleConfigResolves(t *testing.T) {
	cfg, err := config.Load(filepath.Join("..", "..", "zarf", "sample", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	work, ok := cfg.Profiles["work"]
	if !ok {
		t.Fatal("sample config missing 'work' profile")
	}
	if len(work.Subpaths) == 0 {
		t.Fatal("sample 'work' profile should declare subpaths")
	}
	targets, err := work.Targets()
	if err != nil {
		t.Fatalf("Targets() on sample work profile: %v", err)
	}
	if len(targets) != len(work.Subpaths) {
		t.Fatalf("got %d targets, want %d", len(targets), len(work.Subpaths))
	}

	// Every profile in the sample must resolve to at least one target
	// (whole-root profiles resolve to one; subpath profiles to one per subpath).
	for name, p := range cfg.Profiles {
		got, err := p.Targets()
		if err != nil {
			t.Fatalf("Targets() on sample %q profile: %v", name, err)
		}
		if len(got) == 0 {
			t.Fatalf("sample %q profile resolved to no targets", name)
		}
	}
}
