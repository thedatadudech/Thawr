package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/thedatadudech/thawr/internal/config"
	"github.com/thedatadudech/thawr/internal/server"
)

// Exit codes of `thawr server`.
const (
	exitConfigError = 2
)

// envConfig names the config file when --config is not given.
const envConfig = "THAWR_CONFIG"

const defaultConfigPath = "/etc/thawr/server.yaml"

// exitError carries a process exit code out of a cobra command.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

func newServerCmd() *cobra.Command {
	var (
		configPath string
		check      bool
	)
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run the control server (registry, STUN, relay, admin UI)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServer(cmd, configPath, check)
		},
	}
	def := os.Getenv(envConfig)
	if def == "" {
		def = defaultConfigPath
	}
	cmd.Flags().StringVar(&configPath, "config", def, "path to the server YAML config ($"+envConfig+")")
	cmd.Flags().BoolVar(&check, "check", false, "validate config, TLS files and policy, then exit")
	return cmd
}

func runServer(cmd *cobra.Command, configPath string, check bool) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		var verr *config.ValidationError
		if errors.As(err, &verr) {
			return &exitError{code: exitConfigError, err: fmt.Errorf("invalid config %s:\n  %s", configPath, strings.Join(verr.Problems, "\n  "))}
		}
		return &exitError{code: exitConfigError, err: err}
	}
	logger := server.NewLogger(cfg.Log, cmd.ErrOrStderr())
	srv, err := server.New(cfg, server.Deps{Logger: logger, Version: version})
	if err != nil {
		return &exitError{code: exitConfigError, err: err}
	}
	if check {
		if err := srv.Check(); err != nil {
			return &exitError{code: exitConfigError, err: err}
		}
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "config %s ok\n", configPath)
		return err
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	reload := make(chan struct{}, 1)
	stopReload := notifyReload(reload)
	defer stopReload()

	logger.Info("starting thawr server", "version", version, "config", configPath)
	return srv.Run(ctx, reload)
}
