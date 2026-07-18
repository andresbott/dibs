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

func newCheckoutCmd(cfgPath *string) *cobra.Command {
	return newCheckoutCmdWithRunner(cfgPath, lifecycle.Runner{ToolVersion: metainfo.Version})
}

func newCheckoutCmdWithRunner(cfgPath *string, r lifecycle.Runner) *cobra.Command {
	var force, dryRun bool
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
			opts := lifecycle.Options{Force: force, DryRun: dryRun}
			rep, err := r.Checkout(context.Background(), name, p, id, rel, opts)
			if err != nil {
				return err
			}
			printCheckoutReport(cmd.OutOrStdout(), name, rep)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "override an existing lock held by someone else")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "run the checks without writing a marker")
	return cmd
}

func printCheckoutReport(w io.Writer, name string, rep lifecycle.Report) {
	if rep.DryRun {
		_, _ = fmt.Fprintf(w, "%s: dry-run — would write a marker (lock only)\n", name)
		return
	}
	_, _ = fmt.Fprintf(w, "%s: checked out (locked; run 'dibs sync %s' to pull files)\n", name, name)
}
