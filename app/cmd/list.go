package cmd

import (
	"fmt"
	"io"
	"sort"

	"github.com/andresbott/netcheckout/internal/config"
	"github.com/spf13/cobra"
)

func newListCmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "list configured profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolvePath(*cfgPath)
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			printProfiles(cmd.OutOrStdout(), cfg)
			return nil
		},
	}
}

func resolvePath(flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	return config.DefaultPath()
}

func printProfiles(w io.Writer, cfg *config.Config) {
	if len(cfg.Profiles) == 0 {
		_, _ = fmt.Fprintln(w, "No profiles configured yet. Run 'netcheckout' to add one.")
		return
	}
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		p := cfg.Profiles[name]
		_, _ = fmt.Fprintf(w, "%s\n  local:  %s\n  remote: %s\n", name, p.LocalRoot, p.RemoteRoot)
	}
}
