package dns

import (
	"context"
	"fmt"
	"net/netip"
)

// resolvedDir exists when systemd-resolved runs.
const resolvedDir = "/run/systemd/resolve"

// resolved registers the zone as a routing domain of the interface with
// systemd-resolved, so only .thawr queries reach the resolver and the
// interface never becomes a default DNS route.
type resolved struct {
	opts RegistrarOptions
}

func (r *resolved) available() bool {
	if !exists(r.opts.Root, resolvedDir) {
		return false
	}
	_, err := r.opts.LookPath("resolvectl")
	return err == nil
}

func (r *resolved) Register(ctx context.Context, iface string, server netip.Addr) (string, error) {
	steps := [][]string{
		{"dns", iface, server.String()},
		{"domain", iface, "~" + r.opts.Zone},
		{"default-route", iface, "false"},
	}
	for _, args := range steps {
		if _, err := r.opts.Runner(ctx, "resolvectl", args...); err != nil {
			return MethodResolved, fmt.Errorf("dns: resolvectl %s: %w", args[0], err)
		}
	}
	return MethodResolved, nil
}

func (r *resolved) Update(context.Context, []Entry) error { return nil }

func (r *resolved) Unregister(ctx context.Context, iface string) error {
	if _, err := r.opts.Runner(ctx, "resolvectl", "revert", iface); err != nil {
		return fmt.Errorf("dns: resolvectl revert: %w", err)
	}
	return nil
}
