package tui

import (
	"sort"
	"testing"

	"github.com/andresbott/dibs/internal/config"
)

// setServerFormFields is a test helper that writes the five input values into
// the form's textinputs and returns the parsed (name, server) pair.
func setServerFormFields(f *serverFormModel, name, host, port, user, passwordFile string) (string, config.Server) {
	f.inputs[srvName].SetValue(name)
	f.inputs[srvHost].SetValue(host)
	f.inputs[srvPort].SetValue(port)
	f.inputs[srvUser].SetValue(user)
	f.inputs[srvPass].SetValue(passwordFile)
	return f.values()
}

func TestServerFormValuesAndValidate(t *testing.T) {
	f := newServerForm("", config.Server{})
	// simulate typing: set input values via the form's setters/test seam
	name, s := setServerFormFields(&f, "nas", "nas.local", "8730", "bob", "/etc/pw")
	if err := f.validate(); err != nil {
		t.Fatalf("valid form rejected: %v", err)
	}
	if name != "nas" || s.Host != "nas.local" || s.Port != 8730 || s.User != "bob" || s.PasswordFile != "/etc/pw" {
		t.Fatalf("values mismatch: %q %+v", name, s)
	}
	// empty host must fail validation
	setServerFormFields(&f, "nas", "", "", "", "")
	if err := f.validate(); err == nil {
		t.Fatal("empty host accepted")
	}
	// non-numeric port must fail validation
	setServerFormFields(&f, "nas", "nas.local", "abc", "", "")
	if err := f.validate(); err == nil {
		t.Fatal("non-numeric port accepted")
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
