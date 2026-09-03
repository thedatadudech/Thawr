package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/thedatadudech/thawr/internal/client"
	"github.com/thedatadudech/thawr/internal/config"
	"github.com/thedatadudech/thawr/internal/server"
)

// envClientSocket overrides the client's local control socket.
const envClientSocket = "THAWR_CLIENT_SOCKET"

func defaultClientSocket() string {
	if s := os.Getenv(envClientSocket); s != "" {
		return s
	}
	return client.DefaultSocket
}

func newClientCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "client",
		Short: "Enrol this device and run the node client",
	}
	var (
		serverURL, token, fingerprint, name, stateDir, socket, iface, logLevel string
		acceptFingerprint                                                      bool
	)
	up := &cobra.Command{
		Use:   "up",
		Short: "Enrol this device if needed, then run the client in the foreground",
		Long: `Runs the node client: brings up the WireGuard interface, restores the
cached netmap, and keeps the interface in sync with the server until
SIGINT or SIGTERM. When the device is not enrolled yet, --server and
--token enrol it first.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := server.NewLogger(logConfig(logLevel), cmd.ErrOrStderr())
			if _, err := client.LoadState(stateDir); errors.Is(err, client.ErrNotEnrolled) {
				if serverURL == "" || token == "" {
					return &exitError{code: exitConfigError, err: errors.New("not enrolled: --server and --token are required")}
				}
				st, err := client.Enroll(cmd.Context(), client.Options{
					Server: serverURL, Token: token, Fingerprint: fingerprint, AcceptFingerprint: acceptFingerprint,
					Name: name, StateDir: stateDir, Version: version,
				})
				if err != nil {
					var fpErr *client.FingerprintError
					if errors.As(err, &fpErr) {
						return &exitError{code: exitConfigError, err: err}
					}
					return err
				}
				logger.Info("enrolled", "name", st.Name, "ipv4", st.IPv4, "server", st.Server)
			} else if err != nil {
				return err
			} else if token != "" {
				logger.Warn("already enrolled; ignoring --token")
			}
			d, err := client.NewDaemon(client.DaemonOptions{StateDir: stateDir, Socket: socket, Interface: iface, Logger: logger, Version: version})
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return d.Run(ctx)
		},
	}
	up.Flags().StringVar(&serverURL, "server", "", "server URL for enrollment, e.g. https://vpn.example.com")
	up.Flags().StringVar(&token, "token", "", "one-time enrollment token")
	up.Flags().StringVar(&fingerprint, "fingerprint", "", "server TLS fingerprint (sha256:...) from the join command")
	up.Flags().BoolVar(&acceptFingerprint, "accept-fingerprint", false, "trust whatever certificate the server presents now (prints it)")
	up.Flags().StringVar(&name, "name", "", "peer name to request instead of the hostname")
	up.Flags().StringVar(&iface, "interface", "thawr0", "WireGuard interface name (macOS: utun)")
	up.Flags().StringVar(&logLevel, "log-level", "info", "debug, info, warn or error")
	addClientCommonFlags(up, &stateDir, &socket)

	var forget bool
	down := &cobra.Command{
		Use:   "down",
		Short: "Stop the running client; with --forget also remove the enrollment state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			lc := client.NewLocalClient(socket)
			if err := lc.Down(cmd.Context()); err != nil {
				if !forget {
					return fmt.Errorf("client is not running (%w)", err)
				}
			} else {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "client stopping")
			}
			if !forget {
				return nil
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
			_ = os.Remove(stateDirFile(stateDir, client.NetMapFile))
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "forgot enrollment as %s (%s); the server still lists the peer until an admin deletes it\n", st.Name, st.IPv4)
			return err
		},
	}
	down.Flags().BoolVar(&forget, "forget", false, "remove node.key, state.json and the netmap cache")
	addClientCommonFlags(down, &stateDir, &socket)

	var asJSON bool
	status := &cobra.Command{
		Use:   "status",
		Short: "Show the running client's state (JSON; spec 007 adds the table view)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := client.NewLocalClient(socket).Status(cmd.Context())
			if err != nil {
				return &exitError{code: exitNotRunning, err: fmt.Errorf("thawr client is not running (%w)", err)}
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			if err := enc.Encode(st); err != nil {
				return err
			}
			if !st.Connected {
				return &exitError{code: exitNotConnected, err: errors.New("client is running but not connected to the server")}
			}
			return nil
		},
	}
	status.Flags().BoolVar(&asJSON, "json", true, "print JSON (the only format until spec 007)")
	addClientCommonFlags(status, &stateDir, &socket)

	rotate := &cobra.Command{
		Use:   "rotate-key",
		Short: "Generate a new WireGuard key and register it with the server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := client.NewLocalClient(socket).RotateKey(cmd.Context()); err != nil {
				return err
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "key rotated")
			return err
		},
	}
	addClientCommonFlags(rotate, &stateDir, &socket)

	ping := &cobra.Command{
		Use:   "ping <peer>",
		Short: "Establish a path to a peer and report it (JSON; spec 007 adds the table view)",
		Long: `Marks traffic intent toward the named peer so the client probes its
candidates now, waits until the path is settled and prints the result.
Exit code 1 when no direct path was found.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := client.NewLocalClient(socket).Ping(cmd.Context(), args[0])
			if err != nil {
				var le *client.LocalError
				if errors.As(err, &le) {
					return &exitError{code: exitConfigError, err: err}
				}
				return &exitError{code: exitNotRunning, err: fmt.Errorf("thawr client is not running (%w)", err)}
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			if err := enc.Encode(res); err != nil {
				return err
			}
			if res.State != "direct" {
				return &exitError{code: exitNotConnected, err: fmt.Errorf("no direct path to %s (%s)", args[0], res.State)}
			}
			return nil
		},
	}
	addClientCommonFlags(ping, &stateDir, &socket)

	cmd.AddCommand(up, down, status, rotate, ping)
	return cmd
}

func addClientCommonFlags(cmd *cobra.Command, stateDir, socket *string) {
	cmd.Flags().StringVar(stateDir, "state-dir", client.DefaultDir(), "state directory ($"+client.EnvStateDir+")")
	cmd.Flags().StringVar(socket, "socket", defaultClientSocket(), "local control socket ($"+envClientSocket+")")
}

func stateDirFile(dir, name string) string { return dir + string(os.PathSeparator) + name }

// Client exit codes (spec 007).
const (
	exitNotConnected = 1
	exitNotRunning   = 3
)

func logConfig(level string) config.Log { return config.Log{Level: level, Format: "text"} }
