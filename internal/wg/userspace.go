package wg

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"

	"github.com/thedatadudech/thawr/internal/stun"
)

// userspaceDevice runs wireguard-go on a TUN interface and configures it
// through the in-process IPC API, so no UAPI socket is needed.
type userspaceDevice struct {
	name string
	mtu  int
	dev  *device.Device
	bind *stunBind
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
		// wireguard-go reports transient conditions (a peer without an
		// endpoint yet, a failed handshake attempt) through Errorf; they
		// are expected during path setup, so they log as warnings and the
		// no-endpoint case, which recurs every keepalive, as debug.
		Errorf: func(format string, args ...any) {
			msg := fmt.Sprintf(format, args...)
			if strings.Contains(msg, "no known endpoint") {
				log.Debug(msg)
				return
			}
			log.Warn(msg)
		},
	}
	bind := newSTUNBind(conn.NewDefaultBind())
	dev := device.NewDevice(tdev, bind, logger)
	return &userspaceDevice{name: name, mtu: opts.MTU, dev: dev, bind: bind, log: log}, nil
}

func (u *userspaceDevice) Configure(ctx context.Context, cfg Config) error {
	current, err := u.Stats(ctx)
	if err != nil {
		return err
	}
	want := make(map[Key]bool, len(cfg.Peers))
	for _, p := range cfg.Peers {
		want[p.PublicKey] = true
	}
	var remove []Key
	for _, s := range current {
		if !want[s.PublicKey] {
			remove = append(remove, s.PublicKey)
		}
	}
	if err := u.dev.IpcSet(renderUAPI(cfg, remove)); err != nil {
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

func (u *userspaceDevice) SetPeer(_ context.Context, p Peer) error {
	if err := u.dev.IpcSet(renderPeerUAPI(p)); err != nil {
		return fmt.Errorf("wg: set peer %s on %s: %w", Fingerprint(p.PublicKey), u.name, err)
	}
	return nil
}

func (u *userspaceDevice) RemovePeer(_ context.Context, key Key) error {
	if err := u.dev.IpcSet(renderRemoveUAPI(key)); err != nil {
		return fmt.Errorf("wg: remove peer %s on %s: %w", Fingerprint(key), u.name, err)
	}
	return nil
}

// STUNTransport sends and receives STUN through the device's own socket,
// so the reflexive address is that of the WireGuard port.
func (u *userspaceDevice) STUNTransport() stun.Transport { return u.bind.transport() }

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
