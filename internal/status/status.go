// Package status previews whether a profile's local and remote roots are in
// sync. It is a read-only dry-run of sync: it loads the checkout state and runs
// the same three-way engine (libs/threewayrsync) that sync uses to apply changes,
// so what status reports and what sync does can never diverge. The preview is
// grouped per configured subpath (target), or a single "(root)" group when the
// profile has no subpaths.
package status

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/andresbott/netcheckout/internal/baseline"
	"github.com/andresbott/netcheckout/internal/config"
	"github.com/andresbott/netcheckout/internal/marker"
	"github.com/andresbott/netcheckout/libs/threewayrsync"
)

// Change is one planned copy: a path and whether it modifies an existing file
// (in the base manifest) or adds a new one.
type Change struct {
	Path   string
	Modify bool // true = an existing file changed; false = a newly added file
}

// TargetStatus is the three-way plan for one target of a profile — the whole
// root when no subpaths are declared, or one declared subpath — split into the
// buckets a sync uses.
type TargetStatus struct {
	Subpath       string
	Push          []Change // local -> remote (add or modify)
	Pull          []Change // remote -> local (add or modify)
	LocalDeletes  []string // mirror a remote delete by removing the local file
	RemoteDeletes []string // propagate a local delete by removing the remote file
	Conflicts     []string // changed on both sides; a sync would stop
}

// InSync reports whether this target has no pending changes in any bucket.
func (t TargetStatus) InSync() bool {
	return len(t.Push)+len(t.Pull)+len(t.LocalDeletes)+len(t.RemoteDeletes)+len(t.Conflicts) == 0
}

// Label is a human-readable name for this target: "(root)" for the whole root,
// or the declared subpath.
func (t TargetStatus) Label() string {
	return label(t.Subpath)
}

func label(subpath string) string {
	if subpath == "" {
		return "(root)"
	}
	return subpath
}

// ProfileStatus is the sync preview across every target of a profile. When
// CheckedOut is false the profile has no marker; when CheckedOut is true but
// HasBaseline is false the profile is checked out but this machine holds no local
// state, so no plan could be computed and Targets is empty.
type ProfileStatus struct {
	CheckedOut  bool
	HasBaseline bool
	Targets     []TargetStatus
}

// InSync reports whether every target of the profile is in sync.
func (s ProfileStatus) InSync() bool {
	for _, t := range s.Targets {
		if !t.InSync() {
			return false
		}
	}
	return true
}

// Differ is the read-only engine surface Compute needs; *threewayrsync.Syncer
// satisfies it. Injectable for tests.
type Differ interface {
	Diff(ctx context.Context, local, remote threewayrsync.Endpoint, opts threewayrsync.Options) (threewayrsync.Plan, error)
}

// Compute loads the profile's checkout state and previews the three-way plan,
// grouped per target, against the current local and remote trees. It errors only
// for a real failure (the remote root missing, an invalid subpath, or an rsync
// failure); pending changes are a normal result captured in the returned
// ProfileStatus. name is the profile name (the state-file key); rsyncBin
// overrides the rsync binary ("" means "rsync" from PATH).
func Compute(ctx context.Context, name string, p config.Profile, rsyncBin string) (ProfileStatus, error) {
	return ComputeWith(ctx, nil, name, p, rsyncBin)
}

// ComputeWith is Compute with an injectable engine; a nil differ uses the real
// threewayrsync syncer over the profile's store.
func ComputeWith(ctx context.Context, differ Differ, name string, p config.Profile, rsyncBin string) (ProfileStatus, error) {
	remote, err := p.RemoteEndpoint()
	if err != nil {
		return ProfileStatus{}, err
	}
	if p.RemoteIsLocalPath() {
		if info, err := os.Stat(remote.Path); err != nil || !info.IsDir() {
			return ProfileStatus{}, fmt.Errorf("remote root %s is not mounted", remote.Path)
		}
	}

	// No marker means the profile is not checked out: nothing to preview.
	_, exists, err := marker.ForEndpoint(remote, rsyncBin).Read(ctx)
	if err != nil {
		return ProfileStatus{}, err
	}
	if !exists {
		return ProfileStatus{CheckedOut: false}, nil
	}

	// A marker without local state means the checkout is held elsewhere (or the
	// state was lost): report checked out, but no plan can be computed.
	st, hasState, err := baseline.Load(name)
	if err != nil {
		return ProfileStatus{}, err
	}
	if !hasState {
		return ProfileStatus{CheckedOut: true, HasBaseline: false}, nil
	}

	targets, err := p.Targets()
	if err != nil {
		return ProfileStatus{}, err
	}

	if differ == nil {
		s := threewayrsync.New(baseline.Store(name))
		s.Bin = rsyncBin
		differ = s
	}

	// One engine Diff scoped exactly as sync scopes (the checkout state's
	// recorded relpaths — a whole-root entry means no scope): two rsync listings
	// total, then the plan is grouped per target below. Status previews sync, so
	// diverging scopes here would make it report changes sync would never touch.
	// The local root may not exist yet right after checkout; the engine treats a
	// missing dir over an empty base as an empty tree, so status stays read-only.
	plan, err := differ.Diff(ctx, p.LocalEndpoint(), remote, threewayrsync.Options{
		Scope:       st.Scope(),
		Exclude:     marker.Exclude(),
		AcceptEmpty: true, // status is read-only; an empty side is data, not danger
	})
	if err != nil {
		return ProfileStatus{}, err
	}

	out := ProfileStatus{CheckedOut: true, HasBaseline: true, Targets: make([]TargetStatus, 0, len(targets))}
	for _, t := range targets {
		out.Targets = append(out.Targets, targetFromPlan(t.Subpath, plan, st.Files))
	}
	return out, nil
}

// underTarget reports whether path belongs to the target: everything for the
// whole-root target, or an exact/prefix match for a subpath target.
func underTarget(path, subpath string) bool {
	if subpath == "" {
		return true
	}
	return path == subpath || strings.HasPrefix(path, subpath+"/")
}

// targetFromPlan projects the slice of a plan under one target into a
// TargetStatus, labelling each pushed or pulled path as a modify (already in the
// base manifest) or an add (not yet).
func targetFromPlan(subpath string, plan threewayrsync.Plan, base threewayrsync.Manifest) TargetStatus {
	filter := func(paths []string) []string {
		var out []string
		for _, p := range paths {
			if underTarget(p, subpath) {
				out = append(out, p)
			}
		}
		return out
	}
	changes := func(paths []string) []Change {
		var out []Change
		for _, p := range filter(paths) {
			_, inBase := base[p]
			out = append(out, Change{Path: p, Modify: inBase})
		}
		return out
	}
	return TargetStatus{
		Subpath:       subpath,
		Push:          changes(plan.Push),
		Pull:          changes(plan.Pull),
		LocalDeletes:  filter(plan.LocalDeletes),
		RemoteDeletes: filter(plan.RemoteDeletes),
		Conflicts:     filter(plan.Conflicts),
	}
}
