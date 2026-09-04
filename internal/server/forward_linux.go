package server

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// enableForwarding lets the kernel forward packets that arrive on the
// hub interface back out of it, which is how a phone reaches a mesh
// peer. Per-interface forwarding is enough: Linux consults the ingress
// device's flag.
func enableForwarding(iface string, log *slog.Logger) {
	path := filepath.Join("/proc/sys/net/ipv4/conf", iface, "forwarding")
	if err := os.WriteFile(path, []byte("1\n"), 0); err != nil {
		log.Warn("hub forwarding not enabled; mobile peers cannot reach the mesh", "path", path, "err", fmt.Errorf("write: %w", err))
		return
	}
	log.Debug("hub forwarding enabled", "interface", iface)
}
