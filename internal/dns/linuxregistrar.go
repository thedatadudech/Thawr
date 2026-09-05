package dns

import (
	"context"
	"errors"
	"net/netip"
)

// linuxRegistrar uses systemd-resolved when it runs and the hosts file
// otherwise. The choice is made at Register; Unregister clears both so
// a host that changed resolver setups between runs is left clean.
type linuxRegistrar struct {
	resolved *resolved
	hosts    *hostsFile
	active   Registrar
}

func newLinuxRegistrar(o RegistrarOptions) *linuxRegistrar {
	return &linuxRegistrar{resolved: &resolved{opts: o}, hosts: newHostsFile(o)}
}

func (l *linuxRegistrar) Register(ctx context.Context, iface string, server netip.Addr) (string, error) {
	if l.resolved.available() {
		l.active = l.resolved
	} else {
		l.active = l.hosts
	}
	return l.active.Register(ctx, iface, server)
}

func (l *linuxRegistrar) Update(ctx context.Context, entries []Entry) error {
	if l.active == nil {
		return nil
	}
	return l.active.Update(ctx, entries)
}

func (l *linuxRegistrar) Unregister(ctx context.Context, iface string) error {
	var errs []error
	if l.resolved.available() {
		if err := l.resolved.Unregister(ctx, iface); err != nil {
			errs = append(errs, err)
		}
	}
	if err := l.hosts.Unregister(ctx, iface); err != nil {
		errs = append(errs, err)
	}
	l.active = nil
	return errors.Join(errs...)
}
