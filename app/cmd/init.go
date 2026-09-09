package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/candy-tools/dibs/internal/config"
	"github.com/candy-tools/dibs/internal/ident"
	"github.com/spf13/cobra"
)

func newInitCmd(cfgPath *string) *cobra.Command {
	var identity string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "create the initial config file and set the client identity",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolvePath(*cfgPath)
			if err != nil {
				return err
			}
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("config already exists at %s", path)
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			cfg := &config.Config{Profiles: map[string]config.Profile{}}
			id := strings.TrimSpace(identity)
			if id == "" {
				r, err := ident.Resolve(cfg)
				if err != nil {
					return err
				}
				id = r.By
			}
			if err := config.ValidateIdentity(id); err != nil {
				return err
			}
			cfg.Identity = id
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"Created %s\n  identity: %s\nRun 'dibs' to add profiles.\n", path, id)
			return nil
		},
	}
	cmd.Flags().StringVar(&identity, "identity", "", "client identity recorded in checkout markers (default: $USER@$HOSTNAME)")
	return cmd
}
