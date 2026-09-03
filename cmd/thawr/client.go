package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/thedatadudech/thawr/internal/client"
)

func newClientCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "client",
		Short: "Enrol this device and run the node client",
	}
	var (
		server, token, fingerprint, name, stateDir string
		acceptFingerprint                          bool
	)
	up := &cobra.Command{
		Use:   "up",
		Short: "Enrol this device with a one-time token",
		Long: `Enrols this device: generates its WireGuard key, verifies the server
certificate against --fingerprint, redeems the token and stores the
result in the state directory. Running the connected daemon is spec 003.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if server == "" || token == "" {
				return &exitError{code: exitConfigError, err: errors.New("--server and --token are required")}
			}
			st, err := client.Enroll(cmd.Context(), client.Options{
				Server: server, Token: token, Fingerprint: fingerprint, AcceptFingerprint: acceptFingerprint,
				Name: name, StateDir: stateDir, Version: version,
			})
			if err != nil {
				var fpErr *client.FingerprintError
				if errors.As(err, &fpErr) {
					return &exitError{code: exitConfigError, err: err}
				}
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "enrolled as %s, %s (server %s)\n", st.Name, st.IPv4, st.Server)
			return err
		},
	}
	up.Flags().StringVar(&server, "server", "", "server URL, e.g. https://vpn.example.com")
	up.Flags().StringVar(&token, "token", "", "one-time enrollment token")
	up.Flags().StringVar(&fingerprint, "fingerprint", "", "server TLS fingerprint (sha256:...) from the join command")
	up.Flags().BoolVar(&acceptFingerprint, "accept-fingerprint", false, "trust whatever certificate the server presents now (prints it)")
	up.Flags().StringVar(&name, "name", "", "peer name to request instead of the hostname")
	up.Flags().StringVar(&stateDir, "state-dir", client.DefaultDir(), "state directory ($"+client.EnvStateDir+")")

	var forget bool
	down := &cobra.Command{
		Use:   "down",
		Short: "Stop the client; with --forget also remove the enrollment state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !forget {
				// TODO(2026-09-03): spec 003 stops the running daemon here.
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "no daemon to stop yet; use --forget to remove the enrollment state")
				return err
			}
			st, err := client.LoadState(stateDir)
			if errors.Is(err, client.ErrNotEnrolled) {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "not enrolled")
				return err
			}
			if err != nil {
				return err
			}
			if err := client.Forget(stateDir); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "forgot enrollment as %s (%s); the server still lists the peer until an admin deletes it\n", st.Name, st.IPv4)
			return err
		},
	}
	down.Flags().BoolVar(&forget, "forget", false, "remove node.key and state.json")
	down.Flags().StringVar(&stateDir, "state-dir", client.DefaultDir(), "state directory ($"+client.EnvStateDir+")")
	cmd.AddCommand(up, down)
	return cmd
}
