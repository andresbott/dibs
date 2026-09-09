package tui

import (
	"context"
	"time"

	"github.com/candy-tools/dibs/libs/threewayrsync"
	tea "github.com/charmbracelet/bubbletea"
)

// connChecker verifies a daemon endpoint is reachable and its credentials are
// accepted — a seam so tests inject a canned result instead of a live daemon.
type connChecker func(ctx context.Context, d threewayrsync.Daemon) error

// serverCheckTimeout bounds the whole post-save connection probe (module list
// plus the per-module auth listings) so an unresponsive daemon cannot wedge the
// form forever.
const serverCheckTimeout = 30 * time.Second

// serverCheckResultMsg carries the finished connection probe back into Update.
// seq is the launch stamp so a result abandoned by esc (or superseded by a
// newer Save) is recognised and dropped.
type serverCheckResultMsg struct {
	seq int
	err error
}

// checker resolves the form's connection-check seam, defaulting to a
// Syncer-backed CheckConnection over the configured rsync binary.
func (s serverFormModel) checker() connChecker {
	if s.checkConn != nil {
		return s.checkConn
	}
	syncer := &threewayrsync.Syncer{Bin: s.rsyncBin}
	return syncer.CheckConnection
}

// checkConnectionCmd runs the connection probe off the UI thread, then calls
// cleanup (e.g. removing a temp password file) and reports the outcome.
func checkConnectionCmd(fn connChecker, d threewayrsync.Daemon, cleanup func(), seq int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), serverCheckTimeout)
		defer cancel()
		err := fn(ctx, d)
		if cleanup != nil {
			cleanup()
		}
		return serverCheckResultMsg{seq: seq, err: err}
	}
}
