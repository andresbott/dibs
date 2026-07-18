package ident

import (
	"os"
	"testing"

	"github.com/andresbott/netcheckout/internal/config"
)

func TestResolveUsesConfigIdentity(t *testing.T) {
	got, err := Resolve(&config.Config{Identity: "andres@thinkpad"})
	if err != nil {
		t.Fatal(err)
	}
	if got.By != "andres@thinkpad" {
		t.Errorf("By = %q, want andres@thinkpad", got.By)
	}
	host, _ := os.Hostname()
	if got.Host != host {
		t.Errorf("Host = %q, want %q", got.Host, host)
	}
}

func TestResolveFallsBackToUserAtHost(t *testing.T) {
	t.Setenv("USER", "tester")
	got, err := Resolve(&config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	host, _ := os.Hostname()
	want := "tester@" + host
	if got.By != want {
		t.Errorf("By = %q, want %q", got.By, want)
	}
}
