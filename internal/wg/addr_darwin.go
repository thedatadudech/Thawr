package wg

import (
	"fmt"
	"net/netip"
	"os/exec"
	"strconv"
)

func platformSupported() error { return nil }

// setAddresses configures a utun interface the way wg-quick does on
// macOS: a point-to-point alias per address and a route for its prefix.
func setAddresses(name string, want []netip.Prefix, mtu int) error {
	for _, p := range want {
		addr := p.Addr().String()
		mask := netip.PrefixFrom(p.Addr(), p.Bits()).Masked()
		if out, err := exec.Command("ifconfig", name, "inet", addr, addr, "netmask", maskString(p.Bits()), "alias").CombinedOutput(); err != nil {
			return fmt.Errorf("wg: ifconfig %s %s: %w: %s", name, addr, err, out)
		}
		if out, err := exec.Command("route", "-q", "-n", "add", "-inet", mask.String(), "-interface", name).CombinedOutput(); err != nil {
			return fmt.Errorf("wg: route add %s via %s: %w: %s", mask, name, err, out)
		}
	}
	if mtu > 0 {
		if out, err := exec.Command("ifconfig", name, "mtu", strconv.Itoa(mtu)).CombinedOutput(); err != nil {
			return fmt.Errorf("wg: ifconfig %s mtu: %w: %s", name, err, out)
		}
	}
	if out, err := exec.Command("ifconfig", name, "up").CombinedOutput(); err != nil {
		return fmt.Errorf("wg: ifconfig %s up: %w: %s", name, err, out)
	}
	return nil
}

func maskString(bits int) string {
	m := uint32(0xffffffff) << (32 - bits)
	return fmt.Sprintf("%d.%d.%d.%d", byte(m>>24), byte(m>>16), byte(m>>8), byte(m))
}
