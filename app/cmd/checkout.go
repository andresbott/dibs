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

func newCheckoutCmd(cfgPath *string) *cobra.Command {
	return newCheckoutCmdWithRunner(cfgPath, lifecycle.Runner{ToolVersion: metainfo.Version})
}

func newCheckoutCmdWithRunner(cfgPath *string, r lifecycle.Runner) *cobra.Command {
	var force, dryRun, resume bool
	cmd := &cobra.Command{
		Use:   "checkout <profile> [relpath]",
		Short: "lock a profile's remote root (files are copied by sync)",
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
			rel := ""
			if len(args) == 2 {
				rel = args[1]
			}
			opts := lifecycle.Options{Force: force, DryRun: dryRun, Resume: resume}
			rep, err := r.Checkout(cmd.Context(), name, p, id, rel, opts)
			if err != nil {
				return err
			}
			printCheckoutReport(cmd.OutOrStdout(), name, rep)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "override an existing lock held by someone else")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "run the checks without writing a marker")
	cmd.Flags().BoolVar(&resume, "resume", false, "adopt the local copy left by a checkin without --clean (validated against the released baseline; refuses if the copy was modified while released)")
	return cmd
}

func printCheckoutReport(w io.Writer, name string, rep lifecycle.Report) {
	switch {
	case rep.DryRun && rep.Resumed:
		_, _ = fmt.Fprintf(w, "%s: dry-run — local copy verified; would resume the checkout\n", name)
	case rep.DryRun:
		_, _ = fmt.Fprintf(w, "%s: dry-run — would write a marker (lock only)\n", name)
	case rep.Resumed:
		_, _ = fmt.Fprintf(w, "%s: resumed (local copy adopted; run 'dibs sync %s' to reconcile any remote changes)\n", name, name)
	default:
		_, _ = fmt.Fprintf(w, "%s: checked out (locked; run 'dibs sync %s' to pull files)\n", name, name)
	}
}
