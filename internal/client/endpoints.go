package client

import (
	"net"
	"net/netip"
	"sort"
)

// LocalEndpoints lists this host's unicast IPv4 addresses with the
// WireGuard listen port, skipping loopback, link-local and the overlay
// interface itself. Spec 004 adds server-reflexive candidates.
func LocalEndpoints(port int, ignoreIface string) []netip.AddrPort {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []netip.AddrPort
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || ifc.Name == ignoreIface {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(ipnet.IP)
			if !ok {
				continue
			}
			ip = ip.Unmap()
			if !ip.Is4() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
				continue
			}
			out = append(out, netip.AddrPortFrom(ip, uint16(port))) //nolint:gosec // ports are validated 1-65535 by callers
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Addr().Less(out[j].Addr()) })
	if len(out) > 16 {
		out = out[:16]
	}
	return out
}
