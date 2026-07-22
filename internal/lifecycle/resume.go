package lifecycle

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/andresbott/dibs/internal/baseline"
	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/internal/ident"
	"github.com/andresbott/dibs/internal/marker"
	"github.com/andresbott/dibs/libs/threewayrsync"
)

// resumeCheckout adopts a local copy left behind by a checkin without --clean.
// The released baseline the checkin kept is the arbiter: every local entry must
// match it exactly (size+mtime; directories presence-only) — matching entries
// become the new baseline, base entries missing locally are pruned so the first
// sync re-pulls them, and ANY local drift refuses with the paths listed. The
// validation is purely local (one listing of the local tree, no remote round
// trip), so remote drift never blocks a resume: the kept base lets the first
// sync classify it correctly afterwards (edits pull, deletes mirror).
// The caller has already handled the marker (foreign refuses / self widens),
// so this only runs when no marker exists or Force stole it.
func (r Runner) resumeCheckout(ctx context.Context, rep Report, name string, p config.Profile, id ident.Ident, localRoot string, acc marker.Accessor, relpath string, opts Options) (Report, error) {
	// The released envelope is resumed as recorded; scoping a resume would
	// leave the out-of-scope part of the copy stranded outside the baseline.
	if normalizeRelpath(relpath) != "." {
		return rep, fmt.Errorf("--resume adopts the released envelope as recorded — drop the relpath (resume first, then widen with a plain checkout)")
	}
	st, hasState, err := baseline.Load(name)
	if err != nil {
		return rep, err
	}
	if !hasState || !st.IsReleased() {
		return rep, fmt.Errorf("profile %q has no released baseline to resume — use a plain checkout", name)
	}
	remoteRoot := config.ExpandRoot(p.RemoteRoot)
	if (st.LocalRoot != "" && st.LocalRoot != localRoot) || (st.RemoteRoot != "" && st.RemoteRoot != remoteRoot) {
		return rep, fmt.Errorf(
			"the released baseline of %q was recorded against different roots (local %s, remote %s) than now configured (local %s, remote %s) — remove the stale state and check out fresh",
			name, st.LocalRoot, st.RemoteRoot, localRoot, remoteRoot)
	}

	// List the local tree exactly as a sync would see it (same scope, marker
	// exclude, and ignore patterns), so the validation and the engine can
	// never disagree about what exists.
	localM, err := r.syncer(baseline.Store(name)).List(ctx, p.LocalEndpoint(), threewayrsync.Options{
		Scope:   st.Scope(),
		Exclude: marker.Exclude(),
		Ignore:  p.Ignore,
	})
	if err != nil {
		return rep, err
	}
	adopted, violations := adoptBaseline(st.Files, localM)
	if len(violations) > 0 {
		return rep, fmt.Errorf(
			"cannot resume %q: the local copy changed while released — restore or remove these paths and retry, or delete the copy and check out fresh:\n  %s",
			name, strings.Join(violations, "\n  "))
	}

	rep.Resumed = true
	if opts.DryRun {
		return rep, nil
	}

	released := *st // rollback copy: a failed claim must leave the token usable
	now := r.now()
	st.Files = adopted
	st.ReleasedAt = time.Time{}
	if err := baseline.Save(st); err != nil {
		return rep, err
	}
	m := &marker.Marker{
		CheckedOutBy: id.By,
		Profile:      name,
		ProfileID:    p.ID,
		Host:         id.Host,
		Relpaths:     st.Relpaths,
		CheckedOutAt: now,
		LastSyncAt:   now,
		ToolVersion:  r.ToolVersion,
	}
	if err := acc.Write(ctx, m); err != nil {
		_ = baseline.Save(&released)
		return rep, err
	}
	if err := verifyClaim(ctx, acc, name, id, p.ID); err != nil {
		_ = baseline.Save(&released)
		return rep, err
	}
	rep.Marker = m
	return rep, nil
}

// adoptBaseline validates the local manifest against the released base and
// derives the baseline a resumed checkout starts from. Entries present on both
// sides with equal state are adopted (the local state, i.e. what is actually on
// disk). Base entries missing locally are pruned — the copy shrank while
// released, and the first sync harmlessly re-pulls them. A local entry that is
// absent from the base or differs from it is a violation: the copy was touched
// while nothing held the lock, and adopting it could either overwrite the
// remote or silently lose the local edit — only the user can arbitrate.
func adoptBaseline(base, local threewayrsync.Manifest) (adopted threewayrsync.Manifest, violations []string) {
	adopted = threewayrsync.Manifest{}
	for path, lst := range local {
		bst, inBase := base[path]
		switch {
		case !inBase:
			violations = append(violations, path+" (added while released)")
		case !lst.Equal(bst):
			violations = append(violations, path+" (modified while released)")
		default:
			adopted[path] = lst
		}
	}
	sort.Strings(violations)
	return adopted, violations
}

// resumeHint decorates a plain checkout's vacancy refusal with the --resume
// pointer when a released token exists for this profile against the same local
// root — exactly the situation --resume exists for. Any load error or mismatch
// leaves the original refusal untouched.
func resumeHint(vacancyErr error, name, localRoot string) error {
	st, ok, err := baseline.Load(name)
	if err != nil || !ok || !st.IsReleased() {
		return vacancyErr
	}
	if st.LocalRoot != "" && st.LocalRoot != localRoot {
		return vacancyErr
	}
	return fmt.Errorf("%w — or run 'dibs checkout %s --resume' to adopt the copy left by the last checkin", vacancyErr, name)
}
