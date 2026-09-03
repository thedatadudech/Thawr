package control

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/thedatadudech/thawr/internal/store"
)

// EndpointKind classifies a candidate address.
type EndpointKind int

// Endpoint kinds in probing priority order.
const (
	EndpointLocal EndpointKind = iota + 1
	EndpointReflexive
	EndpointStable
)

// Endpoint is one ip:port a peer may be reached at.
type Endpoint struct {
	Addr netip.AddrPort
	Kind EndpointKind
}

// NetPeer is one visible peer as delivered to a client.
type NetPeer struct {
	ID         string
	Name       string
	Kind       string
	PublicKey  string
	IPv4       netip.Addr
	Online     bool
	Endpoints  []Endpoint
	Symmetric  bool
	Keepalive  bool
	AllowedIPs []netip.Prefix
}

// HubPeer is the server's own WireGuard interface as seen by a peer.
type HubPeer struct {
	PublicKey  string
	Endpoint   string
	AllowedIPs []netip.Prefix
}

// FilterRule allows SrcIPv4 to reach the receiver on a port range.
type FilterRule struct {
	SrcIPv4 netip.Addr
	Proto   string
	PortLo  uint16
	PortHi  uint16
}

// NetMap is one peer's complete view of the network.
type NetMap struct {
	Generation int64
	SelfID     string
	SelfName   string
	SelfIPv4   netip.Addr
	Overlay    netip.Prefix
	Peers      []NetPeer
	Hub        HubPeer
	Filter     []FilterRule
}

// Visibility decides whether two peers may see each other's keys.
type Visibility interface {
	Visible(a, b store.Peer) bool
}

// OwnerVisibility is the rule until spec 006: peers with the same
// non-empty owner see each other.
type OwnerVisibility struct{}

// Visible implements Visibility.
func (OwnerVisibility) Visible(a, b store.Peer) bool {
	return a.OwnerID != "" && a.OwnerID == b.OwnerID
}

// HubConfig describes the server's WireGuard interface for netmaps.
type HubConfig struct {
	PublicKey string
	Endpoint  string
	Address   netip.Addr
	Overlay   netip.Prefix
}

// Presence reports whether a peer is online.
type Presence interface {
	Online(peerID string) bool
}

// NetMapBuilder computes per-peer netmaps from the store and the
// in-memory tables.
type NetMapBuilder struct {
	store      *store.Store
	visibility Visibility
	endpoints  *EndpointTable
	presence   Presence
	hub        HubConfig
	generation func() int64
}

// NewNetMapBuilder builds the netmap builder. generation supplies the
// current sequence number.
func NewNetMapBuilder(st *store.Store, vis Visibility, ep *EndpointTable, pr Presence, hub HubConfig, generation func() int64) *NetMapBuilder {
	return &NetMapBuilder{store: st, visibility: vis, endpoints: ep, presence: pr, hub: hub, generation: generation}
}

// Build returns the netmap for peerID or ErrNotFound when it no longer
// exists. The map contains public keys and addresses only.
func (b *NetMapBuilder) Build(ctx context.Context, peerID string) (NetMap, error) {
	self, err := b.store.Peers().GetByID(ctx, peerID)
	if errors.Is(err, store.ErrNotFound) {
		return NetMap{}, fmt.Errorf("peer %s: %w", peerID, ErrNotFound)
	}
	if err != nil {
		return NetMap{}, err
	}
	all, err := b.store.Peers().List(ctx)
	if err != nil {
		return NetMap{}, err
	}
	selfIP, err := netip.ParseAddr(self.IPv4)
	if err != nil {
		return NetMap{}, fmt.Errorf("control: peer %s address %q: %w", self.ID, self.IPv4, err)
	}
	nm := NetMap{
		Generation: b.generation(),
		SelfID:     self.ID,
		SelfName:   self.Name,
		SelfIPv4:   selfIP,
		Overlay:    b.hub.Overlay,
		Hub: HubPeer{
			PublicKey:  b.hub.PublicKey,
			Endpoint:   b.hub.Endpoint,
			AllowedIPs: []netip.Prefix{netip.PrefixFrom(b.hub.Address, 32)},
		},
		Peers:  []NetPeer{},
		Filter: []FilterRule{},
	}
	for _, p := range all {
		if p.ID == self.ID {
			continue
		}
		ip, err := netip.ParseAddr(p.IPv4)
		if err != nil {
			continue
		}
		// Static peers are reached through the hub, whatever the policy
		// says about visibility; spec 008 fills in hub-side filtering.
		if p.Mode == store.ModeStatic {
			if b.visibility.Visible(self, p) {
				nm.Hub.AllowedIPs = append(nm.Hub.AllowedIPs, netip.PrefixFrom(ip, 32))
			}
			continue
		}
		if !b.visibility.Visible(self, p) {
			continue
		}
		var (
			eps       []Endpoint
			symmetric bool
		)
		if b.endpoints != nil {
			eps, symmetric = b.endpoints.Get(p.ID)
		}
		online := false
		if b.presence != nil {
			online = b.presence.Online(p.ID)
		}
		nm.Peers = append(nm.Peers, NetPeer{
			ID:         p.ID,
			Name:       p.Name,
			Kind:       p.Kind,
			PublicKey:  p.PublicKey,
			IPv4:       ip,
			Online:     online,
			Endpoints:  eps,
			Symmetric:  symmetric,
			AllowedIPs: []netip.Prefix{netip.PrefixFrom(ip, 32)},
		})
	}
	return nm, nil
}
