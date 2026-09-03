package wg

import (
	"fmt"
	"net/netip"
)

// TODO(2026-09-02): Windows address configuration via winipcfg lands
// with the client work in spec 003; until then Open fails on Windows.
func platformSupported() error {
	return fmt.Errorf("%w: windows address configuration not implemented", ErrPlatformUnsupported)
}

func setAddresses(string, []netip.Prefix, int) error {
	return platformSupported()
}
