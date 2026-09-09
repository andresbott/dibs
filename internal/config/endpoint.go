package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/candy-tools/dibs/libs/threewayrsync"
)

// RemoteIsLocalPath reports whether the profile's remote root is a plain filesystem path
// (a mounted share) rather than an ssh:// or rsync:// endpoint. Stat-based checks (mount
// probing, marker reads, subpath existence) only apply when this is true.
func (p Profile) RemoteIsLocalPath() bool {
	return !strings.HasPrefix(p.RemoteRoot, "ssh://") && !strings.HasPrefix(p.RemoteRoot, "rsync://")
}

// RemoteEndpoint resolves the profile's remote root into a threewayrsync endpoint. Three
// syntaxes are recognized: a plain absolute path (a mounted share), "ssh://[user@]host[:port]/abs/path",
// and "rsync://[user@]host[:port]/module[/path]". The optional ssh_identity_file and
// rsyncd_password_file profile keys feed the corresponding transport's auth setting.
func (p Profile) RemoteEndpoint() (threewayrsync.Endpoint, error) {
	switch {
	case p.Server != "":
		return threewayrsync.Endpoint{}, fmt.Errorf("profile still references server %q — resolve it via Config.ResolveProfile before use", p.Server)
	case strings.HasPrefix(p.RemoteRoot, "ssh://"):
		return p.sshEndpoint()
	case strings.HasPrefix(p.RemoteRoot, "rsync://"):
		return p.daemonEndpoint()
	default:
		return threewayrsync.Endpoint{Path: ExpandRoot(p.RemoteRoot)}, nil
	}
}

// LocalEndpoint resolves the profile's local root into a filesystem endpoint.
func (p Profile) LocalEndpoint() threewayrsync.Endpoint {
	return threewayrsync.Endpoint{Path: ExpandRoot(p.LocalRoot)}
}

// parseRemoteURL parses one of the two remote URL forms, returning the host, optional
// port, optional user, and the slash-trimmed path inside it.
func parseRemoteURL(raw string) (host string, port int, user, path string, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", 0, "", "", fmt.Errorf("remote root %q: %w", raw, err)
	}
	if u.Hostname() == "" {
		return "", 0, "", "", fmt.Errorf("remote root %q: host is required", raw)
	}
	if ps := u.Port(); ps != "" {
		port, err = strconv.Atoi(ps)
		if err != nil {
			return "", 0, "", "", fmt.Errorf("remote root %q: bad port %q", raw, ps)
		}
	}
	if u.User != nil {
		if _, hasPw := u.User.Password(); hasPw {
			return "", 0, "", "", fmt.Errorf("remote root %q: a password in the URL is not supported (use %s)", raw, authKeyFor(u.Scheme))
		}
		user = u.User.Username()
	}
	return u.Hostname(), port, user, strings.Trim(u.Path, "/"), nil
}

func authKeyFor(scheme string) string {
	if scheme == "rsync" {
		return "rsyncd_password_file"
	}
	return "ssh_identity_file"
}

func (p Profile) sshEndpoint() (threewayrsync.Endpoint, error) {
	host, port, user, path, err := parseRemoteURL(p.RemoteRoot)
	if err != nil {
		return threewayrsync.Endpoint{}, err
	}
	if path == "" {
		return threewayrsync.Endpoint{}, fmt.Errorf("remote root %q: an ssh remote needs an absolute path (ssh://host/abs/path)", p.RemoteRoot)
	}
	return threewayrsync.Endpoint{
		Path: "/" + path,
		SSH: &threewayrsync.SSH{
			User:         user,
			Host:         host,
			Port:         port,
			IdentityFile: ExpandRoot(p.SSHIdentityFile),
		},
	}, nil
}

func (p Profile) daemonEndpoint() (threewayrsync.Endpoint, error) {
	host, port, user, path, err := parseRemoteURL(p.RemoteRoot)
	if err != nil {
		return threewayrsync.Endpoint{}, err
	}
	if path == "" {
		return threewayrsync.Endpoint{}, fmt.Errorf("remote root %q: an rsync daemon remote needs a module (rsync://host/module[/path])", p.RemoteRoot)
	}
	module, rest, _ := strings.Cut(path, "/")
	return threewayrsync.Endpoint{
		Path: rest,
		Daemon: &threewayrsync.Daemon{
			Host:         host,
			Port:         port,
			User:         user,
			Module:       module,
			PasswordFile: ExpandRoot(p.RsyncdPasswordFile),
		},
	}, nil
}

// RemoteParts is a remote root decomposed for form editing: the kind ("local", "ssh",
// "rsync") plus the fields that kind carries. For kind "rsync", ModulePath is
// "module[/path]"; for "ssh", Path is the absolute remote path; for "local", Path is the
// raw value.
type RemoteParts struct {
	Kind       string
	User, Host string
	Port       int
	ModulePath string // rsync: "module[/path]"
	Path       string // ssh: "/abs/path"; local: the raw value
}

// SplitRemoteRoot decomposes a remote root into its editable parts — the inverse of
// BuildRsyncRemoteRoot for the rsync kind. A malformed URL returns an error along with
// the detected kind so a caller can still route the raw value to the right field.
func SplitRemoteRoot(root string) (RemoteParts, error) {
	switch {
	case strings.HasPrefix(root, "rsync://"):
		host, port, user, path, err := parseRemoteURL(root)
		if err != nil {
			return RemoteParts{Kind: "rsync"}, err
		}
		return RemoteParts{Kind: "rsync", User: user, Host: host, Port: port, ModulePath: path}, nil
	case strings.HasPrefix(root, "ssh://"):
		host, port, user, path, err := parseRemoteURL(root)
		if err != nil {
			return RemoteParts{Kind: "ssh"}, err
		}
		return RemoteParts{Kind: "ssh", User: user, Host: host, Port: port, Path: "/" + path}, nil
	default:
		return RemoteParts{Kind: "local", Path: root}, nil
	}
}

// BuildRsyncRemoteRoot renders "rsync://[user@]host[:port]/modulePath", omitting the
// user when empty and the port when blank. port is a raw string (a form field value):
// a non-numeric port composes a URL that ValidateRemoteRoot rejects with its clear
// "bad port" message, so no separate validation path is needed.
func BuildRsyncRemoteRoot(user, host, port, modulePath string) string {
	h := strings.TrimSpace(host)
	if u := strings.TrimSpace(user); u != "" {
		h = u + "@" + h
	}
	if p := strings.TrimSpace(port); p != "" {
		h += ":" + p
	}
	return "rsync://" + h + "/" + strings.Trim(strings.TrimSpace(modulePath), "/")
}

// ValidateRemoteRoot reports whether a remote root is usable: a plain absolute path (after
// ~ and env expansion), or a well-formed ssh:// or rsync:// endpoint URL.
func ValidateRemoteRoot(root string) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("root path is required")
	}
	p := Profile{RemoteRoot: root}
	if !p.RemoteIsLocalPath() {
		_, err := p.RemoteEndpoint()
		return err
	}
	return ValidateRoot(root)
}

// ResolveProfile returns the named profile with its server reference resolved
// into a self-contained Profile, ready for sanity/status/lifecycle. It reads
// the stored profile, so an rsync:// value in RemoteRoot with no Server is
// unambiguously the old embedded shape and is rejected.
func (c *Config) ResolveProfile(name string) (Profile, error) {
	p, ok := c.Profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("profile %q not found", name)
	}
	if p.Server == "" && strings.HasPrefix(p.RemoteRoot, "rsync://") {
		return Profile{}, fmt.Errorf("profile %q uses the old embedded rsync remote; rsync connections are now configured as servers — recreate this profile", name)
	}
	return c.resolve(p)
}

// resolve resolves a profile's server reference. A profile with no Server (a
// local-mount or ssh:// remote) passes through unchanged. Otherwise the named
// server supplies the connection: RemoteRoot is composed as the daemon URL and
// RsyncdPasswordFile is copied from the server, and Server is cleared so the
// result is a plain rsync:// profile the rest of the code already understands.
func (c *Config) resolve(p Profile) (Profile, error) {
	if p.Server == "" {
		return p, nil
	}
	srv, ok := c.Servers[p.Server]
	if !ok {
		return Profile{}, fmt.Errorf("profile references unknown server %q", p.Server)
	}
	port := ""
	if srv.Port != 0 {
		port = strconv.Itoa(srv.Port)
	}
	p.RemoteRoot = BuildRsyncRemoteRoot(srv.User, srv.Host, port, p.RemoteModule)
	p.RsyncdPasswordFile = srv.PasswordFile
	p.Server = ""
	return p, nil
}
