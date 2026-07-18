package threewayrsync

import "sort"

type action int

const (
	actNoop action = iota
	actPush
	actPull
	actRemoteDelete
	actLocalDelete
	actConflict
)

// Plan is the set of operations a Sync will perform.
type Plan struct {
	Pull          []string // remote → local
	Push          []string // local → remote
	LocalDeletes  []string // remove under the local root
	RemoteDeletes []string // remove under the remote root
	Conflicts     []string // changed on both sides and not converged
	InSync        bool     // every bucket empty
}

// classifyPath encodes the three-way merge table. "changed" is meaningful only when the
// path is in base and present on that side. converged is true when the path is present on
// both sides with identical state (size+mtime): two sides that already agree are never a
// conflict, which is what makes a canceled sync safe to resume.
func classifyPath(inBase, localPresent, localChanged, remotePresent, remoteChanged, converged bool) action { //nolint:gocyclo // exhaustive three-way decision table; branch count is intrinsic to the (base × present × changed) matrix and is fully covered by TestClassifyPathTable.
	if localPresent && remotePresent && converged {
		return actNoop
	}
	if !inBase {
		switch {
		case localPresent && remotePresent:
			return actConflict // both independently added the same path (not converged)
		case remotePresent:
			return actPull
		case localPresent:
			return actPush
		default:
			return actNoop
		}
	}
	switch {
	case !localPresent && !remotePresent:
		return actNoop // deleted on both sides
	case !localPresent && remotePresent:
		if remoteChanged {
			return actConflict // local delete vs remote edit
		}
		return actRemoteDelete // propagate local delete
	case localPresent && !remotePresent:
		if localChanged {
			return actConflict // remote delete vs local edit
		}
		return actLocalDelete // mirror remote delete
	default: // present both sides
		switch {
		case localChanged && remoteChanged:
			return actConflict
		case localChanged:
			return actPush
		case remoteChanged:
			return actPull
		default:
			return actNoop
		}
	}
}

// Classify compares the current local and remote manifests against base and buckets every
// path into a Plan. "changed" is size+mtime vs base; "converged" is size+mtime between the
// two live sides.
func Classify(base, local, remote Manifest) Plan {
	seen := map[string]struct{}{}
	for p := range base {
		seen[p] = struct{}{}
	}
	for p := range local {
		seen[p] = struct{}{}
	}
	for p := range remote {
		seen[p] = struct{}{}
	}

	var plan Plan
	for p := range seen {
		bState, inBase := base[p]
		lState, lPresent := local[p]
		rState, rPresent := remote[p]

		lChanged := lPresent && inBase && !lState.Equal(bState)
		rChanged := rPresent && inBase && !rState.Equal(bState)
		converged := lPresent && rPresent && lState.Equal(rState)

		switch classifyPath(inBase, lPresent, lChanged, rPresent, rChanged, converged) {
		case actPush:
			plan.Push = append(plan.Push, p)
		case actPull:
			plan.Pull = append(plan.Pull, p)
		case actRemoteDelete:
			plan.RemoteDeletes = append(plan.RemoteDeletes, p)
		case actLocalDelete:
			plan.LocalDeletes = append(plan.LocalDeletes, p)
		case actConflict:
			plan.Conflicts = append(plan.Conflicts, p)
		case actNoop:
		}
	}
	sort.Strings(plan.Pull)
	sort.Strings(plan.Push)
	sort.Strings(plan.LocalDeletes)
	sort.Strings(plan.RemoteDeletes)
	sort.Strings(plan.Conflicts)
	plan.InSync = len(plan.Pull)+len(plan.Push)+len(plan.LocalDeletes)+len(plan.RemoteDeletes)+len(plan.Conflicts) == 0
	return plan
}
