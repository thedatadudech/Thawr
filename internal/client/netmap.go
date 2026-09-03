package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	thawrv1 "github.com/thedatadudech/thawr/internal/api/proto/thawr/v1"
	"github.com/thedatadudech/thawr/internal/wg"
)

// NetMapFile caches the last netmap so WireGuard can be restored before
// the server is reachable.
const NetMapFile = "netmap.json"

// PeerKeepalive is the persistent keepalive toward the hub and toward
// peers flagged keepalive.
const PeerKeepalive = 25 * time.Second

// NetMap is the client-side, JSON-serialisable form of a netmap.
type NetMap struct {
	Generation int64     `json:"generation"`
	SelfID     string    `json:"self_id"`
	SelfName   string    `json:"self_name"`
	SelfIPv4   string    `json:"self_ipv4"`
	Overlay    string    `json:"overlay"`
	Peers      []Peer    `json:"peers"`
	Hub        HubPeer   `json:"hub"`
	ReceivedAt time.Time `json:"received_at"`
}

// Peer is one visible peer.
type Peer struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	PublicKey  string   `json:"public_key"`
	IPv4       string   `json:"ipv4"`
	Online     bool     `json:"online"`
	Endpoints  []string `json:"endpoints"`
	Symmetric  bool     `json:"symmetric"`
	Keepalive  bool     `json:"keepalive"`
	AllowedIPs []string `json:"allowed_ips"`
}

// HubPeer is the server's WireGuard interface.
type HubPeer struct {
	PublicKey  string   `json:"public_key"`
	Endpoint   string   `json:"endpoint"`
	AllowedIPs []string `json:"allowed_ips"`
}

// NetMapFromProto converts a received netmap.
func NetMapFromProto(m *thawrv1.NetMap, now time.Time) NetMap {
	nm := NetMap{
		Generation: m.GetGeneration(),
		SelfID:     m.GetSelf().GetId(),
		SelfName:   m.GetSelf().GetName(),
		SelfIPv4:   m.GetSelf().GetIpv4(),
		Overlay:    m.GetSelf().GetOverlayCidr(),
		Peers:      []Peer{},
		Hub:        HubPeer{PublicKey: m.GetHub().GetPublicKey(), Endpoint: m.GetHub().GetEndpoint(), AllowedIPs: append([]string{}, m.GetHub().GetAllowedIps()...)},
		ReceivedAt: now,
	}
	for _, p := range m.GetPeers() {
		peer := Peer{ID: p.GetId(), Name: p.GetName(), Kind: p.GetKind(), PublicKey: p.GetPublicKey(), IPv4: p.GetIpv4(),
			Online: p.GetOnline(), Symmetric: p.GetSymmetric(), Keepalive: p.GetKeepalive(),
			Endpoints: []string{}, AllowedIPs: append([]string{}, p.GetAllowedIps()...)}
		for _, e := range p.GetEndpoints() {
			peer.Endpoints = append(peer.Endpoints, e.GetAddr())
		}
		nm.Peers = append(nm.Peers, peer)
	}
	return nm
}

// SaveNetMap caches nm in the state directory (mode 0600).
func SaveNetMap(dir string, nm NetMap) error {
	data, err := json.MarshalIndent(nm, "", "  ")
	if err != nil {
		return fmt.Errorf("client: encode netmap: %w", err)
	}
	return writeSecret(dir, NetMapFile, append(data, '\n'))
}

// LoadNetMap reads the cached netmap; ok is false when there is none.
func LoadNetMap(dir string) (nm NetMap, ok bool, err error) {
	data, err := os.ReadFile(filepath.Join(dir, NetMapFile))
	if errors.Is(err, os.ErrNotExist) {
		return NetMap{}, false, nil
	}
	if err != nil {
		return NetMap{}, false, fmt.Errorf("client: read netmap cache: %w", err)
	}
	if err := json.Unmarshal(data, &nm); err != nil {
		return NetMap{}, false, fmt.Errorf("client: parse %s: %w", NetMapFile, err)
	}
	return nm, true, nil
}

// BuildConfig turns a netmap into the full WireGuard configuration of
// this device: every visible peer with its allowed prefixes and, when
// known, its first endpoint candidate; the hub with keepalive.
func BuildConfig(nm NetMap, key wg.Key, listenPort int, overlay netip.Prefix) (wg.Config, error) {
	selfIP, err := netip.ParseAddr(nm.SelfIPv4)
	if err != nil {
		return wg.Config{}, fmt.Errorf("client: self address %q: %w", nm.SelfIPv4, err)
	}
	cfg := wg.Config{
		PrivateKey: key,
		ListenPort: listenPort,
		Addresses:  []netip.Prefix{netip.PrefixFrom(selfIP, overlay.Bits())},
	}
	if nm.Hub.PublicKey != "" {
		hubKey, err := wg.ParseKey(nm.Hub.PublicKey)
		if err != nil {
			return wg.Config{}, fmt.Errorf("client: hub key: %w", err)
		}
		hub := wg.Peer{PublicKey: hubKey, Keepalive: PeerKeepalive}
		if ep, err := resolveEndpoint(nm.Hub.Endpoint); err == nil {
			hub.Endpoint = ep
		}
		hub.AllowedIPs, err = parsePrefixes(nm.Hub.AllowedIPs)
		if err != nil {
			return wg.Config{}, fmt.Errorf("client: hub allowed ips: %w", err)
		}
		cfg.Peers = append(cfg.Peers, hub)
	}
	for _, p := range nm.Peers {
		pub, err := wg.ParseKey(p.PublicKey)
		if err != nil {
			return wg.Config{}, fmt.Errorf("client: peer %s key: %w", p.Name, err)
		}
		peer := wg.Peer{PublicKey: pub}
		if p.Keepalive {
			peer.Keepalive = PeerKeepalive
		}
		peer.AllowedIPs, err = parsePrefixes(p.AllowedIPs)
		if err != nil {
			return wg.Config{}, fmt.Errorf("client: peer %s allowed ips: %w", p.Name, err)
		}
		if ip, err := netip.ParseAddr(p.IPv4); err == nil && len(peer.AllowedIPs) == 0 {
			peer.AllowedIPs = []netip.Prefix{netip.PrefixFrom(ip, 32)}
		}
		// TODO(2026-09-03): spec 004 replaces "first candidate" with the
		// path state machine.
		for _, e := range p.Endpoints {
			if ap, err := netip.ParseAddrPort(e); err == nil {
				peer.Endpoint = ap
				break
			}
		}
		cfg.Peers = append(cfg.Peers, peer)
	}
	return cfg, nil
}

func parsePrefixes(in []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(in))
	for _, s := range in {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", s, err)
		}
		out = append(out, p)
	}
	return out, nil
}
