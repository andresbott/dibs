// Package ident resolves the two identity facts a checkout marker records:
// By (the human-readable "who", from config or $USER@$HOSTNAME) and Host (this
// machine's hostname). Ownership of a marker requires both to match, plus the
// profile's UUID (marker.OwnedBy) — identity and host alone cannot tell two
// profiles on the same machine apart.
package ident

import (
	"os"

	"github.com/candy-tools/dibs/internal/config"
)

// Ident is the resolved identity of the current machine/user.
type Ident struct {
	By   string // marker checked_out_by
	Host string // marker host (os.Hostname)
}

// Resolve derives Ident from cfg. By is cfg.Identity when set, else
// "$USER@$HOSTNAME". Host is always os.Hostname().
func Resolve(cfg *config.Config) (Ident, error) {
	host, err := os.Hostname()
	if err != nil {
		return Ident{}, err
	}
	by := cfg.Identity
	if by == "" {
		user := os.Getenv("USER")
		switch {
		case user != "" && host != "":
			by = user + "@" + host
		case host != "":
			by = host
		default:
			by = "unknown"
		}
	}
	return Ident{By: by, Host: host}, nil
}
