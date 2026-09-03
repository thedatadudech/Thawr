package control

import (
	"fmt"
	"net/netip"
	"sync"
	"time"
)

// Endpoint report limits.
const (
	MaxEndpoints = 16
	endpointTTL  = 5 * time.Minute
)

type endpointEntry struct {
	endpoints  []Endpoint
	symmetric  bool
	listenPort uint16
	updated    time.Time
}

type observedEntry struct {
	addr    netip.AddrPort
	updated time.Time
}

// EndpointTable holds each peer's reported candidates in memory, plus
// the address the hub interface last saw its WireGuard packets come
// from (the exact NAT mapping of the client's socket, which a kernel
// WireGuard client cannot learn through STUN itself). Entries expire
// after five minutes without a report; a restart starts empty.
type EndpointTable struct {
	now func() time.Time

	mu       sync.Mutex
	m        map[string]endpointEntry
	observed map[string]observedEntry
}

// NewEndpointTable builds an empty table.
func NewEndpointTable(now func() time.Time) *EndpointTable {
	return &EndpointTable{now: now, m: map[string]endpointEntry{}, observed: map[string]observedEntry{}}
}

// ValidateEndpoints checks a report: at most MaxEndpoints, each a valid
// ip:port with a non-zero port, no loopback or unspecified addresses.
func ValidateEndpoints(eps []Endpoint) error {
	if len(eps) > MaxEndpoints {
		return fmt.Errorf("%w: at most %d endpoints", ErrValidation, MaxEndpoints)
	}
	for _, e := range eps {
		if !e.Addr.IsValid() || e.Addr.Port() == 0 {
			return fmt.Errorf("%w: endpoint %q is not ip:port", ErrValidation, e.Addr)
		}
		a := e.Addr.Addr().Unmap()
		if a.IsLoopback() || a.IsUnspecified() || a.IsMulticast() {
			return fmt.Errorf("%w: endpoint %s is not routable", ErrValidation, e.Addr)
		}
		if e.Kind < EndpointLocal || e.Kind > EndpointStable {
			return fmt.Errorf("%w: endpoint %s has unknown kind", ErrValidation, e.Addr)
		}
	}
	return nil
}

// Set replaces a peer's candidates and reports whether they changed.
func (t *EndpointTable) Set(peerID string, eps []Endpoint, symmetric bool, listenPort uint16) (changed bool, err error) {
	if err := ValidateEndpoints(eps); err != nil {
		return false, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	old, ok := t.m[peerID]
	changed = !ok || old.symmetric != symmetric || old.listenPort != listenPort || !sameEndpoints(old.endpoints, eps)
	t.m[peerID] = endpointEntry{endpoints: append([]Endpoint(nil), eps...), symmetric: symmetric, listenPort: listenPort, updated: t.now()}
	return changed, nil
}

// SetObserved records where the hub last saw peerID's WireGuard packets
// come from and reports whether that changed. Loopback and unspecified
// addresses are ignored.
func (t *EndpointTable) SetObserved(peerID string, addr netip.AddrPort) (changed bool) {
	a := addr.Addr().Unmap()
	if !addr.IsValid() || addr.Port() == 0 || a.IsLoopback() || a.IsUnspecified() {
		return false
	}
	addr = netip.AddrPortFrom(a, addr.Port())
	t.mu.Lock()
	defer t.mu.Unlock()
	old, ok := t.observed[peerID]
	t.observed[peerID] = observedEntry{addr: addr, updated: t.now()}
	return !ok || old.addr != addr
}

// Get returns a copy of a peer's live candidates: the reported ones in
// report order, then the hub-observed address as a reflexive candidate
// unless it is already listed.
func (t *EndpointTable) Get(peerID string) (eps []Endpoint, symmetric bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if e, ok := t.m[peerID]; ok {
		if now.Sub(e.updated) > endpointTTL {
			delete(t.m, peerID)
		} else {
			eps = append([]Endpoint(nil), e.endpoints...)
			symmetric = e.symmetric
		}
	}
	if o, ok := t.observed[peerID]; ok {
		if now.Sub(o.updated) > endpointTTL {
			delete(t.observed, peerID)
		} else if !containsAddr(eps, o.addr) {
			eps = append(eps, Endpoint{Addr: o.addr, Kind: EndpointReflexive})
		}
	}
	return eps, symmetric
}

// Delete forgets a peer.
func (t *EndpointTable) Delete(peerID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, peerID)
	delete(t.observed, peerID)
}

func containsAddr(eps []Endpoint, addr netip.AddrPort) bool {
	for _, e := range eps {
		if e.Addr == addr {
			return true
		}
	}
	return false
}

func sameEndpoints(a, b []Endpoint) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// PathState is how a peer reaches another peer, as reported by it.
type PathState struct {
	PeerID   string
	State    string
	Endpoint string
	Updated  time.Time
}

// PathTable keeps the latest path report per peer for status displays.
type PathTable struct {
	now func() time.Time

	mu sync.Mutex
	m  map[string][]PathState
}

// NewPathTable builds an empty table.
func NewPathTable(now func() time.Time) *PathTable {
	return &PathTable{now: now, m: map[string][]PathState{}}
}

// Set replaces the paths reported by peerID.
func (t *PathTable) Set(peerID string, paths []PathState) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	out := make([]PathState, 0, len(paths))
	for _, p := range paths {
		p.Updated = now
		out = append(out, p)
	}
	t.m[peerID] = out
}

// Get returns the paths reported by peerID.
func (t *PathTable) Get(peerID string) []PathState {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]PathState(nil), t.m[peerID]...)
}

// Delete forgets a peer.
func (t *PathTable) Delete(peerID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, peerID)
}

// ParseEndpoint parses "ip:port" into an Endpoint of the given kind.
func ParseEndpoint(s string, kind EndpointKind) (Endpoint, error) {
	ap, err := netip.ParseAddrPort(s)
	if err != nil {
		return Endpoint{}, fmt.Errorf("%w: endpoint %q: %w", ErrValidation, s, err)
	}
	return Endpoint{Addr: ap, Kind: kind}, nil
}
