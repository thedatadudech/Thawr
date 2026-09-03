package client

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"
)

// resolveEndpoint turns host:port into an AddrPort, resolving DNS names
// with a short timeout and preferring IPv4.
func resolveEndpoint(hostport string) (netip.AddrPort, error) {
	if ap, err := netip.ParseAddrPort(hostport); err == nil {
		return ap, nil
	}
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("client: endpoint %q: %w", hostport, err)
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("client: endpoint %q port: %w", hostport, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
	if err != nil || len(addrs) == 0 {
		addrs, err = net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return netip.AddrPort{}, fmt.Errorf("client: resolve %q: %w", host, err)
		}
	}
	if len(addrs) == 0 {
		return netip.AddrPort{}, fmt.Errorf("client: resolve %q: no addresses", host)
	}
	return netip.AddrPortFrom(addrs[0].Unmap(), uint16(port)), nil
}
