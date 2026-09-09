package tui

import (
	"testing"

	"github.com/candy-tools/dibs/internal/config"
	"github.com/candy-tools/dibs/internal/ident"
	"github.com/candy-tools/dibs/internal/sanity"
)

func TestVisibleActionsHiddenOnConfigErr(t *testing.T) {
	r := &sanity.Result{ConfigErr: "profile \"x\" references unknown server \"ghost\""}
	if acts := visibleActions(r, ident.Ident{}, ""); acts != nil {
		t.Fatalf("expected no actions on config error, got %v", acts)
	}
}

func TestSanityCmdReportsResolveError(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{"x": {Server: "ghost", RemoteModule: "m"}}}
	msg := sanityCmd(cfg, "x", "")()
	res, ok := msg.(sanityResultMsg)
	if !ok {
		t.Fatalf("unexpected msg type %T", msg)
	}
	if res.result.ConfigErr == "" {
		t.Fatal("expected ConfigErr to be set for an unknown-server profile")
	}
}
