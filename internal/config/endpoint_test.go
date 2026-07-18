package config

import (
	"strings"
	"testing"
)

func TestRemoteEndpointLocalPath(t *testing.T) {
	p := Profile{RemoteRoot: "/mnt/share/data"}
	if !p.RemoteIsLocalPath() {
		t.Fatal("plain path must be local")
	}
	e, err := p.RemoteEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	if e.Path != "/mnt/share/data" || e.SSH != nil || e.Daemon != nil {
		t.Errorf("endpoint = %+v", e)
	}
}

func TestRemoteEndpointSSH(t *testing.T) {
	p := Profile{RemoteRoot: "ssh://alice@nas:2222/srv/data", SSHIdentityFile: "/home/a/.ssh/id"}
	if p.RemoteIsLocalPath() {
		t.Fatal("ssh URL must not be local")
	}
	e, err := p.RemoteEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	if e.SSH == nil || e.Path != "/srv/data" {
		t.Fatalf("endpoint = %+v", e)
	}
	if e.SSH.User != "alice" || e.SSH.Host != "nas" || e.SSH.Port != 2222 || e.SSH.IdentityFile != "/home/a/.ssh/id" {
		t.Errorf("ssh = %+v", *e.SSH)
	}
}

func TestRemoteEndpointSSHDefaults(t *testing.T) {
	e, err := Profile{RemoteRoot: "ssh://nas/srv/data"}.RemoteEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	if e.SSH.User != "" || e.SSH.Port != 0 || e.SSH.Host != "nas" {
		t.Errorf("ssh = %+v", *e.SSH)
	}
}

func TestRemoteEndpointDaemon(t *testing.T) {
	p := Profile{RemoteRoot: "rsync://bob@nas:8730/backup/photos/2025", RsyncdPasswordFile: "/home/b/.rsyncpw"}
	e, err := p.RemoteEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	if e.Daemon == nil {
		t.Fatalf("endpoint = %+v", e)
	}
	d := *e.Daemon
	if d.Host != "nas" || d.Port != 8730 || d.User != "bob" || d.Module != "backup" || d.PasswordFile != "/home/b/.rsyncpw" {
		t.Errorf("daemon = %+v", d)
	}
	if e.Path != "photos/2025" {
		t.Errorf("path inside module = %q", e.Path)
	}
}

func TestRemoteEndpointDaemonModuleRoot(t *testing.T) {
	e, err := Profile{RemoteRoot: "rsync://nas/backup"}.RemoteEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	if e.Daemon.Module != "backup" || e.Path != "" {
		t.Errorf("endpoint = %+v daemon = %+v", e, *e.Daemon)
	}
}

func TestRemoteEndpointErrors(t *testing.T) {
	cases := map[string]string{
		"ssh://nas":               "absolute path",
		"ssh:///srv/data":         "host is required",
		"rsync://nas":             "module",
		"ssh://u:pw@nas/srv/data": "password",
	}
	for root, want := range cases {
		_, err := Profile{RemoteRoot: root}.RemoteEndpoint()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("RemoteEndpoint(%q) err = %v, want containing %q", root, err, want)
		}
	}
}

func TestValidateRemoteRoot(t *testing.T) {
	for _, ok := range []string{"/mnt/share", "ssh://nas/srv/data", "rsync://nas/mod"} {
		if err := ValidateRemoteRoot(ok); err != nil {
			t.Errorf("ValidateRemoteRoot(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "relative/path", "ssh://nas", "rsync://"} {
		if err := ValidateRemoteRoot(bad); err == nil {
			t.Errorf("ValidateRemoteRoot(%q) should fail", bad)
		}
	}
}
