package lifecycle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/andresbott/netcheckout/internal/baseline"
	"github.com/andresbott/netcheckout/internal/config"
	"github.com/andresbott/netcheckout/internal/ident"
	"github.com/andresbott/netcheckout/internal/marker"
	"github.com/andresbott/netcheckout/libs/threewayrsync"
)

// Checkout locks a profile: it verifies the remote is reachable and not already
// held elsewhere, refuses to lock over a non-empty local target, then records an
// empty baseline and writes the per-profile marker. It copies NO files — pulling
// the remote down is sync's job. relpath scopes the recorded relpaths (and thus
// what the first sync reconciles); omitted, the declared subpaths (or the whole
// root) are recorded (GOALS §8). The lock is always the whole profile. An
// existing foreign marker refuses unless Force; a marker already held by THIS
// machine widens the recorded relpath set under the same lock (GOALS §5).
func (r Runner) Checkout(ctx context.Context, name string, p config.Profile, id ident.Ident, relpath string, opts Options) (Report, error) {
	rep := Report{Action: "checkout", DryRun: opts.DryRun}
	remote, err := p.RemoteEndpoint()
	if err != nil {
		return rep, err
	}
	localRoot := config.ExpandRoot(p.LocalRoot)

	// A mounted-share remote gets the cheap stat check; a URL remote is probed by
	// the marker read below, which already crosses the transport.
	if p.RemoteIsLocalPath() {
		if info, err := os.Stat(remote.Path); err != nil || !info.IsDir() {
			return rep, fmt.Errorf("remote root %s is not mounted", remote.Path)
		}
	}

	// An explicit relpath must lie inside the declared subpaths (when any are
	// declared): checking out an undeclared subtree would pull content the
	// unlisted-local guard then flags on every subsequent sync/checkin — a
	// wedged state whose tempting exit (delete the local files) propagates the
	// deletions to the remote.
	if rel := normalizeRelpath(relpath); rel != "." && len(p.Subpaths) > 0 && !relpathCovered(rel, p.Subpaths) {
		return rep, fmt.Errorf("relpath %q is not inside the declared subpaths of %q (%s) — add it to the profile's subpaths first",
			rel, name, strings.Join(p.Subpaths, ", "))
	}

	acc := r.accessor(remote)
	existing, exists, err := acc.Read(ctx)
	if err != nil {
		return rep, err
	}
	if exists && existing.OwnedBy(id.By, id.Host) {
		return r.widenCheckout(ctx, rep, name, localRoot, acc, existing, relpath)
	}
	if exists && !opts.Force {
		return rep, fmt.Errorf("profile %q is checked out by %s on %s since %s (use --force to override)",
			name, existing.CheckedOutBy, existing.Host, existing.CheckedOutAt.Format("2006-01-02 15:04"))
	}

	relpaths := checkoutRelpaths(p, relpath)

	// Refuse to lock over an existing local copy. This guard is absolute: unlike
	// the lock check, --force does not bypass it. Keeping the targets empty means
	// the first sync after checkout is an unambiguous pull, not a wall of
	// "added on both sides" conflicts against the empty baseline recorded below.
	for _, rel := range relpaths {
		if err := ensureLocalTargetVacant(filepath.Join(localRoot, rel), name); err != nil {
			return rep, err
		}
	}

	// Dry-run: the checks passed and nothing is written. Checkout copies no files,
	// so there is nothing to preview beyond "would write the marker".
	if opts.DryRun {
		return rep, nil
	}

	// Record an EMPTY baseline. The local targets are vacant (guard above), so
	// there is nothing to snapshot; the empty manifest makes the first sync
	// classify every remote file as a fresh pull (never as a delete against a
	// phantom snapshot).
	now := r.now()
	st := &baseline.State{
		Profile:    name,
		Relpaths:   relpaths,
		LocalRoot:  localRoot,
		RemoteRoot: config.ExpandRoot(p.RemoteRoot),
		Files:      threewayrsync.Manifest{},
		LastSyncAt: now,
	}
	if err := baseline.Save(st); err != nil {
		return rep, err
	}

	m := &marker.Marker{
		CheckedOutBy: id.By,
		Profile:      name,
		Host:         id.Host,
		Relpaths:     relpaths,
		CheckedOutAt: now,
		LastSyncAt:   now,
		ToolVersion:  r.ToolVersion,
	}
	if err := acc.Write(ctx, m); err != nil {
		_ = baseline.Remove(name) // roll back the fresh checkout's baseline
		return rep, err
	}
	if err := verifyClaim(ctx, acc, name, id); err != nil {
		_ = baseline.Remove(name)
		return rep, err
	}
	rep.Marker = m
	return rep, nil
}

// verifyClaim re-reads the marker just written and confirms it is still ours.
// The marker write is read-check-then-write on a shared filesystem, so two
// machines checking out simultaneously can both pass the "no marker" check and
// both write — last write wins silently. The read-back shrinks that window to
// the gap between the two writes (it cannot be fully closed over SMB/rsync).
// The loser learns who holds the lock; the caller rolls its baseline back.
func verifyClaim(ctx context.Context, acc marker.Accessor, name string, id ident.Ident) error {
	verify, found, err := acc.Read(ctx)
	if err != nil {
		return fmt.Errorf("checkout of %q may not have taken — verifying the marker failed: %w (re-run checkout)", name, err)
	}
	if !found {
		return fmt.Errorf("checkout of %q did not take — the marker vanished after writing (re-run checkout)", name)
	}
	if !verify.OwnedBy(id.By, id.Host) {
		return fmt.Errorf("profile %q was checked out by %s on %s at the same time — the other checkout won",
			name, verify.CheckedOutBy, verify.Host)
	}
	return nil
}

// checkoutRelpaths resolves the relpath set a checkout records: the explicit
// relpath when given, else every declared subpath, else the whole root
// (GOALS §8: "relpath omitted = all declared subpaths, or the whole root").
func checkoutRelpaths(p config.Profile, relpath string) []string {
	if rel := normalizeRelpath(relpath); rel != "." {
		return []string{rel}
	}
	if len(p.Subpaths) > 0 {
		out := make([]string, 0, len(p.Subpaths))
		for _, s := range p.Subpaths {
			out = append(out, normalizeRelpath(s))
		}
		return out
	}
	return []string{"."}
}

// widenCheckout handles a checkout of a profile THIS machine already holds:
// per GOALS §5/§8 step 3 it grows the recorded relpath set under the same lock.
// A relpath already covered by the held set (or no relpath at all) is refused —
// there is nothing to widen; sync is the right command. The existing baseline
// manifest is preserved: only the relpath envelope grows, so the next sync
// classifies the new subtree against an empty slice of the base (fresh pulls).
func (r Runner) widenCheckout(ctx context.Context, rep Report, name, localRoot string, acc marker.Accessor, m *marker.Marker, relpath string) (Report, error) {
	rel := normalizeRelpath(relpath)
	st, hasState, err := baseline.Load(name)
	if err != nil {
		return rep, err
	}
	if !hasState {
		return rep, fmt.Errorf("profile %q is checked out on this machine but has no local baseline — remove the marker by hand and re-checkout", name)
	}
	if rel == "." || relpathCovered(rel, st.Relpaths) {
		return rep, fmt.Errorf("profile %q is already checked out on this machine — use sync to reconcile", name)
	}
	// The new subtree must be vacant locally, same as a fresh checkout target.
	if err := ensureLocalTargetVacant(filepath.Join(localRoot, rel), name); err != nil {
		return rep, err
	}
	if rep.DryRun {
		return rep, nil
	}

	prevRelpaths := st.Relpaths
	st.Relpaths = append(append([]string(nil), st.Relpaths...), rel)
	if err := baseline.Save(st); err != nil {
		return rep, err
	}
	m.Relpaths = st.Relpaths
	if err := acc.Write(ctx, m); err != nil {
		st.Relpaths = prevRelpaths // roll the widening back; the checkout stays as it was
		_ = baseline.Save(st)
		return rep, err
	}
	rep.Marker = m
	return rep, nil
}

// relpathCovered reports whether rel is already inside the held relpath set: a
// whole-root entry covers everything; otherwise an exact or ancestor match.
func relpathCovered(rel string, held []string) bool {
	for _, h := range held {
		h = normalizeRelpath(h)
		if h == "." || rel == h || strings.HasPrefix(rel, h+"/") {
			return true
		}
	}
	return false
}

// ensureLocalTargetVacant returns an error if the checkout destination already
// holds content: a plain file, or a non-empty directory blocks. An empty
// directory or a missing path is fine; any other stat failure (e.g. permission
// denied) fails closed — an unreadable path is not proof of vacancy. This is
// the absolute local-copy guard — callers must not let --force bypass it.
func ensureLocalTargetVacant(dst, name string) error {
	info, err := os.Stat(dst)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // missing path — nothing to clobber
		}
		return fmt.Errorf("cannot verify local target %s is empty: %w", dst, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("local target %s already exists — remove it before checking out %q", dst, name)
	}
	entries, err := os.ReadDir(dst)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("local target %s is not empty — remove its contents before checking out %q", dst, name)
	}
	return nil
}
