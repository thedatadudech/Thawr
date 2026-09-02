package wg

import (
	"fmt"
	"net"
	"net/netip"

	"github.com/vishvananda/netlink"
)

func platformSupported() error { return nil }

// setAddresses makes the interface's IPv4 addresses equal to want and
// brings the link up.
func setAddresses(name string, want []netip.Prefix, mtu int) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("wg: find %s: %w", name, err)
	}
	have, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("wg: list addresses on %s: %w", name, err)
	}
	wanted := make(map[string]netip.Prefix, len(want))
	for _, p := range want {
		wanted[p.String()] = p
	}
	for _, a := range have {
		key := a.IPNet.String()
		if _, ok := wanted[key]; ok {
			delete(wanted, key)
			continue
		}
		if err := netlink.AddrDel(link, &a); err != nil {
			return fmt.Errorf("wg: remove address %s from %s: %w", key, name, err)
		}
	}
	for key, p := range wanted {
		ipnet := prefixToIPNet(p)
		if err := netlink.AddrAdd(link, &netlink.Addr{IPNet: &ipnet}); err != nil {
			return fmt.Errorf("wg: add address %s to %s: %w", key, name, err)
		}
	}
	if mtu > 0 && link.Attrs().MTU != mtu {
		if err := netlink.LinkSetMTU(link, mtu); err != nil {
			return fmt.Errorf("wg: set mtu on %s: %w", name, err)
		}
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("wg: bring up %s: %w", name, err)
	}
	return nil
}

func prefixToIPNet(p netip.Prefix) net.IPNet {
	addr := p.Addr()
	bits := addr.BitLen()
	return net.IPNet{IP: addr.AsSlice(), Mask: net.CIDRMask(p.Bits(), bits)}
}
