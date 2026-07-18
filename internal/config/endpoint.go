package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/andresbott/netcheckout/libs/threewayrsync"
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
