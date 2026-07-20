package config

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Profile is a named pair of roots: one on fast local disk, one on the network share.
// RemoteRoot is a plain absolute path (a mounted share), or an "ssh://[user@]host[:port]/abs/path"
// or "rsync://[user@]host[:port]/module[/path]" endpoint URL (see RemoteEndpoint).
// Subpaths, when non-empty, scope the profile to only those relative paths under both
// roots; an empty list means the whole root.
type Profile struct {
	// ID is the profile's stable unique identifier (a UUID assigned when the
	// profile is created). It is recorded in checkout markers so lock ownership
	// is per-profile, not per-machine: two profiles on the same machine pointing
	// at the same remote root must not pass each other's ownership check.
	ID         string `yaml:"id,omitempty"`
	LocalRoot  string `yaml:"local_root"`
	RemoteRoot string `yaml:"remote_root"`
	// SSHIdentityFile, for an ssh:// remote, is the private key handed to ssh -i.
	SSHIdentityFile string `yaml:"ssh_identity_file,omitempty"`
	// RsyncdPasswordFile, for an rsync:// remote, is handed to rsync --password-file.
	RsyncdPasswordFile string `yaml:"rsyncd_password_file,omitempty"`
	// Subpaths is an intentional hard scope, not a live view of the remote: a folder
	// appearing on the remote outside this list is deliberately ignored — scoping means
	// "only these folders", never "these plus whatever shows up". Only the local side is
	// guarded (sanity.UnlistedLocal; lifecycle refuses to sync past local content outside
	// the scope), because unlisted local content risks stranding work, while unlisted
	// remote content simply stays remote until the user widens this list.
	Subpaths []string `yaml:"subpaths,omitempty"`
	// Ignore lists slash-free glob patterns (path.Match) for filenames the sync
	// never touches: a path any of whose segments matches a pattern is neither
	// pulled, pushed, nor deleted — it is reported under its own "ignored"
	// category. Meant for filesystem metadata droppings (.DS_Store, .directory).
	Ignore []string `yaml:"ignore,omitempty"`
}

// DefaultIgnore is the ignore list seeded into newly created profiles: the
// metadata files macOS Finder (.DS_Store) and KDE Dolphin (.directory) drop
// into any folder they display — including a freshly mounted share.
func DefaultIgnore() []string { return []string{".DS_Store", ".directory"} }

// NewProfileID returns a fresh profile identifier: a random UUID v4.
func NewProfileID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])      // crypto/rand.Read never returns an error
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Config is the on-disk configuration: an identity string and named profiles.
type Config struct {
	Identity string `yaml:"identity,omitempty"`
	// RsyncPath overrides the rsync binary used for all transfers; empty means "rsync"
	// from PATH. Useful on macOS, where /usr/bin/rsync is Apple's openrsync.
	RsyncPath string             `yaml:"rsync_path,omitempty"`
	Profiles  map[string]Profile `yaml:"profiles"`
}

// Load reads the YAML config at path. A missing file yields an empty config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: config path is user-supplied via --config/$DIBS_CONFIG by design; no trust boundary is crossed.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{Profiles: map[string]Profile{}}, nil
		}
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	return &cfg, nil
}

// Save writes cfg to path atomically (temp file + rename). Parent directories are
// created with mode 0700 and the resulting file has mode 0600.
func Save(path string, cfg *Config) error {
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".dibs-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// DefaultPath returns the config file location: $DIBS_CONFIG if set,
// otherwise the OS config directory + dibs/config.yaml.
func DefaultPath() (string, error) {
	if p := os.Getenv("DIBS_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "dibs", "config.yaml"), nil
}

// ExpandRoot expands environment variables and a leading ~ in a root path.
func ExpandRoot(root string) string {
	expanded := os.ExpandEnv(root)
	if expanded == "~" || strings.HasPrefix(expanded, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			expanded = filepath.Join(home, strings.TrimPrefix(expanded, "~"))
		}
	}
	return expanded
}

// ValidateName reports whether a profile name is usable.
func ValidateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("profile name is required")
	}
	return nil
}

// ValidateIdentity reports whether a client identity is usable: a concrete,
// non-empty "who" is required to record checkout markers.
func ValidateIdentity(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("identity is required")
	}
	return nil
}

// ValidateRoot reports whether a root path is usable: non-empty and absolute
// once ~ and environment variables are expanded.
func ValidateRoot(root string) error {
	if strings.TrimSpace(root) == "" {
		return errors.New("root path is required")
	}
	if !filepath.IsAbs(ExpandRoot(root)) {
		return errors.New("root path must be absolute")
	}
	return nil
}

// ValidateSubpath reports whether a profile subpath is usable: a non-empty, relative
// path that does not escape the root once cleaned. A leading "./" is allowed.
func ValidateSubpath(sub string) error {
	if strings.TrimSpace(sub) == "" {
		return errors.New("subpath is required")
	}
	if filepath.IsAbs(sub) {
		return errors.New("subpath must be relative")
	}
	clean := filepath.Clean(sub)
	if clean == "." {
		return errors.New("subpath must not be the root itself")
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return errors.New("subpath must not escape the root")
	}
	return nil
}

// ValidateIgnorePattern reports whether an ignore pattern is usable: non-blank,
// free of slashes (patterns match single path segments, so a slash could never
// match), and a valid path.Match glob.
func ValidateIgnorePattern(pat string) error {
	p := strings.TrimSpace(pat)
	if p == "" {
		return errors.New("ignore pattern is required")
	}
	if strings.Contains(p, "/") {
		return errors.New("ignore pattern must not contain a slash (it matches a single file or folder name)")
	}
	if _, err := path.Match(p, "x"); err != nil {
		return errors.New("ignore pattern is not a valid glob")
	}
	return nil
}

// MatchesIgnoreName reports whether a single path segment (a file or directory
// name) matches any of the profile's ignore patterns. Invalid patterns never
// match — validation happens at edit time (ValidateIgnorePattern). The
// path-level matching (any segment ignores the subtree) lives in the sync
// engine; this name-level helper serves the local walkers (sanity, localstat).
func MatchesIgnoreName(name string, patterns []string) bool {
	for _, pat := range patterns {
		if ok, _ := path.Match(strings.TrimSpace(pat), name); ok {
			return true
		}
	}
	return false
}

// Target is a concrete pair of absolute paths a profile action operates on. Subpath is
// the relative path under the roots ("" for the whole root).
type Target struct {
	Subpath string
	Local   string
	Remote  string
}

// Targets resolves a profile into the concrete local/remote path pairs to act on. With no
// subpaths it returns a single target for the whole (expanded) root; otherwise one target
// per validated subpath, joined onto each expanded root. Order is preserved.
func (p Profile) Targets() ([]Target, error) {
	localRoot := ExpandRoot(p.LocalRoot)
	remoteRoot := ExpandRoot(p.RemoteRoot)
	if len(p.Subpaths) == 0 {
		return []Target{{Local: localRoot, Remote: remoteRoot}}, nil
	}
	out := make([]Target, 0, len(p.Subpaths))
	for _, sub := range p.Subpaths {
		if err := ValidateSubpath(sub); err != nil {
			return nil, fmt.Errorf("subpath %q: %w", sub, err)
		}
		out = append(out, Target{
			Subpath: sub,
			Local:   filepath.Join(localRoot, sub),
			Remote:  filepath.Join(remoteRoot, sub),
		})
	}
	return out, nil
}
