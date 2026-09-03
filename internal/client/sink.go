package client

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sync/atomic"
)

// sink is a loopback UDP socket a peer's WireGuard endpoint points at
// while no path exists. WireGuard sends its handshake initiation there
// as soon as traffic for the peer is queued, which is how the daemon
// learns traffic intent without any packet leaving the host. The sink
// never transmits.
type sink struct {
	conn   net.PacketConn
	addr   netip.AddrPort
	intent atomic.Bool
}

func newSink(ctx context.Context) (*sink, error) {
	var lc net.ListenConfig
	conn, err := lc.ListenPacket(ctx, "udp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("client: sink socket: %w", err)
	}
	ua, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		_ = conn.Close()
		return nil, fmt.Errorf("client: sink socket: unexpected address %v", conn.LocalAddr())
	}
	s := &sink{conn: conn, addr: ua.AddrPort()}
	go s.drain()
	return s, nil
}

func (s *sink) drain() {
	buf := make([]byte, 2048)
	for {
		if _, _, err := s.conn.ReadFrom(buf); err != nil {
			return
		}
		s.intent.Store(true)
	}
}

// endpoint is the loopback address to configure as the peer's endpoint.
func (s *sink) endpoint() netip.AddrPort { return s.addr }

// takeIntent reports and clears whether a packet arrived since the
// last call.
func (s *sink) takeIntent() bool { return s.intent.Swap(false) }

func (s *sink) close() { _ = s.conn.Close() }
