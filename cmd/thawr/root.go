package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// errNotImplemented is returned by subcommands whose spec has not been
// implemented yet. Each message names the spec that will replace it.
var errNotImplemented = errors.New("not implemented")

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

func newClientCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "client",
		Short: "Run the node client (up, down, status)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("client: %w (docs/specs/002-peer-enrollment.md)", errNotImplemented)
		},
	}
}

func newAdminCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "admin",
		Short: "Manage users, tokens, peers and policy",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("admin: %w (docs/specs/002-peer-enrollment.md)", errNotImplemented)
		},
	}
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
