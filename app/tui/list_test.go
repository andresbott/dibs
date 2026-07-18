package tui

import (
	"strings"
	"testing"
)

func TestListSelectedAndNavigation(t *testing.T) {
	l := newList([]string{"alpha", "beta", "gamma"})
	if name, ok := l.selected(); !ok || name != "alpha" {
		t.Fatalf("initial selected = %q,%v, want alpha,true", name, ok)
	}
	l.moveDown()
	if name, _ := l.selected(); name != "beta" {
		t.Fatalf("after moveDown selected = %q, want beta", name)
	}
	l.moveUp()
	l.moveUp() // clamps at top
	if name, _ := l.selected(); name != "alpha" {
		t.Fatalf("after moveUp*2 selected = %q, want alpha", name)
	}
}

func TestListEmptySelected(t *testing.T) {
	l := newList(nil)
	if _, ok := l.selected(); ok {
		t.Fatal("empty list should report no selection")
	}
}

func TestListSetNamesClampsCursor(t *testing.T) {
	l := newList([]string{"a", "b", "c"})
	l.moveDown()
	l.moveDown() // cursor at 2 ("c")
	l.setNames([]string{"x"})
	if name, ok := l.selected(); !ok || name != "x" {
		t.Fatalf("after shrink selected = %q,%v, want x,true", name, ok)
	}
}

func TestListViewShowsSelection(t *testing.T) {
	l := newList([]string{"alpha", "beta"})
	l.moveDown()
	view := l.view(20, 5)
	if !strings.Contains(view, "beta") {
		t.Fatalf("view missing selected name:\n%s", view)
	}
	if !strings.Contains(view, "▌") {
		t.Fatalf("view missing selection marker:\n%s", view)
	}
}
