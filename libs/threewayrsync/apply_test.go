package threewayrsync

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// testEndpoints returns two local endpoints backed by real (empty) temp dirs so the
// preflight stat passes; enumeration still comes from the fake runner's canned lists.
func testEndpoints(t *testing.T) (Endpoint, Endpoint) {
	t.Helper()
	return Endpoint{Path: t.TempDir()}, Endpoint{Path: t.TempDir()}
}

// applyRunner routes list calls (by source path) to canned manifests and records transfer
// and remote-delete calls. computePlan performs exactly two list calls, so from the third
// list on — the post-apply re-list — answers come from postLists (when set), simulating
// the state after apply. It returns empty output (success) for non-list calls, unless
// failTransfer is set, in which case a transfer call (not a list, not a delete) fails
// with a non-nil error.
type applyRunner struct {
	lists        map[string]string // src prefix -> list output before apply
	postLists    map[string]string // src prefix -> list output for the post-apply re-list
	transfers    [][]string        // captured rsync transfer arg lists
	deletes      [][]string        // captured rsync remote-delete arg lists (--delete-missing-args)
	deletedLists []string          // contents of each delete call's --files-from list
	failTransfer bool              // when true, a transfer call returns a non-nil error
	listCalls    int
}

func (a *applyRunner) run(_ context.Context, _ string, args []string, tee io.Writer) (runResult, error) {
	// rsync: a list call has --dry-run + --out-format; a remote delete has
	// --delete-missing-args; anything else is a transfer.
	isList, isDelete := false, false
	var filesFrom string
	for _, x := range args {
		switch {
		case x == "--dry-run":
			isList = true
		case x == "--delete-missing-args":
			isDelete = true
		case strings.HasPrefix(x, "--files-from="):
			filesFrom = strings.TrimPrefix(x, "--files-from=")
		}
	}
	if isDelete {
		a.deletes = append(a.deletes, args)
		if filesFrom != "" {
			data, err := os.ReadFile(filesFrom)
			if err != nil {
				return runResult{}, err
			}
			a.deletedLists = append(a.deletedLists, string(data))
		}
		return runResult{}, nil
	}
	if isList {
		a.listCalls++
		lists := a.lists
		if a.listCalls > 2 && a.postLists != nil {
			lists = a.postLists
		}
		src := args[len(args)-2]
		for prefix, out := range lists {
			if strings.HasPrefix(src, prefix) {
				return runResult{stdout: out}, nil
			}
		}
		return runResult{}, nil
	}
	a.transfers = append(a.transfers, args)
	if a.failTransfer {
		return runResult{stderr: "rsync: connection unexpectedly closed", exitCode: 12}, errors.New("exit status 12")
	}
	return runResult{}, nil
}

func TestSyncAbortsOnConflict(t *testing.T) {
	local, remote := testEndpoints(t)
	ar := &applyRunner{lists: map[string]string{
		local.Path + "/":  ">f.st...... 2 2026/07/14-09:15:01 x.txt\n", // local edited
		remote.Path + "/": ">f.st...... 3 2026/07/14-09:15:02 x.txt\n", // remote edited differently
	}}
	base := Manifest{"x.txt": {Size: 1, ModTime: time.Unix(100, 0)}}
	store := &memStore{base: base, ok: true}
	s := &Syncer{Store: store, run: ar.run}
	_, err := s.Sync(context.Background(), local, remote, Options{Conflict: Abort})
	var ce *ConflictError
	if !errors.As(err, &ce) || len(ce.Paths) != 1 || ce.Paths[0] != "x.txt" {
		t.Fatalf("want ConflictError for x.txt, got %v", err)
	}
	if len(ar.transfers) != 0 {
		t.Errorf("Abort must not transfer anything, got %v", ar.transfers)
	}
	if store.saved != 0 {
		t.Errorf("Abort must not persist a base, got saved=%d", store.saved)
	}
}

func TestSyncPreferLocalPushesConflict(t *testing.T) {
	local, remote := testEndpoints(t)
	agreed := ">f.st...... 2 2026/07/14-09:15:01 x.txt\n"
	ar := &applyRunner{
		lists: map[string]string{
			local.Path + "/":  agreed,
			remote.Path + "/": ">f.st...... 3 2026/07/14-09:15:02 x.txt\n",
		},
		// After the push both sides agree on the local state.
		postLists: map[string]string{local.Path + "/": agreed, remote.Path + "/": agreed},
	}
	store := &memStore{base: Manifest{"x.txt": {Size: 1, ModTime: time.Unix(100, 0)}}, ok: true}
	s := &Syncer{Store: store, run: ar.run}
	res, err := s.Sync(context.Background(), local, remote, Options{Conflict: PreferLocal})
	if err != nil {
		t.Fatal(err)
	}
	if len(ar.transfers) != 1 {
		t.Fatalf("want one push transfer, got %d", len(ar.transfers))
	}
	// A push runs local -> remote: source (2nd-to-last arg) is under the local root.
	pushArgs := ar.transfers[0]
	if !strings.HasPrefix(pushArgs[len(pushArgs)-2], local.Path+"/") {
		t.Errorf("conflict should be pushed local->remote: %v", pushArgs)
	}
	if !res.BaseSaved || store.saved != 1 {
		t.Errorf("base must be saved exactly once; BaseSaved=%v saved=%d", res.BaseSaved, store.saved)
	}
	// After PreferLocal, merged base records the local state (size 2).
	if store.base["x.txt"].Size != 2 {
		t.Errorf("merged base x.txt size = %d, want 2", store.base["x.txt"].Size)
	}
}

func TestSyncPreferLocalDeletesRemoteOnDeleteVsEdit(t *testing.T) {
	local, remote := testEndpoints(t)
	anchor := ">f+++++++++ 1 2026/07/14-09:15:00 anchor.txt\n"
	ar := &applyRunner{
		lists: map[string]string{
			local.Path + "/":  anchor,                                                   // x.txt deleted locally
			remote.Path + "/": anchor + ">f.st...... 5 2026/07/14-09:15:02 x.txt\n",     // remote edited x.txt
		},
		postLists: map[string]string{local.Path + "/": anchor, remote.Path + "/": anchor},
	}
	base := Manifest{
		"x.txt":      {Size: 1, ModTime: time.Unix(100, 0)},
		"anchor.txt": {Size: 1, ModTime: time.Date(2026, 7, 14, 9, 15, 0, 0, time.Local)},
	}
	store := &memStore{base: base, ok: true}
	s := &Syncer{Store: store, run: ar.run}
	res, err := s.Sync(context.Background(), local, remote, Options{Conflict: PreferLocal, AcceptEmpty: true, AllowDeletes: true})
	if err != nil {
		var ce *ConflictError
		if errors.As(err, &ce) {
			t.Fatalf("PreferLocal must resolve a delete-vs-edit conflict, not report ConflictError: %v", err)
		}
		t.Fatal(err)
	}
	if !slices.Contains(res.Applied.RemoteDeletes, "x.txt") {
		t.Errorf("RemoteDeletes = %v, want x.txt", res.Applied.RemoteDeletes)
	}
	if slices.Contains(res.Applied.Push, "x.txt") {
		t.Errorf("Push must not contain x.txt when local already deleted it: %v", res.Applied.Push)
	}
	if _, ok := store.base["x.txt"]; ok {
		t.Errorf("saved base must not resurrect x.txt: %+v", store.base)
	}
	if store.saved != 1 {
		t.Errorf("base must be saved exactly once; saved=%d", store.saved)
	}
}

func TestSyncPreferRemoteDeletesLocalOnEditVsDelete(t *testing.T) {
	local, remote := testEndpoints(t)
	anchor := ">f+++++++++ 1 2026/07/14-09:15:00 anchor.txt\n"
	ar := &applyRunner{
		lists: map[string]string{
			local.Path + "/":  anchor + ">f.st...... 5 2026/07/14-09:15:02 x.txt\n", // local edited x.txt
			remote.Path + "/": anchor,                                              // x.txt deleted remotely
		},
		postLists: map[string]string{local.Path + "/": anchor, remote.Path + "/": anchor},
	}
	base := Manifest{
		"x.txt":      {Size: 1, ModTime: time.Unix(100, 0)},
		"anchor.txt": {Size: 1, ModTime: time.Date(2026, 7, 14, 9, 15, 0, 0, time.Local)},
	}
	store := &memStore{base: base, ok: true}
	s := &Syncer{Store: store, run: ar.run}
	res, err := s.Sync(context.Background(), local, remote, Options{Conflict: PreferRemote, AcceptEmpty: true, AllowDeletes: true})
	if err != nil {
		var ce *ConflictError
		if errors.As(err, &ce) {
			t.Fatalf("PreferRemote must resolve an edit-vs-delete conflict, not report ConflictError: %v", err)
		}
		t.Fatal(err)
	}
	if !slices.Contains(res.Applied.LocalDeletes, "x.txt") {
		t.Errorf("LocalDeletes = %v, want x.txt", res.Applied.LocalDeletes)
	}
	if slices.Contains(res.Applied.Pull, "x.txt") {
		t.Errorf("Pull must not contain x.txt when remote already deleted it: %v", res.Applied.Pull)
	}
	if _, ok := store.base["x.txt"]; ok {
		t.Errorf("saved base must not resurrect x.txt: %+v", store.base)
	}
	if store.saved != 1 {
		t.Errorf("base must be saved exactly once; saved=%d", store.saved)
	}
}

func TestSyncCanceledContextDoesNotSaveBase(t *testing.T) {
	local, remote := testEndpoints(t)
	failing := func(ctx context.Context, _ string, _ []string, _ io.Writer) (runResult, error) {
		return runResult{}, ctx.Err()
	}
	store := &memStore{}
	s := &Syncer{Store: store, run: failing}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.Sync(ctx, local, remote, Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation must surface ctx.Err(), got %v", err)
	}
	if store.saved != 0 {
		t.Errorf("base must not be saved on cancel; saved=%d", store.saved)
	}
}

func TestSyncErrorAfterTransferDoesNotSaveBase(t *testing.T) {
	local, remote := testEndpoints(t)
	ar := &applyRunner{
		lists: map[string]string{
			local.Path + "/":  ">f+++++++++ 1 2026/07/14-09:15:00 new.txt\n", // local-only add => push
			remote.Path + "/": "",
		},
		failTransfer: true,
	}
	store := &memStore{}
	s := &Syncer{Store: store, run: ar.run}
	_, err := s.Sync(context.Background(), local, remote, Options{})
	if err == nil {
		t.Fatal("want an error when the transfer fails")
	}
	if store.saved != 0 {
		t.Errorf("base must not be saved when a transfer fails after a successful list; saved=%d", store.saved)
	}
}

// With AllowDeletes off (the zero value) Sync must not delete anything, however
// large the planned deletion: the deletes come back in SkippedLocalDeletes, and
// the base keeps their entries so the next run re-plans exactly the same
// deletions — pending, not resurrected.
func TestSyncSkipsDeletesWithoutAllowDeletes(t *testing.T) {
	local, remote := testEndpoints(t)
	// Remote lists 1 of the 20 base files; local still lists all 20, unchanged
	// => 19 local-deletes planned (mirror the remote deletions).
	base := Manifest{}
	var localList strings.Builder
	for i := 0; i < 20; i++ {
		name := "f" + strconv.Itoa(i) + ".txt"
		base[name] = FileState{Size: 1, ModTime: time.Date(2026, 7, 14, 9, 15, 0, 0, time.Local)}
		localList.WriteString(">f+++++++++ 1 2026/07/14-09:15:00 " + name + "\n")
	}
	ar := &applyRunner{lists: map[string]string{
		local.Path + "/":  localList.String(),
		remote.Path + "/": ">f+++++++++ 1 2026/07/14-09:15:00 f0.txt\n",
	}}
	store := &memStore{base: base, ok: true}
	s := &Syncer{Store: store, run: ar.run}
	res, err := s.Sync(context.Background(), local, remote, Options{})
	if err != nil {
		t.Fatalf("flag-off sync must not error on planned deletes: %v", err)
	}
	if len(res.Applied.LocalDeletes)+len(res.Applied.RemoteDeletes) != 0 {
		t.Errorf("no delete may be applied, got %+v", res.Applied)
	}
	if len(res.SkippedLocalDeletes) != 19 {
		t.Errorf("SkippedLocalDeletes = %d, want 19", len(res.SkippedLocalDeletes))
	}
	if len(ar.deletes) != 0 || len(ar.transfers) != 0 {
		t.Errorf("no rsync delete/transfer may run; deletes=%d transfers=%d", len(ar.deletes), len(ar.transfers))
	}
	// The base keeps every entry: the deletions stay pending, nothing is resurrected.
	if len(store.base) != 20 {
		t.Errorf("base must keep all 20 entries, has %d", len(store.base))
	}
	// A second run re-plans the same deletions.
	res2, err := s.Sync(context.Background(), local, remote, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.SkippedLocalDeletes) != 19 {
		t.Errorf("second run SkippedLocalDeletes = %d, want 19 (still pending)", len(res2.SkippedLocalDeletes))
	}
}

// AllowDeletes applies the planned deletions (below a full wipe).
func TestSyncAppliesDeletesWithAllowDeletes(t *testing.T) {
	local, remote := testEndpoints(t)
	base := Manifest{}
	var localList strings.Builder
	for i := 0; i < 20; i++ {
		name := "f" + strconv.Itoa(i) + ".txt"
		base[name] = FileState{Size: 1, ModTime: time.Date(2026, 7, 14, 9, 15, 0, 0, time.Local)}
		localList.WriteString(">f+++++++++ 1 2026/07/14-09:15:00 " + name + "\n")
	}
	ar := &applyRunner{lists: map[string]string{
		local.Path + "/":  localList.String(),
		remote.Path + "/": ">f+++++++++ 1 2026/07/14-09:15:00 f0.txt\n",
	}}
	store := &memStore{base: base, ok: true}
	s := &Syncer{Store: store, run: ar.run}
	res, err := s.Sync(context.Background(), local, remote, Options{AllowDeletes: true})
	if err != nil {
		t.Fatalf("AllowDeletes sync: %v", err)
	}
	if len(res.Applied.LocalDeletes) != 19 {
		t.Errorf("Applied.LocalDeletes = %d, want 19", len(res.Applied.LocalDeletes))
	}
	if len(res.SkippedLocalDeletes)+len(res.SkippedRemoteDeletes) != 0 {
		t.Errorf("nothing may be skipped with the flag on: %+v", res)
	}
}

// The wipe valve: with AllowDeletes on, a plan that deletes 100% of an endpoint's
// in-scope files with nothing transferring in must refuse — and nothing waives it.
// With AllowDeletes off the same state is harmless: the deletes are skipped and
// reported pending, and the sync proceeds.
func TestSyncFullWipeIsNeverApplied(t *testing.T) {
	local, remote := testEndpoints(t)
	// Local was emptied by hand; remote still lists both base files, unchanged.
	remoteList := ">f+++++++++ 1 2026/07/14-09:15:00 a.txt\n>f+++++++++ 1 2026/07/14-09:15:00 b.txt\n"
	ar := &applyRunner{lists: map[string]string{
		local.Path + "/":  "",
		remote.Path + "/": remoteList,
	}}
	base := Manifest{
		"a.txt": {Size: 1, ModTime: time.Date(2026, 7, 14, 9, 15, 0, 0, time.Local)},
		"b.txt": {Size: 1, ModTime: time.Date(2026, 7, 14, 9, 15, 0, 0, time.Local)},
	}
	store := &memStore{base: base, ok: true}
	s := &Syncer{Store: store, run: ar.run}
	_, err := s.Sync(context.Background(), local, remote, Options{AcceptEmpty: true, AllowDeletes: true})
	var ww *WouldWipeError
	if !errors.As(err, &ww) || ww.Side != "remote" || ww.Deletes != 2 {
		t.Fatalf("want WouldWipeError for the remote side (2 deletes), got %v", err)
	}
	if store.saved != 0 || len(ar.transfers) != 0 || len(ar.deletes) != 0 {
		t.Errorf("valve must fire before any change; saved=%d transfers=%d deletes=%d",
			store.saved, len(ar.transfers), len(ar.deletes))
	}
	// Flag off: the wiped-looking state is harmless — deletes are skipped, pending.
	res, err := s.Sync(context.Background(), local, remote, Options{AcceptEmpty: true})
	if err != nil {
		t.Fatalf("flag-off sync over a wiped side must proceed: %v", err)
	}
	if len(res.SkippedRemoteDeletes) != 2 || len(res.Applied.RemoteDeletes) != 0 {
		t.Errorf("want 2 skipped remote deletes and none applied, got %+v", res)
	}
}

// A whole-tree rename (delete-all + add-all) has incoming transfers on the deleted side,
// so it must NOT trip the wipe valve: with AllowDeletes the deletions simply apply.
func TestSyncWipeValveIgnoresRenames(t *testing.T) {
	local, remote := testEndpoints(t)
	mt := "2026/07/14-09:15:00"
	// Local renamed a.txt -> b.txt; remote still has a.txt only. Plan: push b.txt
	// (incoming on remote) + remote-delete a.txt (100% of remote's single file).
	ar := &applyRunner{lists: map[string]string{
		local.Path + "/":  ">f+++++++++ 1 " + mt + " b.txt\n",
		remote.Path + "/": ">f+++++++++ 1 " + mt + " a.txt\n",
	}}
	base := Manifest{"a.txt": {Size: 1, ModTime: time.Date(2026, 7, 14, 9, 15, 0, 0, time.Local)}}
	store := &memStore{base: base, ok: true}
	s := &Syncer{Store: store, run: ar.run}
	res, err := s.Sync(context.Background(), local, remote, Options{AllowDeletes: true})
	if err != nil {
		t.Fatalf("a rename must not trip the wipe valve: %v", err)
	}
	if len(res.Applied.Push) != 1 || len(res.Applied.RemoteDeletes) != 1 {
		t.Errorf("Applied = %+v, want 1 push + 1 remote delete", res.Applied)
	}
}

func TestSyncRefusesEmptyEndpointWithHistory(t *testing.T) {
	local, remote := testEndpoints(t)
	ar := &applyRunner{lists: map[string]string{
		local.Path + "/":  ">f+++++++++ 1 2026/07/14-09:15:00 a.txt\n",
		remote.Path + "/": "", // remote suddenly lists nothing
	}}
	base := Manifest{"a.txt": {Size: 1, ModTime: time.Date(2026, 7, 14, 9, 15, 0, 0, time.Local)}}
	store := &memStore{base: base, ok: true}
	s := &Syncer{Store: store, run: ar.run}
	_, err := s.Sync(context.Background(), local, remote, Options{})
	var ee *EmptyEndpointError
	if !errors.As(err, &ee) || ee.Side != "remote" {
		t.Fatalf("want EmptyEndpointError for the remote side, got %v", err)
	}
	// AcceptEmpty overrides (the delete gate is a separate valve, disabled here).
	if _, err := s.Sync(context.Background(), local, remote, Options{AcceptEmpty: true}); err != nil {
		t.Fatalf("AcceptEmpty must allow the sync: %v", err)
	}
}

// A side that lists nothing but directories is as suspicious as one listing nothing at
// all: its files are what a previous sync recorded, and they are gone.
func TestSyncRefusesDirOnlyEndpointWithHistory(t *testing.T) {
	local, remote := testEndpoints(t)
	ar := &applyRunner{lists: map[string]string{
		local.Path + "/":  ">f+++++++++ 1 2026/07/14-09:15:00 a.txt\n",
		remote.Path + "/": "cd+++++++++ 80 2026/07/14-09:15:00 sub/\n", // dirs only
	}}
	base := Manifest{"a.txt": {Size: 1, ModTime: time.Date(2026, 7, 14, 9, 15, 0, 0, time.Local)}}
	store := &memStore{base: base, ok: true}
	s := &Syncer{Store: store, run: ar.run}
	_, err := s.Sync(context.Background(), local, remote, Options{})
	var ee *EmptyEndpointError
	if !errors.As(err, &ee) || ee.Side != "remote" {
		t.Fatalf("want EmptyEndpointError for the dir-only remote side, got %v", err)
	}
}

func TestSyncMissingLocalEndpointFails(t *testing.T) {
	local, _ := testEndpoints(t)
	s := &Syncer{Store: &memStore{}, run: (&applyRunner{}).run}
	_, err := s.Sync(context.Background(), local, Endpoint{Path: "/does/not/exist-xyz"}, Options{})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("want missing-endpoint error, got %v", err)
	}
}

func TestSyncNilStoreErrors(t *testing.T) {
	local, remote := testEndpoints(t)
	s := &Syncer{run: (&applyRunner{}).run}
	if _, err := s.Sync(context.Background(), local, remote, Options{}); err == nil {
		t.Fatal("nil Store must be an error, not a panic")
	}
}

// lockStore wraps memStore with a Locker that is already held.
type lockStore struct{ memStore }

func (l *lockStore) TryLock() (func(), error) { return nil, ErrLocked }

func TestSyncFailsFastWhenLocked(t *testing.T) {
	local, remote := testEndpoints(t)
	s := &Syncer{Store: &lockStore{}, run: (&applyRunner{}).run}
	if _, err := s.Sync(context.Background(), local, remote, Options{}); !errors.Is(err, ErrLocked) {
		t.Fatalf("want ErrLocked, got %v", err)
	}
}

func TestSyncSkipsDeleteOfLocallyChangedFile(t *testing.T) {
	local, remote := testEndpoints(t)
	// Base + local list say x.txt exists (size 1); remote deleted it => LocalDelete. But
	// the file on disk differs from the planned state (edited after enumeration): the
	// guard must skip it, not delete it. anchor.txt keeps the delete below a full wipe.
	if err := os.WriteFile(filepath.Join(local.Path, "x.txt"), []byte("edited-after-listing"), 0o644); err != nil {
		t.Fatal(err)
	}
	anchor := ">f+++++++++ 1 2026/07/14-09:15:00 anchor.txt\n"
	ar := &applyRunner{lists: map[string]string{
		local.Path + "/":  anchor + ">f+++++++++ 1 2026/07/14-09:15:00 x.txt\n",
		remote.Path + "/": anchor,
	}}
	base := Manifest{
		"x.txt":      {Size: 1, ModTime: time.Date(2026, 7, 14, 9, 15, 0, 0, time.Local)},
		"anchor.txt": {Size: 1, ModTime: time.Date(2026, 7, 14, 9, 15, 0, 0, time.Local)},
	}
	store := &memStore{base: base, ok: true}
	s := &Syncer{Store: store, run: ar.run}
	res, err := s.Sync(context.Background(), local, remote, Options{AcceptEmpty: true, AllowDeletes: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(local.Path, "x.txt")); statErr != nil {
		t.Fatalf("changed file must survive the delete: %v", statErr)
	}
	if len(res.Applied.LocalDeletes) != 0 {
		t.Errorf("LocalDeletes = %v, want none", res.Applied.LocalDeletes)
	}
	if !slices.Contains(res.Conflicts, "x.txt") {
		t.Errorf("skipped delete must be reported as a conflict: %v", res.Conflicts)
	}
}

func TestMergedBaseRecordsAgreementOnly(t *testing.T) {
	prev := Manifest{"old.txt": {Size: 1, ModTime: time.Unix(1, 0)}}
	postLocal := Manifest{
		"agree.txt": {Size: 9, ModTime: time.Unix(2, 0)},
		"old.txt":   {Size: 5, ModTime: time.Unix(5, 0)}, // still disagrees with remote
	}
	postRemote := Manifest{
		"agree.txt": {Size: 9, ModTime: time.Unix(2, 0)},
		"old.txt":   {Size: 7, ModTime: time.Unix(7, 0)},
	}
	merged := mergedBase(prev, postLocal, postRemote, nil)
	if merged["agree.txt"].Size != 9 {
		t.Errorf("agreeing path should enter the base: %+v", merged["agree.txt"])
	}
	// A disagreeing path keeps its previous base entry so it resurfaces next run.
	if merged["old.txt"].Size != 1 {
		t.Errorf("disagreeing path must retain the previous base entry, got %+v", merged["old.txt"])
	}
}

// A dir present on both sides enters the base regardless of mtime drift (dir/dir Equal
// is unconditional); a file↔dir type mismatch falls back to the previous base entry.
func TestMergedBaseDirectories(t *testing.T) {
	prev := Manifest{"typed": {Size: 1, ModTime: time.Unix(1, 0)}}
	postLocal := Manifest{
		"d":     {IsDir: true, ModTime: time.Unix(2, 0)},
		"typed": {IsDir: true, ModTime: time.Unix(2, 0)}, // dir locally...
	}
	postRemote := Manifest{
		"d":     {IsDir: true, ModTime: time.Unix(9, 0)}, // drifted mtime: still agreement
		"typed": {Size: 5, ModTime: time.Unix(5, 0)},     // ...file remotely
	}
	merged := mergedBase(prev, postLocal, postRemote, nil)
	if !merged["d"].IsDir {
		t.Errorf("agreeing dir must enter the base: %+v", merged["d"])
	}
	if merged["typed"].Size != 1 || merged["typed"].IsDir {
		t.Errorf("type-mismatched path must retain the previous base entry: %+v", merged["typed"])
	}
}

func TestMergedBaseDropsDeletedPaths(t *testing.T) {
	prev := Manifest{"del.txt": {Size: 1, ModTime: time.Unix(1, 0)}}
	// Deleted on both sides post-apply: absent from both manifests => out of the base.
	merged := mergedBase(prev, Manifest{}, Manifest{}, nil)
	if _, ok := merged["del.txt"]; ok {
		t.Error("path absent from both sides must be absent from merged base")
	}
}

func TestMergedBaseSkipsBothAddedDisagreement(t *testing.T) {
	// Present on both sides with different state and no previous base entry (Skip policy
	// on a both-added conflict): must stay out so it re-classifies as a conflict.
	postLocal := Manifest{"x.txt": {Size: 2, ModTime: time.Unix(2, 0)}}
	postRemote := Manifest{"x.txt": {Size: 3, ModTime: time.Unix(3, 0)}}
	merged := mergedBase(Manifest{}, postLocal, postRemote, nil)
	if _, ok := merged["x.txt"]; ok {
		t.Errorf("both-added disagreement must not enter the base: %+v", merged)
	}
}

func TestDeleteFromRemoteUsesRsyncDeleteMissingArgs(t *testing.T) {
	for name, ep := range map[string]Endpoint{
		"ssh":    {Path: "/remote", SSH: &SSH{Host: "h"}},
		"daemon": {Path: "sub", Daemon: &Daemon{Host: "h", Module: "m"}},
	} {
		t.Run(name, func(t *testing.T) {
			ar := &applyRunner{}
			s := &Syncer{Store: &memStore{}, run: ar.run}
			paths := []string{"a b.txt", "x$(id).txt", "odd\nname.txt"}
			deleted, skipped, err := s.deleteFrom(context.Background(), ep, paths, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(skipped) != 0 || len(deleted) != 3 {
				t.Fatalf("deleted=%v skipped=%v", deleted, skipped)
			}
			if len(ar.deletes) != 1 {
				t.Fatalf("want one rsync delete call, got %d: %v", len(ar.deletes), ar.deletes)
			}
			args := ar.deletes[0]
			for _, want := range []string{"--delete-missing-args", "--from0"} {
				if !slices.Contains(args, want) {
					t.Errorf("delete args missing %q: %v", want, args)
				}
			}
			// The paths travel via the NUL-separated file list, not the command line
			// (deepest-first sorted; these are all depth 0, so lexical).
			if want := "a b.txt\x00odd\nname.txt\x00x$(id).txt\x00"; ar.deletedLists[0] != want {
				t.Errorf("files-from content = %q, want %q", ar.deletedLists[0], want)
			}
		})
	}
}

func TestSortDeepestFirst(t *testing.T) {
	paths := []string{"a", "a/b/c.txt", "z.txt", "a/b", "b/x.txt"}
	sortDeepestFirst(paths)
	want := []string{"a/b/c.txt", "a/b", "b/x.txt", "a", "z.txt"}
	if !slices.Equal(paths, want) {
		t.Errorf("sorted = %v, want %v", paths, want)
	}
}

func TestSuppressDirDeletes(t *testing.T) {
	t0 := time.Unix(100, 0)
	dir := FileState{IsDir: true, ModTime: t0}
	file := FileState{Size: 1, ModTime: t0}

	// Everything under d is deleted too => d goes.
	m := Manifest{"d": dir, "d/a.txt": file}
	kept, sup := suppressDirDeletes([]string{"d", "d/a.txt"}, m, nil)
	if len(sup) != 0 || len(kept) != 2 {
		t.Errorf("fully emptied dir must be kept for deletion: kept=%v sup=%v", kept, sup)
	}

	// An untracked-by-the-plan live entry under d survives => d suppressed.
	m = Manifest{"d": dir, "d/keep.txt": file}
	kept, sup = suppressDirDeletes([]string{"d"}, m, nil)
	if len(kept) != 0 || !slices.Contains(sup, "d") {
		t.Errorf("dir with a surviving child must be suppressed: kept=%v sup=%v", kept, sup)
	}

	// An incoming transfer landing under d survives => d suppressed.
	m = Manifest{"d": dir}
	kept, sup = suppressDirDeletes([]string{"d"}, m, []string{"d/new.txt"})
	if len(kept) != 0 || !slices.Contains(sup, "d") {
		t.Errorf("dir with an incoming transfer must be suppressed: kept=%v sup=%v", kept, sup)
	}

	// A suppressed child dir keeps its parent suppressed (deepest-first cascade).
	m = Manifest{"p": dir, "p/q": dir, "p/q/keep.txt": file}
	kept, sup = suppressDirDeletes([]string{"p", "p/q"}, m, nil)
	if len(kept) != 0 || len(sup) != 2 {
		t.Errorf("suppression must cascade to ancestors: kept=%v sup=%v", kept, sup)
	}

	// File deletes pass through untouched.
	m = Manifest{"a.txt": file}
	kept, sup = suppressDirDeletes([]string{"a.txt"}, m, nil)
	if len(sup) != 0 || !slices.Contains(kept, "a.txt") {
		t.Errorf("file deletes must pass through: kept=%v sup=%v", kept, sup)
	}
}

// A local directory delete is guarded by emptiness, not size/mtime: one that gained
// content after planning is skipped (never recursively removed), an empty one goes,
// and children are removed before their parent within the same run.
func TestDeleteFromLocalDirectories(t *testing.T) {
	e := Endpoint{Path: t.TempDir()}
	for _, d := range []string{"gone/sub", "full"} {
		if err := os.MkdirAll(filepath.Join(e.Path, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(e.Path, "full", "untracked.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := FileState{IsDir: true, ModTime: time.Unix(100, 0)}
	planned := Manifest{"gone": dir, "gone/sub": dir, "full": dir}
	s := &Syncer{Store: &memStore{}, run: (&applyRunner{}).run}
	// Passed shallow-first on purpose: deleteFrom must reorder deepest-first.
	deleted, skipped, err := s.deleteFrom(context.Background(), e, []string{"gone", "full", "gone/sub"}, planned)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(deleted, "gone") || !slices.Contains(deleted, "gone/sub") {
		t.Errorf("emptied dirs must be deleted: %v", deleted)
	}
	if !slices.Contains(skipped, "full") {
		t.Errorf("non-empty dir must be skipped: %v", skipped)
	}
	if _, err := os.Stat(filepath.Join(e.Path, "gone")); !os.IsNotExist(err) {
		t.Error("gone/ must be removed from disk")
	}
	if _, err := os.Stat(filepath.Join(e.Path, "full", "untracked.txt")); err != nil {
		t.Errorf("untracked file must survive: %v", err)
	}
}

// A path planned as a dir but living as a file (or vice versa) is a type change: skip.
func TestDeleteFromLocalDirTypeChangeSkips(t *testing.T) {
	e := Endpoint{Path: t.TempDir()}
	if err := os.WriteFile(filepath.Join(e.Path, "d"), []byte("now a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	planned := Manifest{"d": {IsDir: true, ModTime: time.Unix(100, 0)}}
	s := &Syncer{Store: &memStore{}, run: (&applyRunner{}).run}
	deleted, skipped, err := s.deleteFrom(context.Background(), e, []string{"d"}, planned)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 0 || !slices.Contains(skipped, "d") {
		t.Errorf("type-changed path must be skipped: deleted=%v skipped=%v", deleted, skipped)
	}
	if _, err := os.Stat(filepath.Join(e.Path, "d")); err != nil {
		t.Errorf("the file must survive: %v", err)
	}
}

// Remote deletes split into one call for files plus one call per directory depth level,
// deepest first: rsync's --files-from needs an entry's parent present in the empty
// source, but a source dir that is itself an entry would be kept — so a dir and its
// subdir can never share a call.
func TestDeleteFromRemoteDirsPerDepthLevel(t *testing.T) {
	ar := &applyRunner{}
	s := &Syncer{Store: &memStore{}, run: ar.run}
	dir := FileState{IsDir: true, ModTime: time.Unix(100, 0)}
	file := FileState{Size: 1, ModTime: time.Unix(100, 0)}
	planned := Manifest{"p": dir, "p/q": dir, "p/q/x.txt": file, "top.txt": file}
	paths := []string{"p", "p/q", "p/q/x.txt", "top.txt"}
	deleted, skipped, err := s.deleteFrom(context.Background(), Endpoint{Path: "/r", SSH: &SSH{Host: "h"}}, paths, planned)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 || len(deleted) != 4 {
		t.Fatalf("deleted=%v skipped=%v", deleted, skipped)
	}
	want := []string{
		"p/q/x.txt\x00top.txt\x00", // files first, one call
		"p/q\x00",                  // then dirs deepest-first, one call per level
		"p\x00",
	}
	if !slices.Equal(ar.deletedLists, want) {
		t.Errorf("delete calls = %q, want %q", ar.deletedLists, want)
	}
}

// rsync reports a non-empty dir it refuses to delete on stdout with exit 0; the path
// must come back as skipped, not deleted.
func TestDeleteFromRemoteNonEmptyDirSkips(t *testing.T) {
	base := &applyRunner{}
	run := func(ctx context.Context, bin string, args []string, tee io.Writer) (runResult, error) {
		res, err := base.run(ctx, bin, args, tee)
		if slices.Contains(args, "--delete-missing-args") {
			res.stdout = "cannot delete non-empty directory: full\n"
		}
		return res, err
	}
	s := &Syncer{Store: &memStore{}, run: run}
	planned := Manifest{"full": {IsDir: true, ModTime: time.Unix(100, 0)}}
	deleted, skipped, err := s.deleteFrom(context.Background(), Endpoint{Path: "/r", SSH: &SSH{Host: "h"}}, []string{"full"}, planned)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 0 || !slices.Contains(skipped, "full") {
		t.Errorf("non-empty remote dir must be skipped: deleted=%v skipped=%v", deleted, skipped)
	}
}

func TestParseNonEmptyDirSkips(t *testing.T) {
	out := "some chatter\ncannot delete non-empty directory: a/b\r\ncannot delete non-empty directory: notrequested\n"
	got := parseNonEmptyDirSkips(out, []string{"a/b", "c"})
	if len(got) != 1 || got[0] != "a/b" {
		t.Errorf("skips = %v, want [a/b]", got)
	}
	if parseNonEmptyDirSkips("all fine\n", []string{"a"}) != nil {
		t.Error("no message => no skips")
	}
}

// The wipe valve judges files only: deleting every file is a wipe even when empty
// dirs remain planned or listed, and a dir-only cleanup is never a wipe.
func TestWouldWipeCountsFilesOnly(t *testing.T) {
	t0 := time.Unix(100, 0)
	m := Manifest{
		"a.txt": {Size: 1, ModTime: t0},
		"d":     {IsDir: true, ModTime: t0},
	}
	if !wouldWipe(m, []string{"a.txt"}, nil) {
		t.Error("deleting the only file is a wipe even though the dir entry stays")
	}
	if wouldWipe(m, []string{"d"}, nil) {
		t.Error("deleting only an empty dir is a cleanup, not a wipe")
	}
	if wouldWipe(m, []string{"a.txt"}, []string{"new.txt"}) {
		t.Error("incoming transfers still waive the wipe")
	}
}

func TestDeleteFromRemoteLargeSetIsOneCall(t *testing.T) {
	ar := &applyRunner{}
	s := &Syncer{Store: &memStore{}, run: ar.run}
	// Paths travel in a file, so even a huge set (way past ARG_MAX as command-line
	// arguments) is a single rsync invocation.
	paths := make([]string, 1000)
	for i := range paths {
		paths[i] = strings.Repeat("d", 100) + "/" + strconv.Itoa(i) + ".txt"
	}
	deleted, _, err := s.deleteFrom(context.Background(), Endpoint{Path: "/remote", SSH: &SSH{Host: "h"}}, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1000 {
		t.Fatalf("deleted %d of 1000", len(deleted))
	}
	if len(ar.deletes) != 1 {
		t.Fatalf("want one rsync delete call, got %d", len(ar.deletes))
	}
	if got := strings.Count(ar.deletedLists[0], "\x00"); got != 1000 {
		t.Errorf("file list entries = %d, want 1000", got)
	}
}
