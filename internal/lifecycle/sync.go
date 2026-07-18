package lifecycle

import (
	"context"
	"errors"
	"fmt"

	"github.com/andresbott/netcheckout/internal/baseline"
	"github.com/andresbott/netcheckout/internal/config"
	"github.com/andresbott/netcheckout/internal/ident"
	"github.com/andresbott/netcheckout/internal/marker"
	"github.com/andresbott/netcheckout/libs/threewayrsync"
)

// ConflictError reports that some paths changed on both sides and the sync
// stopped without writing either one.
type ConflictError struct{ Paths []string }

func (e *ConflictError) Error() string {
	return (&threewayrsync.ConflictError{Paths: e.Paths}).Error()
}

// engineOptions assembles the threewayrsync options for one action run: the
// scope, the conflict policy (--force resolves local-wins), the delete gate,
// and the progress bridge. base is the manifest the add/modify verb is judged
// against. Deletions only execute when the user passed --allow-deletes for
// this run; otherwise the engine skips them and reports them as pending.
func engineOptions(pf profilePlan, opts Options, base threewayrsync.Manifest) threewayrsync.Options {
	policy := threewayrsync.Abort
	if opts.Force {
		policy = threewayrsync.PreferLocal
	}
	// The marker (the cooperative lock at the remote root) is metadata, not content:
	// it must never be pulled, pushed, or counted as a discrepancy. AcceptEmpty
	// defuses the engine's unmounted-share valve so that an emptied side is planned
	// (not an immediate error): without --allow-deletes the planned deletions are
	// skipped harmlessly, and with it the engine's wipe valve refuses any plan that
	// would delete 100% of the previously synced files on one side — nothing waives
	// that. The other dangerous cases keep their own guards: the remote root
	// provably exists (the preflight read our marker off it, which an unmounted
	// mountpoint cannot contain), and a MISSING local root over a non-empty
	// baseline is a hard engine error.
	eo := threewayrsync.Options{
		Scope:        pf.scope,
		Conflict:     policy,
		Exclude:      marker.Exclude(),
		AcceptEmpty:  true,
		AllowDeletes: opts.AllowDeletes,
	}
	if opts.OnApply != nil {
		eo.OnEvent = func(e threewayrsync.Event) { opts.OnApply(translateEvent(e, base)) }
	}
	return eo
}

// guardError carries a user-facing rewrite of an engine guard error while
// preserving the original in the chain, so callers (the TUI) can still
// errors.As for the engine type (e.g. *threewayrsync.WouldWipeError) and offer
// a richer recovery than the flat CLI message.
type guardError struct {
	msg   string
	cause error
}

func (e *guardError) Error() string { return e.msg }
func (e *guardError) Unwrap() error { return e.cause }

// userFacingEngineErr rewrites the engine's delete-guard errors — which point
// at a Go API option — into messages that name the CLI recovery paths. The
// original error stays reachable through errors.As/Is.
func userFacingEngineErr(name string, err error) error {
	var ww *threewayrsync.WouldWipeError
	if errors.As(err, &ww) {
		side, other := "remote", "local"
		if ww.Side == "local" {
			side, other = "local", "remote"
		}
		return &guardError{cause: err, msg: fmt.Sprintf(
			"this sync would delete every previously synced file (%d) on the %s side — a full wipe is never applied; "+
				"if you emptied the %s copy to start over, run 'netcheckout checkin %s --abandon' and check out again",
			ww.Deletes, side, other, name)}
	}
	return err
}

// translateEvent maps an engine event onto the lifecycle event vocabulary: the
// side is where the change landed (a pull lands locally, a push remotely), and a
// transfer is a modify when the path was in the base manifest, an add otherwise.
func translateEvent(e threewayrsync.Event, base threewayrsync.Manifest) Event {
	switch e.Op {
	case "pull":
		return Event{Kind: transferKind(e.Path, base), Side: SideLocal, Path: e.Path}
	case "push":
		return Event{Kind: transferKind(e.Path, base), Side: SideRemote, Path: e.Path}
	case "delete-local":
		return Event{Kind: EventDelete, Side: SideLocal, Path: e.Path}
	default: // "delete-remote"
		return Event{Kind: EventDelete, Side: SideRemote, Path: e.Path}
	}
}

func transferKind(path string, base threewayrsync.Manifest) EventKind {
	if _, ok := base[path]; ok {
		return EventModify
	}
	return EventAdd
}

// fillPlan copies a dry-run plan into the report buckets.
func fillPlan(rep *Report, plan threewayrsync.Plan) {
	rep.Pulled = plan.Pull
	rep.Pushed = plan.Push
	rep.RemovedRemote = plan.RemoteDeletes
	rep.RemovedLocal = plan.LocalDeletes
	rep.Conflicts = plan.Conflicts
}

// Sync reconciles a held checkout in place, leaving the lock untouched. The
// engine loads the base through the profile's store, applies per the conflict
// policy (Abort, or local-wins with --force), and commits the merged base itself;
// lifecycle only refreshes the marker's last-sync stamp afterwards.
func (r Runner) Sync(ctx context.Context, name string, p config.Profile, id ident.Ident, relpath string, opts Options) (Report, error) {
	rep := Report{Action: "sync", DryRun: opts.DryRun}
	pf, err := r.preflightProfile(ctx, name, p, id, relpath, "sync", false)
	if err != nil {
		return rep, err
	}
	syncer := r.syncer(baseline.Store(name))
	eo := engineOptions(pf, opts, pf.state.Files)

	// Dry-run always exits clean: report the plan (and any would-be conflicts)
	// without writing anything, even when the plan itself has conflicts.
	if opts.DryRun {
		plan, err := syncer.Diff(ctx, pf.local, pf.remote, eo)
		if err != nil {
			return rep, userFacingEngineErr(name, err)
		}
		fillPlan(&rep, plan)
		// A dry run previews what THIS flag state would do: without --allow-deletes
		// the planned deletions would be skipped, so report them as pending.
		if !opts.AllowDeletes {
			rep.PendingRemote, rep.RemovedRemote = rep.RemovedRemote, nil
			rep.PendingLocal, rep.RemovedLocal = rep.RemovedLocal, nil
		}
		rep.Marker = pf.marker
		return rep, nil
	}

	res, err := syncer.Sync(ctx, pf.local, pf.remote, eo)
	rep.Pulled = res.Applied.Pull
	rep.Pushed = res.Applied.Push
	rep.RemovedRemote = res.Applied.RemoteDeletes
	rep.RemovedLocal = res.Applied.LocalDeletes
	rep.Conflicts = res.Conflicts
	rep.PendingRemote = res.SkippedRemoteDeletes
	rep.PendingLocal = res.SkippedLocalDeletes
	if err != nil {
		var ce *threewayrsync.ConflictError
		if errors.As(err, &ce) {
			rep.Conflicts = ce.Paths
			return rep, &ConflictError{Paths: ce.Paths}
		}
		return rep, userFacingEngineErr(name, err)
	}

	m := pf.marker
	m.LastSyncAt = r.now()
	if err := pf.acc.Write(ctx, m); err != nil {
		return rep, err
	}
	rep.Marker = m
	return rep, nil
}
