package threewayrsync

import (
	"fmt"
	"strings"
)

// normalizeScope validates Options.Scope and returns it in canonical form (trailing
// slashes trimmed). Each entry must be a safe relative directory path — the same rules a
// manifest path obeys — and free of rsync wildcard characters, whose filter-rule
// semantics would silently widen or narrow the scope. It is idempotent.
func normalizeScope(scope []string) ([]string, error) {
	if len(scope) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(scope))
	for _, dir := range scope {
		d := strings.TrimRight(dir, "/")
		if !safeRelPath(d) {
			return nil, fmt.Errorf("scope entry %q is not a safe relative path", dir)
		}
		if strings.ContainsAny(d, "*?[]") {
			return nil, fmt.Errorf("scope entry %q contains rsync wildcard characters", dir)
		}
		out = append(out, d)
	}
	return out, nil
}

// scopeFilterArgs translates a normalized scope into an rsync filter chain: for each
// scope dir, anchored includes for every ancestor (so rsync descends into them) and a
// "/dir/***" include for the subtree itself, then a single catch-all exclude. Order
// matters — rsync applies the first matching rule — so these must come after any
// specific --exclude flags and the trailing "--exclude=*" must be last. An empty scope
// yields nothing (whole tree).
func scopeFilterArgs(scope []string) []string {
	if len(scope) == 0 {
		return nil
	}
	var args []string
	seen := map[string]bool{}
	for _, dir := range scope {
		segs := strings.Split(dir, "/")
		prefix := ""
		for i, seg := range segs {
			prefix += "/" + seg
			rule := "--include=" + prefix + "/"
			if i == len(segs)-1 {
				rule = "--include=" + prefix + "/***"
			}
			if !seen[rule] {
				seen[rule] = true
				args = append(args, rule)
			}
		}
	}
	return append(args, "--exclude=*")
}

// inScope reports whether a slash-relative path falls under the normalized scope. An
// empty scope means the whole tree.
func inScope(path string, scope []string) bool {
	if len(scope) == 0 {
		return true
	}
	for _, dir := range scope {
		if path == dir || strings.HasPrefix(path, dir+"/") {
			return true
		}
	}
	return false
}

// countInScope counts the manifest file entries that fall under the scope. The safety
// valve (EmptyEndpointError) compares against this rather than the whole base: a scoped
// sync only sees — and can only harm — the in-scope part of the tree. Directory entries
// don't count: a side reduced to empty directories has lost its content all the same.
func countInScope(m Manifest, scope []string) int {
	n := 0
	for p, st := range m {
		if !st.IsDir && inScope(p, scope) {
			n++
		}
	}
	return n
}
