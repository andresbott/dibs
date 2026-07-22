package threewayrsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
)

// Syncer performs three-way syncs between a local and a remote endpoint using rsync,
// persisting the base manifest through a pluggable Store. A Syncer is not safe for
// concurrent use; when the Store implements Locker, Sync additionally holds a
// cross-process lock so two processes cannot interleave over the same base state.
type Syncer struct {
	Store Store
	Bin   string // rsync binary; "" => "rsync"
	run   runner // unexported test seam; nil => execRun
}

// New returns a Syncer that shells out to rsync on PATH, using store for base state.
func New(store Store) *Syncer {
	return &Syncer{Store: store, Bin: "rsync", run: execRun}
}

func (s *Syncer) bin() string {
	if s.Bin == "" {
		return "rsync"
	}
	return s.Bin
}

func (s *Syncer) runner() runner {
	if s.run == nil {
		return execRun
	}
	return s.run
}

// EmptyEndpointError reports an endpoint that lists no files while the base manifest
// records a previous sync. An empty-but-listable directory is what an unmounted share or
// a mistyped path looks like; syncing against it would classify every base file as a
// deletion and wipe the other side.
type EmptyEndpointError struct {
	Side string // "local" | "remote"
	Path string
}

func (e *EmptyEndpointError) Error() string {
	return fmt.Sprintf("%s endpoint %q lists no files but a previous sync recorded some: unmounted share or wrong path? (set Options.AcceptEmpty to sync anyway)", e.Side, e.Path)
}

// preflightLocal verifies a filesystem endpoint path before any rsync call, so an
// unmounted mount point or a typo fails with a clear message. missingOK relaxes the
// existence requirement: a not-yet-created directory is reported as (missing=true, nil)
// instead of an error, for callers that can treat it as an empty tree. Remote endpoints
// (ssh or daemon) need no equivalent: rsync itself fails hard on a missing remote path
// or module.
func preflightLocal(e Endpoint, side string, missingOK bool) (missing bool, err error) {
	if e.remote() {
		return false, nil
	}
	st, err := os.Stat(e.Path)
	if err != nil {
		if os.IsNotExist(err) {
			if missingOK {
				return true, nil
			}
			return false, fmt.Errorf("%s endpoint %q does not exist (is the share mounted?)", side, e.Path)
		}
		return false, fmt.Errorf("%s endpoint %q: %w", side, e.Path, err)
	}
	if !st.IsDir() {
		return false, fmt.Errorf("%s endpoint %q is not a directory", side, e.Path)
	}
	return false, nil
}

// list enumerates an endpoint into a Manifest via a dry-run itemize against an empty
// temporary directory: the empty dest makes rsync report the whole source tree (or the
// scoped part of it). scope must already be normalized.
func (s *Syncer) list(ctx context.Context, e Endpoint, exclude, scope []string) (Manifest, error) {
	empty, err := os.MkdirTemp("", "threewayrsync-empty-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(empty) }()
	args := buildListArgs(e, empty, exclude, scope)
	res, err := s.runner()(ctx, s.bin(), args, nil)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &Error{Op: "list", Args: args, Stderr: res.stderr, ExitCode: res.exitCode, Err: err}
	}
	return parseListOutput(res.stdout)
}

// computePlan loads the base, enumerates both endpoints, and classifies. It returns the
// plan plus the base and the two live manifests (Sync needs them to derive the merged
// base). opts.Scope and opts.Ignore must already be normalized (both public entry
// points do). Ignored paths are partitioned out of the listings and the base BEFORE
// anything judges them — the valves, the classifier, and the returned manifests all see
// only the kept part — and surface solely in Plan.Ignored.
func (s *Syncer) computePlan(ctx context.Context, local, remote Endpoint, opts Options) (Plan, Manifest, Manifest, Manifest, error) {
	if s.Store == nil {
		return Plan{}, nil, nil, nil, errors.New("threewayrsync: Syncer.Store is required")
	}
	if err := validate(local, remote); err != nil {
		return Plan{}, nil, nil, nil, err
	}
	base, _, err := s.Store.LoadBase()
	if err != nil {
		return Plan{}, nil, nil, nil, err
	}
	if base == nil {
		base = Manifest{}
	}
	// A base entry that now matches an ignore pattern must drop out of the merge
	// silently: keeping it would plan its absence as a deletion on both sides.
	base, _ = partitionIgnored(base, opts.Ignore)
	// A local working copy that does not exist yet is only acceptable while the
	// in-scope base is empty (nothing has been synced, so nothing can be lost —
	// it is simply an empty tree). With base entries in scope, a missing local
	// dir is an unmounted disk or a deleted working copy: syncing against it
	// would push every base file as a deletion, so it is a hard error.
	localMissing, err := preflightLocal(local, "local", countInScope(base, opts.Scope) == 0)
	if err != nil {
		return Plan{}, nil, nil, nil, err
	}
	if _, err := preflightLocal(remote, "remote", false); err != nil {
		return Plan{}, nil, nil, nil, err
	}
	localM := Manifest{}
	if !localMissing {
		if localM, err = s.list(ctx, local, opts.Exclude, opts.Scope); err != nil {
			return Plan{}, nil, nil, nil, err
		}
	}
	remoteM, err := s.list(ctx, remote, opts.Exclude, opts.Scope)
	if err != nil {
		return Plan{}, nil, nil, nil, err
	}
	localM, ignoredLocal := partitionIgnored(localM, opts.Ignore)
	remoteM, ignoredRemote := partitionIgnored(remoteM, opts.Ignore)
	// A scoped sync only sees the in-scope part of the tree, so "suddenly lists nothing"
	// is judged against the in-scope base entries: a legitimately empty scope dir must
	// not trip the valve just because the rest of the base has files.
	if countInScope(base, opts.Scope) > 0 && !opts.AcceptEmpty {
		if countFiles(localM) == 0 {
			return Plan{}, nil, nil, nil, &EmptyEndpointError{Side: "local", Path: local.Path}
		}
		if countFiles(remoteM) == 0 {
			return Plan{}, nil, nil, nil, &EmptyEndpointError{Side: "remote", Path: remote.Path}
		}
	}
	plan := Classify(base, localM, remoteM)
	plan.Ignored = unionSorted(ignoredLocal, ignoredRemote)
	return plan, base, localM, remoteM, nil
}

// unionSorted merges two sorted string slices into one sorted, deduplicated slice.
func unionSorted(a, b []string) []string {
	if len(a) == 0 {
		return b
	}
	seen := make(map[string]struct{}, len(a)+len(b))
	var out []string
	for _, s := range append(append([]string(nil), a...), b...) {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// Diff performs a dry run: it loads the base, enumerates both endpoints, and classifies
// every path into a Plan. It has no side effects.
func (s *Syncer) Diff(ctx context.Context, local, remote Endpoint, opts Options) (Plan, error) {
	scope, err := normalizeScope(opts.Scope)
	if err != nil {
		return Plan{}, err
	}
	opts.Scope = scope
	ignore, err := normalizeIgnore(opts.Ignore)
	if err != nil {
		return Plan{}, err
	}
	opts.Ignore = ignore
	plan, _, _, _, err := s.computePlan(ctx, local, remote, opts)
	return plan, err
}

// List enumerates one endpoint into a Manifest, applying the same normalization
// every Sync/Diff listing gets: scope filters, excludes, and ignore
// partitioning (ignored entries are dropped, not reported). A missing local
// directory is an empty tree — List is a read-only census, so absence is data,
// not danger. Only Scope, Exclude, and Ignore are honored from opts.
func (s *Syncer) List(ctx context.Context, e Endpoint, opts Options) (Manifest, error) {
	scope, err := normalizeScope(opts.Scope)
	if err != nil {
		return nil, err
	}
	ignore, err := normalizeIgnore(opts.Ignore)
	if err != nil {
		return nil, err
	}
	if err := validateOne(e); err != nil {
		return nil, err
	}
	missing, err := preflightLocal(e, "endpoint", true)
	if err != nil {
		return nil, err
	}
	if missing {
		return Manifest{}, nil
	}
	m, err := s.list(ctx, e, opts.Exclude, scope)
	if err != nil {
		return nil, err
	}
	kept, _ := partitionIgnored(m, ignore)
	return kept, nil
}
