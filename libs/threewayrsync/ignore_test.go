package threewayrsync

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// mustTime parses an rsync %M-format mtime (the listing format the fakes emit) so
// base entries in these tests compare equal to the parsed listings.
func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.ParseInLocation(listMtimeLayout, s, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func TestNormalizeIgnore(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    []string
		wantErr bool
	}{
		{"empty", nil, nil, false},
		{"plain names", []string{".DS_Store", ".directory"}, []string{".DS_Store", ".directory"}, false},
		{"trims and drops blanks", []string{" .DS_Store ", "", "  "}, []string{".DS_Store"}, false},
		{"glob", []string{"*.tmp"}, []string{"*.tmp"}, false},
		{"slash refused", []string{"a/b"}, nil, true},
		{"bad glob refused", []string{"[unclosed"}, nil, true},
		{"all blanks yields nil", []string{"", " "}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeIgnore(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchesIgnore(t *testing.T) {
	pats := []string{".DS_Store", "*.tmp", ".git"}
	tests := []struct {
		path string
		want bool
	}{
		{".DS_Store", true},
		{"docs/.DS_Store", true},          // nested filename
		{".git", true},                    // the directory itself
		{".git/config", true},             // anything beneath an ignored dir
		{"a/b/.git/hooks/pre-commit", true},
		{"scratch.tmp", true},             // glob on the filename
		{"docs/api.txt", false},
		{"DS_Store", false},               // no leading dot — no match
		{"gitignore/.keep", false},        // segment must match exactly
	}
	for _, tt := range tests {
		if got := matchesIgnore(tt.path, pats); got != tt.want {
			t.Errorf("matchesIgnore(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
	if matchesIgnore("anything", nil) {
		t.Error("nil patterns must match nothing")
	}
}

func TestPartitionIgnored(t *testing.T) {
	m := Manifest{
		"a.txt":          {Size: 1},
		".DS_Store":      {Size: 2},
		"docs/.DS_Store": {Size: 3},
		"docs/b.txt":     {Size: 4},
	}
	kept, ignored := partitionIgnored(m, []string{".DS_Store"})
	if len(kept) != 2 {
		t.Errorf("kept = %v", kept)
	}
	if want := []string{".DS_Store", "docs/.DS_Store"}; !reflect.DeepEqual(ignored, want) {
		t.Errorf("ignored = %v, want %v", ignored, want)
	}
	// No patterns: the manifest passes through untouched (same map, no copy).
	kept, ignored = partitionIgnored(m, nil)
	if len(kept) != 4 || ignored != nil {
		t.Errorf("no-pattern partition changed the manifest: kept=%v ignored=%v", kept, ignored)
	}
}

// A remote-only ignored file is neither pulled nor deleted: it lands in
// Plan.Ignored and the plan stays in sync — a `.DS_Store` dropped on the share
// by mounting it must never block a checkin.
func TestDiffIgnoredRemoteOnlyFileStaysInSync(t *testing.T) {
	local, remote := Endpoint{Path: t.TempDir()}, Endpoint{Path: t.TempDir()}
	run := listRouter(t, map[string]string{
		local.Path + "/":  ">f+++++++++ 1 2026/07/14-09:15:00 a.txt\n",
		remote.Path + "/": ">f+++++++++ 1 2026/07/14-09:15:00 a.txt\n>f+++++++++ 2 2026/07/14-09:15:00 .DS_Store\n",
	})
	base := Manifest{"a.txt": {Size: 1, ModTime: mustTime(t, "2026/07/14-09:15:00")}}
	s := &Syncer{Store: &memStore{base: base, ok: true}, run: run}
	plan, err := s.Diff(context.Background(), local, remote, Options{Ignore: []string{".DS_Store"}})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.InSync {
		t.Errorf("plan not in sync: %+v", plan)
	}
	if want := []string{".DS_Store"}; !reflect.DeepEqual(plan.Ignored, want) {
		t.Errorf("Ignored = %v, want %v", plan.Ignored, want)
	}
}

// A file recorded in the base that now matches an ignore pattern must drop out
// of the merge silently — its absence from a listing is not a deletion.
func TestDiffIgnoredBaseEntryPlansNoDelete(t *testing.T) {
	local, remote := Endpoint{Path: t.TempDir()}, Endpoint{Path: t.TempDir()}
	run := listRouter(t, map[string]string{
		local.Path + "/":  ">f+++++++++ 1 2026/07/14-09:15:00 a.txt\n",
		remote.Path + "/": ">f+++++++++ 1 2026/07/14-09:15:00 a.txt\n",
	})
	base := Manifest{
		"a.txt":     {Size: 1, ModTime: mustTime(t, "2026/07/14-09:15:00")},
		".DS_Store": {Size: 9, ModTime: mustTime(t, "2026/07/14-09:15:00")},
	}
	s := &Syncer{Store: &memStore{base: base, ok: true}, run: run}
	plan, err := s.Diff(context.Background(), local, remote, Options{Ignore: []string{".DS_Store"}})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.InSync {
		t.Errorf("plan not in sync: %+v", plan)
	}
}

// Ignoring a directory name ignores everything beneath it, on either side.
func TestDiffIgnoredDirCoversChildren(t *testing.T) {
	local, remote := Endpoint{Path: t.TempDir()}, Endpoint{Path: t.TempDir()}
	run := listRouter(t, map[string]string{
		local.Path + "/": ">f+++++++++ 1 2026/07/14-09:15:00 a.txt\n" +
			"cd+++++++++ 0 2026/07/14-09:15:00 .git/\n" +
			">f+++++++++ 5 2026/07/14-09:15:00 .git/config\n",
		remote.Path + "/": ">f+++++++++ 1 2026/07/14-09:15:00 a.txt\n",
	})
	base := Manifest{"a.txt": {Size: 1, ModTime: mustTime(t, "2026/07/14-09:15:00")}}
	s := &Syncer{Store: &memStore{base: base, ok: true}, run: run}
	plan, err := s.Diff(context.Background(), local, remote, Options{Ignore: []string{".git"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Push) != 0 {
		t.Errorf("Push = %v, want none", plan.Push)
	}
	if want := []string{".git", ".git/config"}; !reflect.DeepEqual(plan.Ignored, want) {
		t.Errorf("Ignored = %v, want %v", plan.Ignored, want)
	}
}

// An invalid pattern is refused up front by both entry points.
func TestIgnoreInvalidPatternRefused(t *testing.T) {
	s := &Syncer{Store: &memStore{}, run: listRouter(t, nil)}
	opts := Options{Ignore: []string{"a/b"}}
	if _, err := s.Diff(context.Background(), Endpoint{Path: t.TempDir()}, Endpoint{Path: t.TempDir()}, opts); err == nil {
		t.Error("Diff must refuse a slash-containing ignore pattern")
	}
	if _, err := s.Sync(context.Background(), Endpoint{Path: t.TempDir()}, Endpoint{Path: t.TempDir()}, opts); err == nil {
		t.Error("Sync must refuse a slash-containing ignore pattern")
	}
}
