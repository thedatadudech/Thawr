//go:build !linux

package server

import "log/slog"

// enableForwarding is a no-op outside Linux: the host's own IP
// forwarding setting decides whether mobile peers reach the mesh.
func enableForwarding(iface string, log *slog.Logger) {
	log.Info("hub forwarding is left to the host on this platform; enable IP forwarding for mobile peers", "interface", iface)
}
