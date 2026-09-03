package control

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
)

// ErrExhausted means no overlay address is free.
var ErrExhausted = errors.New("control: overlay address space exhausted")

// NextAddress returns the lowest free host address in prefix, skipping
// the network address, the hub (first usable) and the broadcast address.
func NextAddress(prefix netip.Prefix, allocated []netip.Addr) (netip.Addr, error) {
	if !prefix.Addr().Is4() {
		return netip.Addr{}, fmt.Errorf("control: allocator supports IPv4 only, got %s", prefix)
	}
	prefix = prefix.Masked()
	used := make(map[netip.Addr]bool, len(allocated))
	for _, a := range allocated {
		used[a.Unmap()] = true
	}
	network := prefix.Addr()
	hub := network.Next()
	broadcast := lastAddr(prefix)
	for a := hub.Next(); a.IsValid() && prefix.Contains(a) && a.Less(broadcast); a = a.Next() {
		if !used[a] {
			return a, nil
		}
	}
	return netip.Addr{}, ErrExhausted
}

// lastAddr is the broadcast address of an IPv4 prefix.
func lastAddr(p netip.Prefix) netip.Addr {
	b := p.Addr().As4()
	hostBits := 32 - p.Bits()
	var mask uint32
	if hostBits >= 32 {
		mask = 0xffffffff
	} else {
		mask = (1 << uint(hostBits)) - 1
	}
	v := binary.BigEndian.Uint32(b[:]) | mask
	var out [4]byte
	binary.BigEndian.PutUint32(out[:], v)
	return netip.AddrFrom4(out)
}
