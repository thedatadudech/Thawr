package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// newRootCmd builds the command tree. Output writers are injected so
// tests can capture them.
func newRootCmd(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "thawr",
		Short:         "Self-hosted WireGuard private network. One binary. No cloud. Works offline.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(newServerCmd(), newClientCmd(), newAdminCmd(), newVersionCmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "thawr %s\n", version)
			return err
		},
	}
}
