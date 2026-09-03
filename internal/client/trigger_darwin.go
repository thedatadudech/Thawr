package client

import (
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

// bindToInterface forces the probe packet into the WireGuard interface
// (IP_BOUND_IF) so it enters the tunnel even when the host has another
// route to the peer's overlay address.
func bindToInterface(iface string) func(network, address string, c syscall.RawConn) error {
	return func(_, _ string, c syscall.RawConn) error {
		ifc, err := net.InterfaceByName(iface)
		if err != nil {
			return err
		}
		var serr error
		if err := c.Control(func(fd uintptr) {
			serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, ifc.Index)
		}); err != nil {
			return err
		}
		return serr
	}
}
