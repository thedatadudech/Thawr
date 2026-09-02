package wg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// Key is a 32-byte WireGuard key (private, public or preshared).
type Key = wgtypes.Key

// GenerateKey returns a new random private key from crypto/rand via
// wgtypes; Thawr never implements key generation itself.
func GenerateKey() (Key, error) {
	k, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return Key{}, fmt.Errorf("wg: generate key: %w", err)
	}
	return k, nil
}

// ParseKey decodes a base64 key as printed by `wg`.
func ParseKey(s string) (Key, error) {
	k, err := wgtypes.ParseKey(s)
	if err != nil {
		return Key{}, fmt.Errorf("wg: parse key: %w", err)
	}
	return k, nil
}

// Fingerprint returns the first 8 hex characters of SHA-256(key), the
// only form in which keys appear in logs.
func Fingerprint(k Key) string {
	sum := sha256.Sum256(k[:])
	return hex.EncodeToString(sum[:4])
}

// Config is the full desired state of a WireGuard interface. Adapters
// diff it against the device; peers not listed are removed.
type Config struct {
	PrivateKey Key
	ListenPort int
	Addresses  []netip.Prefix
	Peers      []Peer
}

// Peer is one remote WireGuard peer.
type Peer struct {
	PublicKey Key
	// Endpoint is zero when unknown (the peer must initiate).
	Endpoint   netip.AddrPort
	AllowedIPs []netip.Prefix
	// Keepalive of zero disables persistent keepalive.
	Keepalive time.Duration
}

// PeerStats is the runtime state of one peer as reported by the device.
type PeerStats struct {
	PublicKey     Key
	Endpoint      netip.AddrPort
	LastHandshake time.Time
	RxBytes       uint64
	TxBytes       uint64
}

// Device is a WireGuard interface, kernel or userspace.
type Device interface {
	// Configure applies the full desired state.
	Configure(ctx context.Context, cfg Config) error
	// Stats reports per-peer counters and handshake times.
	Stats(ctx context.Context) ([]PeerStats, error)
	// Backend is "kernel", "userspace" or "fake".
	Backend() string
	// Name is the interface name actually created (macOS assigns utunN).
	Name() string
	Close() error
}

// Options controls Open.
type Options struct {
	// Name is the requested interface name. On macOS it must be "utun"
	// (kernel-assigned number) or "utunN".
	Name string
	// MTU defaults to 1420.
	MTU    int
	Logger *slog.Logger
	// ForceUserspace skips the kernel adapter even where available.
	ForceUserspace bool
}

// DefaultMTU is WireGuard's conventional MTU for IPv4 over Ethernet.
const DefaultMTU = 1420

// Errors returned by Open.
var (
	// ErrKernelUnavailable means the kernel module is missing on this
	// host; Open falls back to userspace on it.
	ErrKernelUnavailable = errors.New("wg: kernel WireGuard unavailable")
	// ErrPlatformUnsupported means no adapter works on this OS yet.
	ErrPlatformUnsupported = errors.New("wg: platform not supported")
)

func (o Options) withDefaults() Options {
	if o.MTU == 0 {
		o.MTU = DefaultMTU
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return o
}
