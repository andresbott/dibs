package cmd

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/andresbott/dibs/app/metainfo"
	"github.com/andresbott/dibs/app/tui"
	"github.com/andresbott/dibs/internal/config"
	"github.com/andresbott/dibs/libs/threewayrsync"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// Execute is the entry point for the command line.
func Execute() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	var cfgPath string

	cmd := &cobra.Command{
		Use:           "dibs",
		Short:         "dibs: check out and check in work directories over network drives",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolvePath(cfgPath)
			if err != nil {
				return err
			}
			return runRoot(cmd, path, term.IsTerminal(int(os.Stdout.Fd())))
		},
	}

	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		_ = cmd.Help()
		return nil
	})

	cmd.PersistentFlags().StringVar(&cfgPath, "config", "", "path to the config file (default: OS config dir)")

	cmd.AddCommand(
		versionCmd(),
		newInitCmd(&cfgPath),
		newListCmd(&cfgPath),
		newStatusCmd(&cfgPath),
		newCheckoutCmd(&cfgPath),
		newSyncCmd(&cfgPath),
		newCheckinCmd(&cfgPath),
	)

	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Version:    %s\n", metainfo.Version)
			fmt.Printf("Build date: %s\n", metainfo.BuildTime)
			fmt.Printf("Commit sha: %s\n", metainfo.ShaVer)
			fmt.Printf("Compiler:   %s\n", runtime.Version())
		},
	}
}

// checkRsync verifies the configured rsync binary (GNU rsync >= 3.1) before a
// command that shells out to it runs, so a macOS openrsync or a broken override
// fails up front with an actionable message instead of mid-transfer.
func checkRsync(ctx context.Context, cfg *config.Config) error {
	_, err := (&threewayrsync.Syncer{Bin: cfg.RsyncPath}).CheckBinary(ctx)
	return err
}

func runRoot(cmd *cobra.Command, path string, interactive bool) error {
	if !interactive {
		cfg, err := config.Load(path)
		if err != nil {
			return err
		}
		printProfiles(cmd.OutOrStdout(), cfg)
		return nil
	}
	return tui.Run(path)
}
