package threewayrsync

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// normalizeIgnore validates Options.Ignore and returns it in canonical form (trimmed,
// blanks dropped). Each pattern is a single path segment — a slash would make it
// position-dependent, which segment matching cannot honor — and must be a valid
// path.Match glob. It is idempotent.
func normalizeIgnore(ignore []string) ([]string, error) {
	if len(ignore) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(ignore))
	for _, pat := range ignore {
		p := strings.TrimSpace(pat)
		if p == "" {
			continue
		}
		if strings.Contains(p, "/") {
			return nil, fmt.Errorf("ignore pattern %q must not contain a slash (patterns match single path segments)", pat)
		}
		if _, err := path.Match(p, "x"); err != nil {
			return nil, fmt.Errorf("ignore pattern %q is not a valid glob: %w", pat, err)
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// matchesIgnore reports whether any slash-separated segment of relpath matches any
// pattern, so ignoring a directory name ignores everything beneath it. Patterns are
// pre-validated by normalizeIgnore, which makes path.Match's only error impossible here.
func matchesIgnore(relpath string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	for _, seg := range strings.Split(relpath, "/") {
		for _, pat := range patterns {
			if ok, _ := path.Match(pat, seg); ok {
				return true
			}
		}
	}
	return false
}

// partitionIgnored splits a manifest into the entries the sync operates on and the
// (sorted) ignored paths it must only ever report. patterns must be normalized.
func partitionIgnored(m Manifest, patterns []string) (kept Manifest, ignored []string) {
	if len(patterns) == 0 {
		return m, nil
	}
	kept = make(Manifest, len(m))
	for p, st := range m {
		if matchesIgnore(p, patterns) {
			ignored = append(ignored, p)
			continue
		}
		kept[p] = st
	}
	sort.Strings(ignored)
	return kept, ignored
}
