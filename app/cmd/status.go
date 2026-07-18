package cmd

import (
	"fmt"
	"io"

	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/internal/sanity"
	"github.com/andresbott/dibs/internal/status"
	"github.com/spf13/cobra"
)

func newStatusCmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status <profile>",
		Short: "preview what a sync would do (push, pull, delete, conflicts)",
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
			profile, ok := cfg.Profiles[name]
			if !ok {
				return fmt.Errorf("profile %q not found", name)
			}
			if err := checkRsync(cmd.Context(), cfg); err != nil {
				return err
			}
			if unlisted, uerr := sanity.UnlistedLocal(profile); uerr == nil && len(unlisted) > 0 {
				w := cmd.ErrOrStderr()
				_, _ = fmt.Fprintln(w, "warning: local content outside this profile's subpaths (will NOT be synced):")
				for _, u := range unlisted {
					_, _ = fmt.Fprintf(w, "  %s\n", u)
				}
			}
			st, err := status.Compute(cmd.Context(), name, profile, cfg.RsyncPath)
			if err != nil {
				return err
			}
			printStatus(cmd.OutOrStdout(), name, profile, st)
			return nil
		},
	}
}

// printStatus renders the three-way reconcile plan a sync would carry out,
// grouped per target.
func printStatus(w io.Writer, name string, p config.Profile, st status.ProfileStatus) {
	_, _ = fmt.Fprintf(w, "%s (local: %s, remote: %s)\n", name, p.LocalRoot, p.RemoteRoot)
	if !st.CheckedOut {
		_, _ = fmt.Fprintln(w, "  not checked out")
		return
	}
	if !st.HasBaseline {
		_, _ = fmt.Fprintln(w, "  checked out, but no local baseline on this machine")
		return
	}
	for _, t := range st.Targets {
		_, _ = fmt.Fprintf(w, "  %s\n", t.Label())
		if t.InSync() {
			_, _ = fmt.Fprintln(w, "    in sync")
			continue
		}
		printChanges(w, "push (local -> remote)", t.Push)
		printChanges(w, "pull (remote -> local)", t.Pull)
		printPaths(w, "del-local (mirror remote delete)", t.LocalDeletes)
		printPaths(w, "del-remote (propagate local delete)", t.RemoteDeletes)
		printPaths(w, "conflicts (changed on both sides)", t.Conflicts)
	}
}

func printChanges(w io.Writer, label string, changes []status.Change) {
	if len(changes) == 0 {
		return
	}
	_, _ = fmt.Fprintf(w, "    %s: %d changes\n", label, len(changes))
	for _, c := range changes {
		mark := "+"
		if c.Modify {
			mark = "M"
		}
		_, _ = fmt.Fprintf(w, "      %s %s\n", mark, c.Path)
	}
}

func printPaths(w io.Writer, label string, paths []string) {
	if len(paths) == 0 {
		return
	}
	_, _ = fmt.Fprintf(w, "    %s: %d\n", label, len(paths))
	for _, p := range paths {
		_, _ = fmt.Fprintf(w, "      - %s\n", p)
	}
}
