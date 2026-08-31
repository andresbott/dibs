package tui

import (
	"sort"
	"testing"

	"github.com/andresbott/dibs/internal/config"
)

// setServerFormFields is a test helper that writes the input values into the
// form's textinputs and returns the parsed (name, server) pair. password is the
// secret field; passwordFile is the path field.
func setServerFormFields(f *serverFormModel, name, host, port, user, password, passwordFile string) (string, config.Server) {
	f.inputs[srvName].SetValue(name)
	f.inputs[srvHost].SetValue(host)
	f.inputs[srvPort].SetValue(port)
	f.inputs[srvUser].SetValue(user)
	f.inputs[srvPassword].SetValue(password)
	f.inputs[srvPassFile].SetValue(passwordFile)
	return f.values()
}

func TestServerFormValuesAndValidate(t *testing.T) {
	f := newServerForm("", config.Server{}, "")
	// simulate typing: set input values via the form's setters/test seam
	name, s := setServerFormFields(&f, "nas", "nas.local", "8730", "bob", "", "/etc/pw")
	if err := f.validate(); err != nil {
		t.Fatalf("valid form rejected: %v", err)
	}
	if name != "nas" || s.Host != "nas.local" || s.Port != 8730 || s.User != "bob" || s.PasswordFile != "/etc/pw" {
		t.Fatalf("values mismatch: %q %+v", name, s)
	}
	// empty host must fail validation
	setServerFormFields(&f, "nas", "", "", "", "", "")
	if err := f.validate(); err == nil {
		t.Fatal("empty host accepted")
	}
	// non-numeric port must fail validation
	setServerFormFields(&f, "nas", "nas.local", "abc", "", "", "")
	if err := f.validate(); err == nil {
		t.Fatal("non-numeric port accepted")
	}
}

func TestServerFormPassword(t *testing.T) {
	f := newServerForm("", config.Server{}, "/home/u/.config/dibs/config.yaml")
	// A blank / whitespace-only password reads as "" (no password).
	if got := f.password(); got != "" {
		t.Fatalf("blank password = %q, want empty", got)
	}
	f.inputs[srvPassword].SetValue("  ")
	if got := f.password(); got != "" {
		t.Fatalf("whitespace password = %q, want empty", got)
	}
	// A real password is returned verbatim.
	f.inputs[srvPassword].SetValue("s3 cret")
	if got := f.password(); got != "s3 cret" {
		t.Fatalf("password = %q, want %q", got, "s3 cret")
	}
	// The Password file placeholder tracks the typed name (default location).
	f.inputs[srvName].SetValue("nas")
	f.refreshPassFilePlaceholder()
	want := "/home/u/.config/dibs/nas.pw"
	if got := f.inputs[srvPassFile].Placeholder; got != want {
		t.Fatalf("placeholder = %q, want %q", got, want)
	}
}

func TestServerFormEditKeepsPasswordFieldBlank(t *testing.T) {
	// Editing a server with an existing password file prefills the path but
	// leaves the secret field blank (blank = keep the file).
	f := newServerForm("nas", config.Server{Host: "h", PasswordFile: "/etc/nas.pw"}, "/cfg/config.yaml")
	if got := f.inputs[srvPassFile].Value(); got != "/etc/nas.pw" {
		t.Fatalf("password file not prefilled: %q", got)
	}
	if got := f.password(); got != "" {
		t.Fatalf("password field should be blank on edit, got %q", got)
	}
}

func TestServerRefCount(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"a": {Server: "nas"}, "b": {Server: "nas"}, "c": {RemoteRoot: "/mnt"},
	}}
	got := serverRefCount(cfg, "nas")
	sort.Strings(got)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("refs = %v", got)
	}
}
