//go:build !linux && !darwin

package client

import "syscall"

// bindToInterface has no portable equivalent here; the packet follows
// the host's routing, which the overlay prefix route covers.
func bindToInterface(string) func(network, address string, c syscall.RawConn) error {
	return nil
}
