package client

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// bindToInterface forces the probe packet into the WireGuard interface
// (SO_BINDTODEVICE) so it enters the tunnel even when the host has
// another route to the peer's overlay address.
func bindToInterface(iface string) func(network, address string, c syscall.RawConn) error {
	return func(_, _ string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) { serr = unix.BindToDevice(int(fd), iface) }); err != nil {
			return err
		}
		return serr
	}
}
