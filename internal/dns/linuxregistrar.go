package dns

import (
	"context"
	"errors"
	"net/netip"
	"sync"
)

// linuxRegistrar uses systemd-resolved when it runs and the hosts file
// otherwise. The choice is made at Register; Unregister clears both so
// a host that changed resolver setups between runs is left clean. The
// daemon may call it from the sync loop and from a key rotation at the
// same time, so every operation runs under one lock.
type linuxRegistrar struct {
	resolved *resolved
	hosts    *hostsFile

	mu     sync.Mutex
	active Registrar
}

func newLinuxRegistrar(o RegistrarOptions) *linuxRegistrar {
	return &linuxRegistrar{resolved: &resolved{opts: o}, hosts: newHostsFile(o)}
}

func (l *linuxRegistrar) Register(ctx context.Context, iface string, server netip.Addr) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.active = l.hosts
	if l.resolved.available() {
		l.active = l.resolved
	}
	return l.active.Register(ctx, iface, server)
}

func (l *linuxRegistrar) Update(ctx context.Context, entries []Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active == nil {
		return nil
	}
	return l.active.Update(ctx, entries)
}

func (l *linuxRegistrar) Unregister(ctx context.Context, iface string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
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
