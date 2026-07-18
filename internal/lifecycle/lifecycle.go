// Package lifecycle orchestrates the mutating checkout actions — checkout, sync,
// and checkin — driving the threewayrsync engine, the marker, and the local
// checkout state. It is the single seam the CLI and the TUI both call.
package lifecycle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andresbott/netcheckout/internal/baseline"
	"github.com/andresbott/netcheckout/internal/config"
	"github.com/andresbott/netcheckout/internal/ident"
	"github.com/andresbott/netcheckout/internal/marker"
	"github.com/andresbott/netcheckout/internal/sanity"
	"github.com/andresbott/netcheckout/libs/threewayrsync"
)

// Options are the shared flags every mutating action understands.
type Options struct {
	Force  bool
	DryRun bool
	Clean  bool // checkin only
	// Abandon (checkin only) releases the checkout without requiring local and
	// remote to be in sync: the marker and local state are removed and any
	// unsynced local work is deliberately discarded from the checkout's point of
	// view (the remote keeps its files; the local copy stays unless Clean).
	// This is the sanctioned "start over" path: abandon, then check out again.
	Abandon bool
	// AllowDeletes permits this run to carry out planned deletions. Off, the
	// engine skips every planned delete — they surface in the report as
	// Pending — and nothing is ever removed. On, deletions apply, except a
	// full wipe of one side, which the engine refuses unconditionally.
	AllowDeletes bool
	// OnApply, when non-nil, is called once per applied change as Sync carries the
	// reconcile out, giving callers live per-file progress. It runs on the goroutine
	// driving the action and is never called on a dry run.
	OnApply func(Event)
}

// Report describes what an action did (or would do, for a dry run).
type Report struct {
	Action        string
	Pulled        []string
	Pushed        []string
	RemovedRemote []string
	RemovedLocal  []string
	Conflicts     []string
	Marker        *marker.Marker
	DryRun        bool
	Released      bool
	Abandoned     bool // the release skipped the in-sync verification (checkin --abandon)
	PendingRemote []string // planned remote deletions skipped (allow-deletes off)
	PendingLocal  []string // planned local deletions skipped (allow-deletes off)
}

// EventKind is the verb of a single applied change, mirroring the status view.
type EventKind int

const (
	EventAdd    EventKind = iota // a new file appeared on the destination side
	EventModify                  // an existing file's contents were updated
	EventDelete                  // a file was removed from the destination side
)

// Side is the endpoint an applied change landed on.
type Side int

const (
	SideLocal  Side = iota // change applied under the local root
	SideRemote             // change applied under the remote root
)

// Event is one applied change, emitted live as Sync carries it out so callers
// can render progress in the same "verb → side  path" shape as the status view.
type Event struct {
	Kind EventKind
	Side Side
	Path string
}

// Syncer is the three-way engine surface lifecycle needs; *threewayrsync.Syncer
// satisfies it.
type Syncer interface {
	Sync(ctx context.Context, local, remote threewayrsync.Endpoint, opts threewayrsync.Options) (threewayrsync.Result, error)
	Diff(ctx context.Context, local, remote threewayrsync.Endpoint, opts threewayrsync.Options) (threewayrsync.Plan, error)
}

// Runner carries the injectable dependencies for the actions.
type Runner struct {
	// NewSyncer builds the engine for one profile's store. nil means the real
	// threewayrsync.New (rsync on PATH); tests inject fakes here.
	NewSyncer func(store threewayrsync.Store) Syncer
	// NewAccessor builds the marker accessor for a remote endpoint. nil means
	// the real marker.ForEndpoint; tests inject fakes (e.g. a racing writer).
	NewAccessor func(e threewayrsync.Endpoint) marker.Accessor
	// RsyncBin overrides the rsync binary for the default syncer and accessor;
	// empty means "rsync" from PATH.
	RsyncBin    string
	ToolVersion string
	Now         func() time.Time
}

func (r Runner) accessor(e threewayrsync.Endpoint) marker.Accessor {
	if r.NewAccessor != nil {
		return r.NewAccessor(e)
	}
	return marker.ForEndpoint(e, r.RsyncBin)
}

func (r Runner) syncer(store threewayrsync.Store) Syncer {
	if r.NewSyncer != nil {
		return r.NewSyncer(store)
	}
	s := threewayrsync.New(store)
	s.Bin = r.RsyncBin
	return s
}

func (r Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now().UTC()
}

// normalizeRelpath maps "" or "." to "." (whole root) and cleans anything else,
// stripping a leading "./".
func normalizeRelpath(rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." {
		return "."
	}
	return filepath.ToSlash(filepath.Clean(rel))
}

// scopeFor translates a set of recorded relpaths into a threewayrsync scope,
// via the same baseline.State.Scope logic status uses.
func scopeFor(relpaths []string) []string {
	return (&baseline.State{Relpaths: relpaths}).Scope()
}

// profilePlan bundles the shared preflight outcome: the resolved endpoints, the
// marker accessor and the read marker, the loaded checkout state, and the scope —
// everything sync and checkin need before either touches anything.
type profilePlan struct {
	local, remote threewayrsync.Endpoint
	acc           marker.Accessor
	marker        *marker.Marker
	state         *baseline.State
	scope         []string
}

// preflightProfile runs the checks shared by sync and checkin, mutating nothing:
// the unlisted-local guard, the remote-mounted check (local-path remotes only —
// a URL remote fails loudly on its first rsync call instead), the lock ownership
// check, and the local-state requirement. relpath narrows the scope when given.
// action names the operation in error messages. skipUnlisted waives the
// unlisted-local guard for callers that deliberately leave local work behind
// (checkin --abandon) — ownership and state checks still apply.
func (r Runner) preflightProfile(ctx context.Context, name string, p config.Profile, id ident.Ident, relpath, action string, skipUnlisted bool) (profilePlan, error) {
	// Refuse if local content lies outside the declared subpaths, so a scoped push
	// never silently skips local work. Runs before any transfer; --force does not
	// bypass it (this is data safety, not a lock override). A walk error fails the
	// operation closed — unlike status/Check, which swallow walk errors best-effort.
	if !skipUnlisted {
		if err := refuseUnlistedLocal(p, name, action); err != nil {
			return profilePlan{}, err
		}
	}

	remote, err := p.RemoteEndpoint()
	if err != nil {
		return profilePlan{}, err
	}
	local := p.LocalEndpoint()
	// The local root is NOT created here: a missing dir with an empty baseline is
	// just a not-yet-pulled working copy (the engine treats it as an empty tree
	// and Sync materializes it on the first pull), while a missing dir with a
	// non-empty baseline is an unmounted disk — creating it would turn the whole
	// baseline into pushed deletions.

	if p.RemoteIsLocalPath() {
		if info, err := os.Stat(remote.Path); err != nil || !info.IsDir() {
			return profilePlan{}, fmt.Errorf("remote root %s is not mounted", remote.Path)
		}
	}

	acc := r.accessor(remote)
	m, exists, err := acc.Read(ctx)
	if err != nil {
		return profilePlan{}, err
	}
	if !exists {
		return profilePlan{}, fmt.Errorf("profile %q is not checked out (no marker)", name)
	}
	// Ownership is absolute here: sync's --force only resolves same-file
	// conflicts local-wins (GOALS §9.5) and checkin has no force at all (§9).
	// Overriding a foreign lock is checkout's job, never sync/checkin's.
	if !m.OwnedBy(id.By, id.Host) {
		return profilePlan{}, fmt.Errorf("profile %q is checked out by %s on %s (not this machine)", name, m.CheckedOutBy, m.Host)
	}

	st, hasState, err := baseline.Load(name)
	if err != nil {
		return profilePlan{}, err
	}
	if !hasState {
		return profilePlan{}, fmt.Errorf("no local baseline for %q — re-checkout on this machine to establish one", name)
	}
	// The baseline is only meaningful against the roots it was recorded from: a
	// profile whose roots were edited since checkout (or recreated under the same
	// name) would merge the manifest against the wrong trees, manufacturing
	// deletes and conflicts. Pre-binding state files (empty roots) are accepted.
	localRoot := config.ExpandRoot(p.LocalRoot)
	remoteRoot := config.ExpandRoot(p.RemoteRoot)
	if (st.LocalRoot != "" && st.LocalRoot != localRoot) || (st.RemoteRoot != "" && st.RemoteRoot != remoteRoot) {
		return profilePlan{}, fmt.Errorf(
			"profile %q was checked out against different roots (local %s, remote %s) than now configured (local %s, remote %s) — check in from the old roots first, or remove the stale state",
			name, st.LocalRoot, st.RemoteRoot, localRoot, remoteRoot)
	}

	// The same guard again, but against the checkout ENVELOPE (the recorded
	// relpaths) rather than the declared subpaths: a checkout scoped narrower
	// than the declaration (e.g. `checkout p a` on a profile declaring [a, b])
	// leaves local work under the other declared subpaths invisible to every
	// scoped sync and to checkin's in-sync verification — the guard above cannot
	// see it because the content IS inside a declared subpath.
	if !skipUnlisted {
		if err := refuseOutsideEnvelope(p, name, action, st.Relpaths); err != nil {
			return profilePlan{}, err
		}
	}

	relpaths := st.Relpaths
	if rel := normalizeRelpath(relpath); rel != "." {
		// The relpath must lie inside the checkout envelope. A sync outside it
		// would pull a subtree the envelope never records: subsequent unscoped
		// syncs would skip it, and checkin — which diffs the envelope only —
		// would report in-sync while that subtree holds unsynced work (which
		// --clean would then delete). Widening the envelope is checkout's job.
		if !relpathCovered(rel, st.Relpaths) {
			return profilePlan{}, fmt.Errorf(
				"relpath %q is outside the checked-out scope of %q (%s) — run 'netcheckout checkout %s %s' to widen the checkout first",
				rel, name, strings.Join(st.Relpaths, ", "), name, rel)
		}
		relpaths = []string{rel}
	}

	return profilePlan{local: local, remote: remote, acc: acc, marker: m, state: st, scope: scopeFor(relpaths)}, nil
}

// refuseUnlistedLocal errors when local content lies outside the profile's
// declared subpaths (see the preflight comment at the call site).
func refuseUnlistedLocal(p config.Profile, name, action string) error {
	unlisted, err := sanity.UnlistedLocal(p)
	if err != nil {
		return err
	}
	if len(unlisted) > 0 {
		return fmt.Errorf(
			"refusing to %s %q: local content is outside the profile's subpaths and would not be synced (add it to subpaths or remove it):\n  %s",
			action, name, strings.Join(unlisted, "\n  "))
	}
	return nil
}

// refuseOutsideEnvelope errors when local content lies outside the checkout
// envelope (the recorded relpaths; see the preflight comment at the call site).
func refuseOutsideEnvelope(p config.Profile, name, action string, relpaths []string) error {
	unlisted, err := sanity.UnlistedOutside(p, relpaths)
	if err != nil {
		return err
	}
	if len(unlisted) > 0 {
		return fmt.Errorf(
			"refusing to %s %q: local content is outside the checked-out scope (%s) and would not be synced — "+
				"move it aside, widen the checkout with 'netcheckout checkout %s <relpath>', sync, then merge it back (or remove it):\n  %s",
			action, name, strings.Join(relpaths, ", "), name, strings.Join(unlisted, "\n  "))
	}
	return nil
}
