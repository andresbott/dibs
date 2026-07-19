package threewayrsync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// ConflictError reports that some paths changed on both sides while the policy was Abort.
type ConflictError struct{ Paths []string }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%d conflicting path(s) changed on both sides", len(e.Paths))
}


// WouldWipeError reports a plan that would leave one endpoint with zero in-scope files
// while the base records previously synced ones. Deleting everything that was ever
// synced — with nothing transferring in to replace it — is almost never a real cleanup:
// it is what "the user emptied the other side to re-pull fresh" (or a share that went
// bad past the other valves) looks like. Nothing waives it: a full wipe is never applied.
type WouldWipeError struct {
	Side    string // "local" | "remote": the endpoint that would end up empty
	Deletes int
}

func (e *WouldWipeError) Error() string {
	return fmt.Sprintf("refusing to delete all %d previously synced file(s) on the %s side — the other side lists none of them; a full wipe is never applied (release and re-checkout to start over)", e.Deletes, e.Side)
}

// wouldWipe reports whether applying the plan leaves an endpoint with zero in-scope
// files: every file it currently lists is planned for deletion and no transfer lands on
// it to repopulate. Only files count, on both sides of the comparison — a side left
// holding nothing but empty directories is still wiped, and a plan that only removes
// empty directories is a cleanup, not a wipe. A whole-tree rename (delete-all + add-all)
// has incoming transfers and passes; an emptied opposite side has none and is caught.
func wouldWipe(m Manifest, deletes, incoming []string) bool {
	files := 0
	for _, p := range deletes {
		if !m[p].IsDir {
			files++
		}
	}
	return files > 0 && len(incoming) == 0 && files >= countFiles(m)
}

// Result is the outcome of a Sync.
type Result struct {
	Applied              Plan     // buckets actually applied
	Conflicts            []string // conflicts left unresolved (Skip policy, deletes skipped because the file changed after planning, or directory deletes skipped because the directory would not be empty)
	SkippedLocalDeletes  []string // planned local deletions not applied (Options.AllowDeletes off)
	SkippedRemoteDeletes []string // planned remote deletions not applied (Options.AllowDeletes off)
	Ignored              []string // paths matching Options.Ignore — enumerated but never touched
	BaseSaved            bool
}

// suppressDirDeletes lifts out of deletes every directory that would not be empty once
// the plan runs: a dir with a survivor underneath — an incoming transfer, or a live
// manifest entry not itself planned for deletion (an untracked file, an unresolved
// conflict, a suppressed deeper dir) — cannot be removed, and attempting it would only
// fail at apply time. Directories are decided deepest-first so a suppressed child keeps
// its parent suppressed. m is the manifest of the endpoint the deletes target; incoming
// is the transfer list landing on that endpoint. Suppressed dirs are reported like
// guard-skipped deletes: unresolved, resurfacing next run.
func suppressDirDeletes(deletes []string, m Manifest, incoming []string) (kept, suppressed []string) {
	deleting := make(map[string]bool, len(deletes))
	var dirs []string
	for _, p := range deletes {
		deleting[p] = true
		if m[p].IsDir {
			dirs = append(dirs, p)
		}
	}
	if len(dirs) == 0 {
		return deletes, nil
	}
	sortDeepestFirst(dirs)
	for _, d := range dirs {
		prefix := d + "/"
		survives := false
		for _, p := range incoming {
			if strings.HasPrefix(p, prefix) {
				survives = true
				break
			}
		}
		if !survives {
			for p := range m {
				if strings.HasPrefix(p, prefix) && !deleting[p] {
					survives = true
					break
				}
			}
		}
		if survives {
			deleting[d] = false // a kept dir is itself a survivor for its ancestors
			suppressed = append(suppressed, d)
		}
	}
	for _, p := range deletes {
		if deleting[p] {
			kept = append(kept, p)
		}
	}
	return kept, suppressed
}

// sortDeepestFirst orders paths by depth descending (then lexically): every path under a
// directory is deeper than the directory itself, so this guarantees children are deleted
// before their parent and the parent is empty when its turn comes.
func sortDeepestFirst(paths []string) {
	sort.Slice(paths, func(i, j int) bool {
		di, dj := strings.Count(paths[i], "/"), strings.Count(paths[j], "/")
		if di != dj {
			return di > dj
		}
		return paths[i] < paths[j]
	})
}

// Sync enumerates, classifies, applies per the conflict policy, and commits the new base
// once at the end. It is safe to cancel via ctx and to re-run to resume: nothing is
// committed until every operation succeeds. When the Store implements Locker, the whole
// run holds the lock; a concurrent Sync fails fast with ErrLocked.
func (s *Syncer) Sync(ctx context.Context, local, remote Endpoint, opts Options) (Result, error) {
	scope, err := normalizeScope(opts.Scope)
	if err != nil {
		return Result{}, err
	}
	opts.Scope = scope
	ignore, err := normalizeIgnore(opts.Ignore)
	if err != nil {
		return Result{}, err
	}
	opts.Ignore = ignore

	if l, ok := s.Store.(Locker); ok {
		release, err := l.TryLock()
		if err != nil {
			return Result{}, err
		}
		defer release()
	}

	plan, base, localM, remoteM, err := s.computePlan(ctx, local, remote, opts)
	if err != nil {
		return Result{}, err
	}

	pull, push, unresolved, err := resolveConflicts(&plan, localM, remoteM, opts.Conflict)
	if err != nil {
		return Result{}, err
	}

	// The delete gate: with AllowDeletes off nothing is ever deleted — the planned
	// deletions (including conflict-routed ones from resolveConflicts) are lifted
	// out of the plan and reported as skipped; the base keeps their entries, so
	// they stay pending. With the flag on, a plan that would fully wipe one side
	// is refused before anything runs, and a directory delete that would not
	// leave the directory empty is lifted out and reported unresolved.
	var skippedLocal, skippedRemote []string
	if opts.AllowDeletes {
		if err := checkWipeValve(&plan, localM, remoteM, pull, push); err != nil {
			return Result{}, err
		}
		var supLocal, supRemote []string
		plan.RemoteDeletes, supRemote = suppressDirDeletes(plan.RemoteDeletes, remoteM, push)
		plan.LocalDeletes, supLocal = suppressDirDeletes(plan.LocalDeletes, localM, pull)
		unresolved = append(unresolved, supRemote...)
		unresolved = append(unresolved, supLocal...)
	} else {
		skippedLocal, skippedRemote = plan.LocalDeletes, plan.RemoteDeletes
		plan.LocalDeletes, plan.RemoteDeletes = nil, nil
	}

	var applied Plan
	if len(pull) > 0 {
		if err := s.pullInto(ctx, remote, local, pull, opts); err != nil {
			return Result{Applied: applied, Conflicts: unresolved, SkippedLocalDeletes: skippedLocal, SkippedRemoteDeletes: skippedRemote, Ignored: plan.Ignored}, err
		}
		applied.Pull = pull
	}
	if len(push) > 0 {
		if err := s.transfer(ctx, local, remote, push, opts, "push"); err != nil {
			return Result{Applied: applied, Conflicts: unresolved, SkippedLocalDeletes: skippedLocal, SkippedRemoteDeletes: skippedRemote, Ignored: plan.Ignored}, err
		}
		applied.Push = push
	}
	rDeleted, rSkipped, err := s.deleteFrom(ctx, remote, plan.RemoteDeletes, remoteM)
	if err != nil {
		return Result{Applied: applied, Conflicts: unresolved, SkippedLocalDeletes: skippedLocal, SkippedRemoteDeletes: skippedRemote, Ignored: plan.Ignored}, err
	}
	applied.RemoteDeletes = rDeleted
	for _, rel := range rDeleted {
		emit(opts.OnEvent, Event{Op: "delete-remote", Path: rel})
	}
	lDeleted, lSkipped, err := s.deleteFrom(ctx, local, plan.LocalDeletes, localM)
	if err != nil {
		return Result{Applied: applied, Conflicts: unresolved, SkippedLocalDeletes: skippedLocal, SkippedRemoteDeletes: skippedRemote, Ignored: plan.Ignored}, err
	}
	applied.LocalDeletes = lDeleted
	for _, rel := range lDeleted {
		emit(opts.OnEvent, Event{Op: "delete-local", Path: rel})
	}
	unresolved = append(unresolved, rSkipped...)
	unresolved = append(unresolved, lSkipped...)
	sort.Strings(unresolved)

	postLocal, postRemote, err := s.postApplyManifests(ctx, local, remote, applied, localM, remoteM, opts)
	if err != nil {
		return Result{Applied: applied, Conflicts: unresolved, SkippedLocalDeletes: skippedLocal, SkippedRemoteDeletes: skippedRemote, Ignored: plan.Ignored}, err
	}
	merged := mergedBase(base, postLocal, postRemote, opts.Scope)
	if err := s.Store.SaveBase(merged); err != nil {
		return Result{Applied: applied, Conflicts: unresolved, SkippedLocalDeletes: skippedLocal, SkippedRemoteDeletes: skippedRemote, Ignored: plan.Ignored}, err
	}
	return Result{Applied: applied, Conflicts: unresolved, SkippedLocalDeletes: skippedLocal, SkippedRemoteDeletes: skippedRemote, Ignored: plan.Ignored, BaseSaved: true}, nil
}

// checkWipeValve refuses a plan that empties an endpoint of every in-scope file
// it lists — no floor, no fraction, no waiver. Deleting 100% of what was
// previously synced, with nothing transferring in, is "the other side was
// emptied", not a cleanup: the recovery for a deliberate start-over is to
// release the checkout and check out again. It only runs when deletions will
// actually execute (Options.AllowDeletes); with the flag off every delete is
// skipped, so a wiped-looking side is harmless.
func checkWipeValve(plan *Plan, localM, remoteM Manifest, pull, push []string) error {
	if wouldWipe(remoteM, plan.RemoteDeletes, push) {
		return &WouldWipeError{Side: "remote", Deletes: len(plan.RemoteDeletes)}
	}
	if wouldWipe(localM, plan.LocalDeletes, pull) {
		return &WouldWipeError{Side: "local", Deletes: len(plan.LocalDeletes)}
	}
	return nil
}

// postApplyManifests returns the manifests to derive the new base from: a fresh listing
// of both sides when anything was applied — mtime granularity differences (e.g. a
// FAT-formatted share) would otherwise bake a state into base that matches neither side —
// or the pre-transfer manifests unchanged when nothing was, since they still are the
// live state.
func (s *Syncer) postApplyManifests(ctx context.Context, local, remote Endpoint, applied Plan, localM, remoteM Manifest, opts Options) (Manifest, Manifest, error) {
	if len(applied.Pull)+len(applied.Push)+len(applied.LocalDeletes)+len(applied.RemoteDeletes) == 0 {
		return localM, remoteM, nil
	}
	postLocal, err := s.list(ctx, local, opts.Exclude, opts.Scope)
	if err != nil {
		return nil, nil, err
	}
	postRemote, err := s.list(ctx, remote, opts.Exclude, opts.Scope)
	if err != nil {
		return nil, nil, err
	}
	// The re-listings see ignored paths again (only the plan filtered them);
	// partition here too so the merged base can never absorb one.
	postLocal, _ = partitionIgnored(postLocal, opts.Ignore)
	postRemote, _ = partitionIgnored(postRemote, opts.Ignore)
	return postLocal, postRemote, nil
}

// resolveConflicts folds the plan's conflicts into the work buckets per the policy and
// returns the final sorted pull/push lists plus the conflicts left unresolved. Under
// Abort any conflict is an error. PreferLocal/PreferRemote route a conflict either to a
// transfer or — when the preferred side deleted the path — to a delete on the other side
// (a transfer would no-op and resurrect it next run).
func resolveConflicts(plan *Plan, localM, remoteM Manifest, policy ConflictPolicy) (pull, push, unresolved []string, err error) {
	pull = append([]string(nil), plan.Pull...)
	push = append([]string(nil), plan.Push...)
	switch policy {
	case Abort:
		if len(plan.Conflicts) > 0 {
			return nil, nil, nil, &ConflictError{Paths: plan.Conflicts}
		}
	case Skip:
		unresolved = append(unresolved, plan.Conflicts...)
	case PreferLocal:
		for _, p := range plan.Conflicts {
			if _, ok := localM[p]; ok {
				push = append(push, p)
			} else {
				plan.RemoteDeletes = append(plan.RemoteDeletes, p)
			}
		}
	case PreferRemote:
		for _, p := range plan.Conflicts {
			if _, ok := remoteM[p]; ok {
				pull = append(pull, p)
			} else {
				plan.LocalDeletes = append(plan.LocalDeletes, p)
			}
		}
	}
	sort.Strings(pull)
	sort.Strings(push)
	sort.Strings(plan.RemoteDeletes)
	sort.Strings(plan.LocalDeletes)
	return pull, push, unresolved, nil
}

// pullInto transfers remote → local, materializing the local working copy first
// when it does not exist yet (fresh checkout): computePlan treated the missing
// dir as an empty tree, and rsync needs a real destination to write into.
func (s *Syncer) pullInto(ctx context.Context, remote, local Endpoint, pull []string, opts Options) error {
	if !local.remote() {
		if err := os.MkdirAll(local.Path, 0o755); err != nil { //nolint:gosec // G301: the local working copy is user content, not private state.
			return err
		}
	}
	return s.transfer(ctx, remote, local, pull, opts, "pull")
}

// transfer runs one rsync transfer from src to dst restricted to files, emitting a
// progress Event per itemized path when opts.OnEvent is set.
func (s *Syncer) transfer(ctx context.Context, src, dst Endpoint, files []string, opts Options, op string) error {
	listPath, err := writeFileList(files)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(listPath) }()
	args := withFilesFrom(buildTransferArgs(src, dst, opts.Checksum, opts.Exclude, opts.Scope), listPath)
	var tee io.Writer
	if opts.OnEvent != nil {
		tee = &itemizeWriter{onPath: func(p string) { opts.OnEvent(Event{Op: op, Path: p}) }}
	}
	res, err := s.runner()(ctx, s.bin(), args, tee)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &Error{Op: op, Args: args, Stderr: res.stderr, ExitCode: res.exitCode, Err: err}
	}
	return nil
}

// deleteFrom removes paths from an endpoint, deepest-first so a directory's tracked
// children go before the directory itself. On a filesystem endpoint each delete is
// guarded: a file is removed only while it still matches the state the plan was
// computed from — a file edited between enumeration and apply is skipped and reported
// (returned in skipped, resurfacing as a delete-vs-edit conflict next run) instead of
// destroyed. A directory is presence-only: the guard is that the path is still a
// directory, and one that turns out non-empty (it gained untracked content after
// planning) is skipped, never recursively removed. A remote endpoint (ssh or daemon)
// has no cheap stat+remove roundtrip, so its deletes run unguarded through rsync (see
// deleteRemote); that enumerate→delete window is a documented limitation — except
// non-empty directories, which rsync itself refuses to remove and are reported skipped.
// A path already gone counts as deleted, so re-running after a partial delete is
// idempotent.
func (s *Syncer) deleteFrom(ctx context.Context, e Endpoint, paths []string, planned Manifest) (deleted, skipped []string, err error) {
	if len(paths) == 0 {
		return nil, nil, nil
	}
	paths = append([]string(nil), paths...)
	sortDeepestFirst(paths)
	if e.remote() {
		return s.deleteRemoteAll(ctx, e, paths, planned)
	}
	return deleteLocal(e.Path, paths, planned)
}

// deleteLocal removes paths under a filesystem root with the per-path guards described
// on deleteFrom: "deleted" | "skipped" per path, error only on an unexpected failure.
func deleteLocal(root string, paths []string, planned Manifest) (deleted, skipped []string, err error) {
	for _, rel := range paths {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		st, statErr := os.Lstat(abs)
		switch {
		case statErr != nil && os.IsNotExist(statErr):
			deleted = append(deleted, rel)
			continue
		case statErr != nil:
			return deleted, skipped, statErr
		}
		want, ok := planned[rel]
		if !ok || st.IsDir() != want.IsDir {
			skipped = append(skipped, rel)
			continue
		}
		// The manifest's mtime has one-second resolution (rsync %M); compare at that
		// grain. Directories are presence-only: no size/mtime guard.
		if !want.IsDir && (st.Size() != want.Size || !st.ModTime().Truncate(time.Second).Equal(want.ModTime.Truncate(time.Second))) {
			skipped = append(skipped, rel)
			continue
		}
		if rmErr := os.Remove(abs); rmErr != nil {
			if os.IsNotExist(rmErr) {
				deleted = append(deleted, rel)
				continue
			}
			// A directory that gained content after planning: skip, don't destroy.
			if want.IsDir && errors.Is(rmErr, syscall.ENOTEMPTY) {
				skipped = append(skipped, rel)
				continue
			}
			return deleted, skipped, rmErr
		}
		deleted = append(deleted, rel)
	}
	return deleted, skipped, nil
}

// deleteRemoteAll deletes paths on a remote endpoint: all files in one rsync call, then
// directories one depth level at a time, deepest first. The split is forced by rsync's
// --files-from semantics: an entry's parent must exist in the empty source directory
// (else the entry "vanishes" on the sender and the deletion is silently skipped, exit
// 24), but a directory present in the source is treated as alive and is NOT deleted —
// so a dir and its subdir can never travel in the same call. Levels are few in
// practice. rsync refuses to delete a non-empty directory (stdout "cannot delete
// non-empty directory", exit 0); those come back in skipped and resurface next run.
func (s *Syncer) deleteRemoteAll(ctx context.Context, e Endpoint, paths []string, planned Manifest) (deleted, skipped []string, err error) {
	var files []string
	byDepth := map[int][]string{}
	var depths []int
	for _, rel := range paths {
		if !planned[rel].IsDir {
			files = append(files, rel)
			continue
		}
		d := strings.Count(rel, "/")
		if _, ok := byDepth[d]; !ok {
			depths = append(depths, d)
		}
		byDepth[d] = append(byDepth[d], rel)
	}
	splitKept := func(batch, kept []string) {
		keptSet := make(map[string]bool, len(kept))
		for _, p := range kept {
			keptSet[p] = true
		}
		for _, p := range batch {
			if keptSet[p] {
				skipped = append(skipped, p)
			} else {
				deleted = append(deleted, p)
			}
		}
	}
	if len(files) > 0 {
		kept, err := s.deleteRemote(ctx, e, files)
		if err != nil {
			return nil, nil, err
		}
		splitKept(files, kept)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(depths)))
	for _, d := range depths {
		kept, err := s.deleteRemote(ctx, e, byDepth[d])
		if err != nil {
			return deleted, skipped, err
		}
		splitKept(byDepth[d], kept)
	}
	return deleted, skipped, nil
}

// deleteRemote deletes the given relative paths on a remote endpoint with a single rsync
// run: a file-less source directory plus --delete-missing-args turns each --files-from
// entry into a deletion request on the destination. One rsync-native mechanism covers ssh
// and daemon endpoints alike (a daemon has no shell to run rm through), with no ARG_MAX
// concern — the paths travel in a NUL-separated file list, not on the command line. The
// source must contain each entry's parent directory: --files-from walks the parents on
// the sending side, and a missing one is a "vanished file" that silently skips the
// deletion (observed with rsync 3.2.7, exit 24). The caller must therefore never mix a
// directory entry with entries beneath it (see deleteRemoteAll). kept lists the entries
// rsync refused to delete because they are non-empty directories (reported on stdout,
// exit 0); a message that fails to parse is treated as deleted — the next run's listing
// re-plans a survivor, so the failure mode is a benign retry, never data loss.
func (s *Syncer) deleteRemote(ctx context.Context, e Endpoint, paths []string) (kept []string, err error) {
	empty, err := os.MkdirTemp("", "threewayrsync-empty-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(empty) }()
	for _, rel := range paths {
		if dir := filepath.Dir(filepath.FromSlash(rel)); dir != "." {
			if err := os.MkdirAll(filepath.Join(empty, dir), 0o700); err != nil {
				return nil, err
			}
		}
	}
	listPath, err := writeFileList(paths)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(listPath) }()
	args := withFilesFrom(buildDeleteArgs(empty, e), listPath)
	res, err := s.runner()(ctx, s.bin(), args, nil)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &Error{Op: "delete", Args: args, Stderr: res.stderr, ExitCode: res.exitCode, Err: err}
	}
	return parseNonEmptyDirSkips(res.stdout, paths), nil
}

// parseNonEmptyDirSkips extracts from rsync stdout the paths it refused to delete
// because the directory is not empty ("cannot delete non-empty directory: <path>"),
// keeping only ones that were actually requested.
func parseNonEmptyDirSkips(stdout string, paths []string) []string {
	const msg = "cannot delete non-empty directory: "
	if !strings.Contains(stdout, msg) {
		return nil
	}
	requested := make(map[string]bool, len(paths))
	for _, p := range paths {
		requested[p] = true
	}
	var kept []string
	for _, line := range strings.Split(stdout, "\n") {
		if p, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), msg); ok && requested[p] {
			kept = append(kept, p)
		}
	}
	return kept
}

// mergedBase derives the new base from the two post-apply manifests: a path enters the
// base only where both sides agree (size+mtime equal) — that agreement is what "last
// synced state" means. Where the sides still disagree (unresolved Skip conflicts, deletes
// skipped by the guard, mtime drift), the previous base entry is kept so the next run
// re-classifies the path and either re-applies or reports it; a disagreeing path with no
// previous base entry stays out and surfaces as a both-added conflict next run.
//
// A scoped sync's listings only see the in-scope part of the tree, so every out-of-scope
// previous base entry is carried forward unchanged — dropping them would make the next
// unscoped sync misread all of them as both-side adds.
func mergedBase(prev, postLocal, postRemote Manifest, scope []string) Manifest {
	merged := make(Manifest, len(postLocal))
	for p, lst := range postLocal {
		if rst, ok := postRemote[p]; ok && lst.Equal(rst) {
			merged[p] = lst
			continue
		}
		if bst, ok := prev[p]; ok {
			merged[p] = bst
		}
	}
	for p := range postRemote {
		if _, ok := postLocal[p]; ok {
			continue
		}
		if bst, ok := prev[p]; ok {
			merged[p] = bst
		}
	}
	if len(scope) > 0 {
		for p, bst := range prev {
			if !inScope(p, scope) {
				merged[p] = bst
			}
		}
	}
	return merged
}

func emit(onEvent func(Event), e Event) {
	if onEvent != nil {
		onEvent(e)
	}
}
