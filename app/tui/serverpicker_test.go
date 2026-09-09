package tui

import (
	"strings"
	"testing"

	"github.com/candy-tools/dibs/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

// serversNamed builds a Servers map with the given names (each a distinct host).
func serversNamed(names ...string) map[string]config.Server {
	s := make(map[string]config.Server, len(names))
	for _, n := range names {
		s[n] = config.Server{Host: n + ".local"}
	}
	return s
}

// openRsyncFormWith opens the add form, installs the given servers, and switches
// the remote type to rsync, leaving focus on the type selector.
func openRsyncFormWith(t *testing.T, servers map[string]config.Server) model {
	t.Helper()
	m := openAddForm(t)
	m.form.setServers(servers)
	m = tabToTypeSel(t, m)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight}) // → rsync
	return m
}

// TestServerSelModalThreshold: up to maxInlineServers list inline; one more
// spills into the modal picker.
func TestServerSelModalThreshold(t *testing.T) {
	f := newForm("", config.Profile{}, nil)
	f.setServers(serversNamed("a", "b", "c", "d")) // == maxInlineServers
	if f.serverSelIsModal() {
		t.Fatalf("%d servers should list inline", maxInlineServers)
	}
	f.setServers(serversNamed("a", "b", "c", "d", "e")) // maxInlineServers+1
	if !f.serverSelIsModal() {
		t.Fatalf("%d servers should use the modal", maxInlineServers+1)
	}
}

// TestServerInlineRadioListSelects: with a handful of servers the form lists them
// as radios; Down navigates the rows and space picks the focused one.
func TestServerInlineRadioListSelects(t *testing.T) {
	m := openRsyncFormWith(t, serversNamed("aaa", "bbb", "ccc"))

	view := m.form.View()
	for _, w := range []string{"( ) aaa", "( ) bbb", "( ) ccc"} {
		if !strings.Contains(view, w) {
			t.Fatalf("inline radio list missing %q:\n%s", w, view)
		}
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // → aaa row
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // → bbb row
	if m.form.focusKind() != slotServerSel || m.form.focusField() != 1 {
		t.Fatalf("want focus on the bbb row (field 1), got kind %v field %d", m.form.focusKind(), m.form.focusField())
	}
	m = update(t, m, spaceKey) // select bbb
	if m.form.serverSel != 1 || m.form.serverNames[m.form.serverSel] != "bbb" {
		t.Fatalf("serverSel=%d (%s), want bbb", m.form.serverSel, safeServerName(m.form.serverNames, m.form.serverSel))
	}
	if !strings.Contains(m.form.View(), "(•) bbb") {
		t.Fatalf("the picked server should be marked (•):\n%s", m.form.View())
	}
}

// TestServerModalPickerSelects: with more servers than fit inline the Server row
// collapses to a chooser hint; enter opens the modal, and selecting there sets
// the profile's server.
func TestServerModalPickerSelects(t *testing.T) {
	m := openRsyncFormWith(t, serversNamed("a", "b", "c", "d", "e")) // sorted a..e

	if v := m.form.View(); !strings.Contains(v, "enter to choose") {
		t.Fatalf("modal-mode Server row should hint at the chooser:\n%s", v)
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}) // → collapsed Server row
	if m.form.focusKind() != slotServerSel {
		t.Fatalf("want focus on the Server row, got kind %v", m.form.focusKind())
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // open the modal
	if !m.form.browsingServer {
		t.Fatal("enter on the collapsed Server row should open the picker")
	}
	if v := m.form.View(); !strings.Contains(v, "Select server") {
		t.Fatalf("modal title missing:\n%s", v)
	}

	// Highlight starts at row 0 (a); move down to "c" and select it.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = update(t, m, spaceKey)
	if m.form.browsingServer {
		t.Fatal("selecting in the modal should close it")
	}
	if m.form.serverNames[m.form.serverSel] != "c" {
		t.Fatalf("serverSel=%s, want c", safeServerName(m.form.serverNames, m.form.serverSel))
	}
	if !strings.Contains(m.form.View(), "(•) c") {
		t.Fatalf("collapsed row should show the picked server:\n%s", m.form.View())
	}
}

// TestServerModalCancelKeepsSelection: esc (and Cancel) in the modal leaves the
// prior selection untouched.
func TestServerModalCancelKeepsSelection(t *testing.T) {
	m := openRsyncFormWith(t, serversNamed("a", "b", "c", "d", "e"))
	m.form.selectServer("b")

	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // → Server row
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // open modal
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})  // move the highlight off b
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})   // cancel
	if m.form.browsingServer {
		t.Fatal("esc should close the modal")
	}
	if m.form.serverNames[m.form.serverSel] != "b" {
		t.Fatalf("esc must keep the prior selection b, got %s", safeServerName(m.form.serverNames, m.form.serverSel))
	}
}
