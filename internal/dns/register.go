package dns

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"strings"
)

// Registration methods as reported in the status document.
const (
	MethodResolved     = "resolved"
	MethodHosts        = "hosts"
	MethodResolverFile = "resolver-file"
	MethodNRPT         = "nrpt"
	MethodNone         = "none"
)

// ErrUnsupported is returned by Register on platforms without a way to
// route the zone to the resolver; the resolver still answers.
var ErrUnsupported = errors.New("dns: no resolver registration on this platform")

// Entry is one name in the zone, without the suffix.
type Entry struct {
	Name string
	Addr netip.Addr
}

// Registrar makes the operating system send queries for the zone to the
// resolver at server.
type Registrar interface {
	// Register routes the zone to server on iface and reports the
	// method used. It is preceded by Unregister on every start so the
	// remains of a crashed instance never accumulate.
	Register(ctx context.Context, iface string, server netip.Addr) (method string, err error)
	// Update gives the registrar the current entries; only the hosts
	// file method needs them.
	Update(ctx context.Context, entries []Entry) error
	Unregister(ctx context.Context, iface string) error
}

// Runner executes a command and returns its combined output.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// RegistrarOptions configure NewRegistrar. Zero values select the
// production defaults.
type RegistrarOptions struct {
	// Zone defaults to Zone.
	Zone string
	// Runner defaults to os/exec.
	Runner Runner
	// Root is prefixed to every absolute path the registrar reads or
	// writes (tests).
	Root string
	// LookPath defaults to exec.LookPath.
	LookPath func(file string) (string, error)
	Logger   *slog.Logger
}

func (o RegistrarOptions) withDefaults() RegistrarOptions {
	if o.Zone == "" {
		o.Zone = Zone
	}
	if o.Runner == nil {
		o.Runner = execRunner
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	return o
}

// execRunner is the production Runner.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // fixed resolver tools, arguments built here
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// NewRegistrar returns the registrar for this platform.
func NewRegistrar(o RegistrarOptions) Registrar {
	return newPlatformRegistrar(o.withDefaults())
}

// unsupported is the registrar of platforms without a method.
type unsupported struct{}

func (unsupported) Register(context.Context, string, netip.Addr) (string, error) {
	return MethodNone, ErrUnsupported
}
func (unsupported) Update(context.Context, []Entry) error    { return nil }
func (unsupported) Unregister(context.Context, string) error { return nil }

// exists reports whether path exists under root.
func exists(root, path string) bool {
	_, err := os.Stat(root + path)
	return err == nil
}
