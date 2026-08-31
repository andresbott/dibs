package cmd

import (
	"fmt"
	"io"

	"github.com/andresbott/dibs/app/metainfo"
	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/internal/ident"
	"github.com/andresbott/dibs/internal/lifecycle"
	"github.com/spf13/cobra"
)

func newCheckinCmd(cfgPath *string) *cobra.Command {
	return newCheckinCmdWithRunner(cfgPath, lifecycle.Runner{ToolVersion: metainfo.Version})
}

func newCheckinCmdWithRunner(cfgPath *string, r lifecycle.Runner) *cobra.Command {
	var dryRun, clean, abandon bool
	cmd := &cobra.Command{
		Use:   "checkin <profile>",
		Short: "verify the profile is fully synced, then release the lock",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolvePath(*cfgPath)
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			name := args[0]
			p, err := cfg.ResolveProfile(name)
			if err != nil {
				return err
			}
			id, err := ident.Resolve(cfg)
			if err != nil {
				return err
			}
			// Only the real engine shells out to rsync; an injected test syncer
			// must not require one on the machine.
			if r.NewSyncer == nil {
				if err := checkRsync(cmd.Context(), cfg); err != nil {
					return err
				}
				r.RsyncBin = cfg.RsyncPath
			}
			opts := lifecycle.Options{DryRun: dryRun, Clean: clean, Abandon: abandon}
			rep, err := r.Checkin(cmd.Context(), name, p, id, opts)
			printCheckinReport(cmd.OutOrStdout(), name, rep, err)
			return err
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report sync status without releasing the lock")
	cmd.Flags().BoolVar(&clean, "clean", false, "remove the local working copy after a successful release")
	cmd.Flags().BoolVar(&abandon, "abandon", false, "release the lock without requiring local and remote to be in sync (start over: unsynced local work is left behind; combine with --clean to remove it)")
	return cmd
}

// printCheckinReport renders the outcome of a checkin: a clean release, a dry-run
// preview, or — when checkin is blocked because the profile isn't fully synced —
// the pending changes that must be synced first. err is the error Checkin
// returned (nil on success). A non-sync error (no marker, remote not mounted,
// etc.) carries its own message via the caller's return, so this prints nothing
// for it; only the "unsynced changes" block is listed here.
func printCheckinReport(w io.Writer, name string, rep lifecycle.Report, err error) {
	pending := len(rep.Pulled) + len(rep.Pushed) + len(rep.RemovedRemote) + len(rep.RemovedLocal) + len(rep.Conflicts)
	switch {
	case rep.DryRun:
		if rep.Abandoned {
			_, _ = fmt.Fprintf(w, "%s: dry-run — abandon would release the lock without checking sync state\n", name)
			return
		}
		if pending == 0 {
			_, _ = fmt.Fprintf(w, "%s: dry-run — in sync; checkin would release the lock\n", name)
			return
		}
		_, _ = fmt.Fprintf(w, "%s: dry-run — %d unsynced change(s); checkin would fail (run 'dibs sync %s' first):\n", name, pending, name)
		printCheckinPending(w, rep)
	case err != nil:
		if pending == 0 {
			return // a non-sync error; the returned err carries the message
		}
		_, _ = fmt.Fprintf(w, "%s: cannot check in — %d unsynced change(s):\n", name, pending)
		printCheckinPending(w, rep)
	case rep.Released && rep.Abandoned:
		_, _ = fmt.Fprintf(w, "%s: abandoned (lock released without sync check)\n", name)
	case rep.Released:
		_, _ = fmt.Fprintf(w, "%s: checked in (lock released)\n", name)
	}
}

// printCheckinPending lists the pending reconcile buckets that block a checkin,
// in the same "verb → path" shape sync uses for its live apply events.
func printCheckinPending(w io.Writer, rep lifecycle.Report) {
	for _, p := range rep.Pushed {
		_, _ = fmt.Fprintf(w, "  push       → %s\n", p)
	}
	for _, p := range rep.Pulled {
		_, _ = fmt.Fprintf(w, "  pull       → %s\n", p)
	}
	for _, p := range rep.RemovedRemote {
		_, _ = fmt.Fprintf(w, "  del-remote → %s\n", p)
	}
	for _, p := range rep.RemovedLocal {
		_, _ = fmt.Fprintf(w, "  del-local  → %s\n", p)
	}
	for _, p := range rep.Conflicts {
		_, _ = fmt.Fprintf(w, "  conflict   ! %s\n", p)
	}
}
