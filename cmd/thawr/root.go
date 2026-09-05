package main

import (
	"io"

	"github.com/spf13/cobra"
)

// newRootCmd builds the command tree with the production dependencies.
// Output writers are injected so tests can capture them.
func newRootCmd(stdout, stderr io.Writer) *cobra.Command {
	return newRootCmdWithDeps(stdout, stderr, productionDeps())
}

// newRootCmdWithDeps builds the command tree on top of deps, which tests
// replace with fakes (service manager, privilege check, enrollment).
func newRootCmdWithDeps(stdout, stderr io.Writer, deps cliDeps) *cobra.Command {
	root := &cobra.Command{
		Use:           "thawr",
		Short:         "Self-hosted WireGuard private network. One binary. No cloud. Works offline.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	// Usage errors exit 2 so scripts can tell them from failures.
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &exitError{code: exitConfigError, err: err}
	})
	root.AddCommand(newServerCmd(deps), newClientCmd(deps), newAdminCmd(), newVersionCmd())
	return root
}

// usageArgs turns a positional-argument error into exit code 2.
func usageArgs(check cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := check(cmd, args); err != nil {
			return &exitError{code: exitConfigError, err: err}
		}
		return nil
	}
}
