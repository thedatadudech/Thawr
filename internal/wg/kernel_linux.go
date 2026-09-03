package wg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// kernelDevice drives the in-kernel WireGuard implementation via
// netlink (link and addresses) and wgctrl (keys and peers).
type kernelDevice struct {
	name   string
	mtu    int
	client *wgctrl.Client
	log    *slog.Logger

	mu     sync.Mutex
	closed bool
}

func openKernel(_ context.Context, opts Options) (Device, error) {
	attrs := netlink.NewLinkAttrs()
	attrs.Name = opts.Name
	attrs.MTU = opts.MTU
	link := &netlink.Wireguard{LinkAttrs: attrs}

	// A leftover interface from an unclean shutdown is recreated so the
	// device starts from a known empty state.
	if existing, err := netlink.LinkByName(opts.Name); err == nil {
		if err := netlink.LinkDel(existing); err != nil {
			return nil, fmt.Errorf("wg: remove stale interface %s: %w", opts.Name, err)
		}
	}
	if err := netlink.LinkAdd(link); err != nil {
		if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.ENODEV) ||
			errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %w", ErrKernelUnavailable, err)
		}
		return nil, fmt.Errorf("wg: create kernel interface %s: %w", opts.Name, err)
	}
	client, err := wgctrl.New()
	if err != nil {
		_ = netlink.LinkDel(link)
		return nil, fmt.Errorf("%w: wgctrl: %w", ErrKernelUnavailable, err)
	}
	return &kernelDevice{name: opts.Name, mtu: opts.MTU, client: client, log: opts.Logger.With("interface", opts.Name)}, nil
}

func (k *kernelDevice) Configure(_ context.Context, cfg Config) error {
	wcfg := wgtypes.Config{
		PrivateKey:   &cfg.PrivateKey,
		ReplacePeers: true,
	}
	if cfg.ListenPort > 0 {
		port := cfg.ListenPort
		wcfg.ListenPort = &port
	}
	for _, p := range cfg.Peers {
		pc := wgtypes.PeerConfig{PublicKey: p.PublicKey, ReplaceAllowedIPs: true}
		if p.Endpoint.IsValid() {
			pc.Endpoint = net.UDPAddrFromAddrPort(p.Endpoint)
		}
		if p.Keepalive > 0 {
			ka := p.Keepalive
			pc.PersistentKeepaliveInterval = &ka
		}
		for _, ip := range p.AllowedIPs {
			pc.AllowedIPs = append(pc.AllowedIPs, prefixToIPNet(ip))
		}
		wcfg.Peers = append(wcfg.Peers, pc)
	}
	if err := k.client.ConfigureDevice(k.name, wcfg); err != nil {
		return fmt.Errorf("wg: configure %s: %w", k.name, err)
	}
	if err := setAddresses(k.name, cfg.Addresses, k.mtu); err != nil {
		return err
	}
	return nil
}

func (k *kernelDevice) Stats(context.Context) ([]PeerStats, error) {
	dev, err := k.client.Device(k.name)
	if err != nil {
		return nil, fmt.Errorf("wg: stats %s: %w", k.name, err)
	}
	stats := make([]PeerStats, 0, len(dev.Peers))
	for _, p := range dev.Peers {
		s := PeerStats{
			PublicKey:     p.PublicKey,
			LastHandshake: p.LastHandshakeTime,
			RxBytes:       counter(p.ReceiveBytes),
			TxBytes:       counter(p.TransmitBytes),
		}
		if p.Endpoint != nil {
			s.Endpoint = p.Endpoint.AddrPort()
		}
		stats = append(stats, s)
	}
	return stats, nil
}

func (k *kernelDevice) Backend() string { return "kernel" }

func (k *kernelDevice) Name() string { return k.name }

func (k *kernelDevice) Close() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return nil
	}
	k.closed = true
	var errs []error
	if err := k.client.Close(); err != nil {
		errs = append(errs, fmt.Errorf("wg: close wgctrl: %w", err))
	}
	if link, err := netlink.LinkByName(k.name); err == nil {
		if err := netlink.LinkDel(link); err != nil {
			errs = append(errs, fmt.Errorf("wg: delete %s: %w", k.name, err))
		}
	}
	return errors.Join(errs...)
}

// counter converts a wgctrl byte counter, which is int64 by API design
// but never negative, into the unsigned form used by PeerStats.
func counter(v int64) uint64 {
	if v < 0 {
		return 0
	}
	return uint64(v)
}
