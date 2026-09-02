package wg

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// userspaceDevice runs wireguard-go on a TUN interface and configures it
// through the in-process IPC API, so no UAPI socket is needed.
type userspaceDevice struct {
	name string
	mtu  int
	dev  *device.Device
	log  *slog.Logger

	mu     sync.Mutex
	closed bool
}

func openUserspace(_ context.Context, opts Options) (Device, error) {
	if err := platformSupported(); err != nil {
		return nil, err
	}
	tdev, err := tun.CreateTUN(opts.Name, opts.MTU)
	if err != nil {
		return nil, fmt.Errorf("wg: create tun %q: %w", opts.Name, err)
	}
	name, err := tdev.Name()
	if err != nil {
		_ = tdev.Close()
		return nil, fmt.Errorf("wg: tun name: %w", err)
	}
	log := opts.Logger.With("interface", name)
	logger := &device.Logger{
		Verbosef: func(format string, args ...any) { log.Debug(fmt.Sprintf(format, args...)) },
		Errorf:   func(format string, args ...any) { log.Error(fmt.Sprintf(format, args...)) },
	}
	dev := device.NewDevice(tdev, conn.NewDefaultBind(), logger)
	return &userspaceDevice{name: name, mtu: opts.MTU, dev: dev, log: log}, nil
}

func (u *userspaceDevice) Configure(_ context.Context, cfg Config) error {
	if err := u.dev.IpcSet(renderUAPI(cfg)); err != nil {
		return fmt.Errorf("wg: configure %s: %w", u.name, err)
	}
	if err := u.dev.Up(); err != nil {
		return fmt.Errorf("wg: bring up %s: %w", u.name, err)
	}
	if err := setAddresses(u.name, cfg.Addresses, u.mtu); err != nil {
		return err
	}
	return nil
}

func (u *userspaceDevice) Stats(context.Context) ([]PeerStats, error) {
	out, err := u.dev.IpcGet()
	if err != nil {
		return nil, fmt.Errorf("wg: stats %s: %w", u.name, err)
	}
	return parseUAPIStats(out)
}

func (u *userspaceDevice) Backend() string { return "userspace" }

func (u *userspaceDevice) Name() string { return u.name }

func (u *userspaceDevice) Close() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return nil
	}
	u.closed = true
	// Closing the device also closes the TUN, which removes the interface.
	u.dev.Close()
	return nil
}
