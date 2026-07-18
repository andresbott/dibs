package threewayrsync

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

func TestNormalizeScope(t *testing.T) {
	got, err := normalizeScope([]string{"docs/api/", "src"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"docs/api", "src"}) {
		t.Errorf("normalized = %v", got)
	}
	for name, scope := range map[string][]string{
		"absolute":  {"/etc"},
		"dotdot":    {"a/../b"},
		"dot":       {"./a"},
		"empty":     {""},
		"wildcard":  {"docs/*"},
		"question":  {"d?cs"},
		"brackets":  {"d[oc]s"},
		"nul":       {"a\x00b"},
		"slashonly": {"/"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeScope(scope); err == nil {
				t.Errorf("scope %v must be rejected", scope)
			}
		})
	}
}

func TestScopeFilterArgs(t *testing.T) {
	if got := scopeFilterArgs(nil); got != nil {
		t.Errorf("empty scope must add no filters, got %v", got)
	}
	got := scopeFilterArgs([]string{"a/b/c", "a/d"})
	want := []string{
		"--include=/a/", "--include=/a/b/", "--include=/a/b/c/***",
		"--include=/a/d/***", // "/a/" ancestor deduped
		"--exclude=*",
	}
	if !slices.Equal(got, want) {
		t.Errorf("filters = %v, want %v", got, want)
	}
	if got[len(got)-1] != "--exclude=*" {
		t.Errorf("catch-all exclude must be last: %v", got)
	}
}

func TestInScope(t *testing.T) {
	scope := []string{"docs/api"}
	cases := map[string]bool{
		"docs/api":           true,
		"docs/api/x.txt":     true,
		"docs/api/sub/y.txt": true,
		"docs/apiary/z.txt":  false, // prefix match must be segment-aware
		"docs/other.txt":     false,
		"top.txt":            false,
	}
	for p, want := range cases {
		if got := inScope(p, scope); got != want {
			t.Errorf("inScope(%q) = %v, want %v", p, got, want)
		}
	}
	if !inScope("anything", nil) {
		t.Error("empty scope must match everything")
	}
}

func TestSyncRejectsBadScope(t *testing.T) {
	local, remote := testEndpoints(t)
	s := &Syncer{Store: &memStore{}, run: (&applyRunner{}).run}
	if _, err := s.Sync(context.Background(), local, remote, Options{Scope: []string{"../up"}}); err == nil {
		t.Error("Sync must reject an unsafe scope")
	}
	if _, err := s.Diff(context.Background(), local, remote, Options{Scope: []string{"a/*"}}); err == nil {
		t.Error("Diff must reject a wildcard scope")
	}
}

func TestScopedArgBuildersCarryFilters(t *testing.T) {
	scope := []string{"keep"}
	for name, args := range map[string][]string{
		"list":     buildListArgs(Endpoint{Path: "/src"}, "/tmp/empty", nil, scope),
		"transfer": buildTransferArgs(Endpoint{Path: "/src"}, Endpoint{Path: "/dst"}, false, nil, scope),
	} {
		for _, want := range []string{"--include=/keep/***", "--exclude=*"} {
			if !slices.Contains(args, want) {
				t.Errorf("%s args missing %q: %v", name, want, args)
			}
		}
	}
}

func TestMergedBasePreservesOutOfScopeEntries(t *testing.T) {
	prev := Manifest{
		"keep/a.txt":  {Size: 1, ModTime: time.Unix(1, 0)},
		"skip/b.txt":  {Size: 2, ModTime: time.Unix(2, 0)},
		"skip/c/d.md": {Size: 3, ModTime: time.Unix(3, 0)},
	}
	// Scoped post-apply listings only see keep/.
	post := Manifest{"keep/a.txt": {Size: 9, ModTime: time.Unix(9, 0)}}
	merged := mergedBase(prev, post, post, []string{"keep"})
	if merged["keep/a.txt"].Size != 9 {
		t.Errorf("in-scope agreement must enter the base: %+v", merged["keep/a.txt"])
	}
	if merged["skip/b.txt"].Size != 2 || merged["skip/c/d.md"].Size != 3 {
		t.Errorf("out-of-scope base entries must be preserved: %+v", merged)
	}
	// Unscoped merge with the same listings would drop them — the scope is what saves them.
	unscoped := mergedBase(prev, post, post, nil)
	if _, ok := unscoped["skip/b.txt"]; ok {
		t.Error("sanity: unscoped merge does not preserve absent paths")
	}
}

func TestSyncScopedGuardsCountInScopeOnly(t *testing.T) {
	local, remote := testEndpoints(t)
	// Base has 20 out-of-scope files and 2 in-scope; the scoped listings see one in-scope
	// file on local and none on remote => 1 remote delete... but first: with AcceptEmpty
	// unset and an empty scoped remote listing, the in-scope base (2 files) trips the
	// EmptyEndpointError valve. That's the desired behavior.
	base := Manifest{}
	for i := 0; i < 20; i++ {
		base["out/f"+string(rune('a'+i))+".txt"] = FileState{Size: 1, ModTime: time.Unix(1, 0)}
	}
	base["in/x.txt"] = FileState{Size: 1, ModTime: time.Date(2026, 7, 14, 9, 15, 0, 0, time.Local)}
	base["in/y.txt"] = FileState{Size: 1, ModTime: time.Date(2026, 7, 14, 9, 15, 0, 0, time.Local)}

	ar := &applyRunner{lists: map[string]string{
		local.Path + "/":  ">f+++++++++ 1 2026/07/14-09:15:00 in/x.txt\n>f+++++++++ 1 2026/07/14-09:15:00 in/y.txt\n",
		remote.Path + "/": "",
	}}
	store := &memStore{base: base, ok: true}
	s := &Syncer{Store: store, run: ar.run}
	_, err := s.Sync(context.Background(), local, remote, Options{Scope: []string{"in"}})
	var ee *EmptyEndpointError
	if !errors.As(err, &ee) {
		t.Fatalf("empty scoped remote with in-scope base entries must trip EmptyEndpointError, got %v", err)
	}

	// A scope whose base part is empty must NOT trip the valve even though the full base
	// has entries: an empty scoped listing there is legitimate.
	ar2 := &applyRunner{lists: map[string]string{}}
	s2 := &Syncer{Store: &memStore{base: base, ok: true}, run: ar2.run}
	if _, err := s2.Sync(context.Background(), local, remote, Options{Scope: []string{"newdir"}}); err != nil {
		t.Fatalf("scope with no base entries must not trip the empty valve: %v", err)
	}
}
