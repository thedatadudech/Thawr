package stun

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
)

// Transport sends STUN requests and receives responses. The userspace
// WireGuard device provides one bound to its own port; the kernel
// adapter cannot share its socket, so NewSocketTransport binds a
// separate one.
type Transport interface {
	Send(ctx context.Context, dst netip.AddrPort, pkt []byte) error
	// Recv blocks until a packet arrives or ctx ends.
	Recv(ctx context.Context) (pkt []byte, from netip.AddrPort, err error)
	Close() error
}

// MaxPacket is the largest STUN datagram Thawr reads.
const MaxPacket = 1500

type socketTransport struct {
	conn net.PacketConn
}

// NewSocketTransport binds a UDP socket on port (0 for ephemeral).
func NewSocketTransport(ctx context.Context, port int) (Transport, error) {
	var lc net.ListenConfig
	conn, err := lc.ListenPacket(ctx, "udp4", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, fmt.Errorf("stun: bind port %d: %w", port, err)
	}
	return &socketTransport{conn: conn}, nil
}

// LocalPort reports the bound port of a socket transport.
func LocalPort(t Transport) int {
	if s, ok := t.(*socketTransport); ok {
		if ua, ok := s.conn.LocalAddr().(*net.UDPAddr); ok {
			return ua.Port
		}
	}
	return 0
}

func (s *socketTransport) Send(_ context.Context, dst netip.AddrPort, pkt []byte) error {
	if _, err := s.conn.WriteTo(pkt, net.UDPAddrFromAddrPort(dst)); err != nil {
		return fmt.Errorf("stun: send to %s: %w", dst, err)
	}
	return nil
}

func (s *socketTransport) Recv(ctx context.Context) ([]byte, netip.AddrPort, error) {
	// A previous cancellation may have left a deadline in the past.
	_ = s.conn.SetReadDeadline(time.Time{})
	stop := context.AfterFunc(ctx, func() { _ = s.conn.SetReadDeadline(time.Now()) })
	defer stop()
	buf := make([]byte, MaxPacket)
	n, addr, err := s.conn.ReadFrom(buf)
	if err != nil {
		if ctx.Err() != nil {
			return nil, netip.AddrPort{}, ctx.Err()
		}
		return nil, netip.AddrPort{}, fmt.Errorf("stun: receive: %w", err)
	}
	ua, ok := addr.(*net.UDPAddr)
	if !ok {
		return nil, netip.AddrPort{}, errors.New("stun: receive: not a UDP address")
	}
	return buf[:n], ua.AddrPort(), nil
}

func (s *socketTransport) Close() error { return s.conn.Close() }
