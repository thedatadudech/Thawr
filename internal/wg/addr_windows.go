package wg

import (
	"context"
	"fmt"
	"net/netip"
	"os/exec"
	"strconv"
	"time"
)

// Windows support is untested against a real host (see docs/TESTING.md);
// it compiles in CI and follows what wireguard-windows does with netsh.
func platformSupported() error { return nil }

// setAddresses configures the interface through netsh: a static address
// with the prefix's mask and a route for the prefix over the interface.
func setAddresses(name string, want []netip.Prefix, mtu int) error {
	for i, p := range want {
		mask := maskString(p.Bits())
		var out []byte
		var err error
		if i == 0 {
			out, err = netsh("interface", "ipv4", "set", "address", "name="+name, "static", p.Addr().String(), mask)
		} else {
			out, err = netsh("interface", "ipv4", "add", "address", "name="+name, p.Addr().String(), mask)
		}
		if err != nil {
			return fmt.Errorf("wg: netsh set address %s on %s: %w: %s", p, name, err, out)
		}
	}
	if mtu > 0 {
		if out, err := netsh("interface", "ipv4", "set", "subinterface", name, "mtu="+strconv.Itoa(mtu), "store=active"); err != nil {
			return fmt.Errorf("wg: netsh set mtu on %s: %w: %s", name, err, out)
		}
	}
	return nil
}

func netsh(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "netsh", args...).CombinedOutput()
}

func maskString(bits int) string {
	m := uint32(0xffffffff) << (32 - bits)
	return fmt.Sprintf("%d.%d.%d.%d", byte(m>>24), byte(m>>16), byte(m>>8), byte(m))
}
