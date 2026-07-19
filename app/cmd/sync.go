package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/andresbott/dibs/app/metainfo"
	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/internal/ident"
	"github.com/andresbott/dibs/internal/lifecycle"
	"github.com/spf13/cobra"
)

func newSyncCmd(cfgPath *string) *cobra.Command {
	return newSyncCmdWithRunner(cfgPath, lifecycle.Runner{ToolVersion: metainfo.Version})
}

func newSyncCmdWithRunner(cfgPath *string, r lifecycle.Runner) *cobra.Command {
	var force, dryRun, allowDeletes bool
	cmd := &cobra.Command{
		Use:   "sync <profile> [relpath]",
		Short: "reconcile a held checkout in place (push, pull, stop on conflicts)",
		Args:  cobra.RangeArgs(1, 2),
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
			p, ok := cfg.Profiles[name]
			if !ok {
				return fmt.Errorf("profile %q not found", name)
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
			rel := ""
			if len(args) == 2 {
				rel = args[1]
			}
			opts := lifecycle.Options{Force: force, DryRun: dryRun, AllowDeletes: allowDeletes}
			if !dryRun {
				out := cmd.OutOrStdout()
				opts.OnApply = func(e lifecycle.Event) { printApplyEvent(out, e) }
			}
			rep, err := r.Sync(context.Background(), name, p, id, rel, opts)
			if err != nil {
				printReconcileReport(cmd.OutOrStdout(), name, rep) // show conflicts before the non-zero exit
				return err
			}
			printReconcileReport(cmd.OutOrStdout(), name, rep)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "resolve conflicts local-wins (never overrides the lock check)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the reconcile plan without changing anything")
	cmd.Flags().BoolVar(&allowDeletes, "allow-deletes", false, "carry out planned deletions (default: deletes are skipped and reported pending; a full one-side wipe is never applied)")
	return cmd
}

// printApplyEvent renders one applied change live, in the same "verb → side
// path" shape as the status view, as sync carries the reconcile out.
func printApplyEvent(w io.Writer, e lifecycle.Event) {
	verb := "modify"
	switch e.Kind {
	case lifecycle.EventAdd:
		verb = "add"
	case lifecycle.EventDelete:
		verb = "delete"
	}
	side := "local"
	if e.Side == lifecycle.SideRemote {
		side = "remote"
	}
	_, _ = fmt.Fprintf(w, "  %-8s → %-6s  %s\n", verb, side, e.Path)
}

// printReconcileReport renders a sync outcome: a conflict stop, a dry-run plan,
// or the counts of applied changes. (checkin has its own printer — it no longer
// reconciles.)
func printReconcileReport(w io.Writer, name string, rep lifecycle.Report) {
	if len(rep.Conflicts) > 0 {
		_, _ = fmt.Fprintf(w, "%s: %d conflict(s) — nothing written:\n", name, len(rep.Conflicts))
		for _, c := range rep.Conflicts {
			_, _ = fmt.Fprintf(w, "  ! %s\n", c)
		}
		return
	}
	verb := "reconciled"
	if rep.DryRun {
		verb = "dry-run"
	}
	_, _ = fmt.Fprintf(w, "%s: %s (pull %d, push %d, del-remote %d, del-local %d)\n",
		name, verb, len(rep.Pulled), len(rep.Pushed), len(rep.RemovedRemote), len(rep.RemovedLocal))
	if pend := len(rep.PendingRemote) + len(rep.PendingLocal); pend > 0 {
		_, _ = fmt.Fprintf(w, "  %d pending delete(s) — re-run with --allow-deletes to apply them:\n", pend)
		for _, p := range rep.PendingRemote {
			_, _ = fmt.Fprintf(w, "  pending del-remote → %s\n", p)
		}
		for _, p := range rep.PendingLocal {
			_, _ = fmt.Fprintf(w, "  pending del-local  → %s\n", p)
		}
	}
	if len(rep.Ignored) > 0 {
		_, _ = fmt.Fprintf(w, "  %d ignored (never synced):\n", len(rep.Ignored))
		for _, p := range rep.Ignored {
			_, _ = fmt.Fprintf(w, "  ignored → %s\n", p)
		}
	}
}
