package threewayrsync

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The oldest rsync this package works against: --delete-missing-args (remote deletes)
// appeared in 3.1, and the --out-format itemize parsing assumes GNU rsync output.
const (
	minRsyncMajor = 3
	minRsyncMinor = 1
)

// BinInfo describes the rsync binary a Syncer shells out to.
type BinInfo struct {
	Version   string // e.g. "3.2.7"
	OpenRsync bool   // Apple's openrsync (macOS default), which this package cannot drive
}

// CheckBinary runs `<bin> --version` and verifies it is GNU rsync >= 3.1. It exists to
// fail fast at startup with an actionable message instead of deep inside a sync: macOS
// ships Apple's openrsync as /usr/bin/rsync, which lacks the flags this package depends
// on. The returned error tells the user to install GNU rsync or point the rsync path
// setting at one.
func (s *Syncer) CheckBinary(ctx context.Context) (BinInfo, error) {
	res, err := s.runner()(ctx, s.bin(), []string{"--version"}, nil)
	if err != nil {
		if ctx.Err() != nil {
			return BinInfo{}, ctx.Err()
		}
		return BinInfo{}, fmt.Errorf("rsync not found or not runnable at %q: %v (install GNU rsync or set the rsync path in client settings)", s.bin(), err)
	}
	return parseRsyncVersion(res.stdout)
}

var rsyncVersionRe = regexp.MustCompile(`rsync\s+version\s+v?(\d+)\.(\d+)(\.\d+)?`)

// parseRsyncVersion inspects `rsync --version` output and rejects openrsync and any GNU
// rsync older than minRsyncMajor.minRsyncMinor.
func parseRsyncVersion(out string) (BinInfo, error) {
	if strings.Contains(strings.ToLower(out), "openrsync") {
		return BinInfo{OpenRsync: true}, fmt.Errorf("this rsync is Apple openrsync — netcheckout needs GNU rsync >= %d.%d (e.g. brew install rsync), or set the rsync path in client settings", minRsyncMajor, minRsyncMinor)
	}
	m := rsyncVersionRe.FindStringSubmatch(out)
	if m == nil {
		return BinInfo{}, fmt.Errorf("could not parse `rsync --version` output %q — is this GNU rsync >= %d.%d?", firstLine(out), minRsyncMajor, minRsyncMinor)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	info := BinInfo{Version: m[1] + "." + m[2] + m[3]}
	if major < minRsyncMajor || (major == minRsyncMajor && minor < minRsyncMinor) {
		return info, fmt.Errorf("rsync %s is too old — netcheckout needs GNU rsync >= %d.%d, or set the rsync path in client settings", info.Version, minRsyncMajor, minRsyncMinor)
	}
	return info, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
