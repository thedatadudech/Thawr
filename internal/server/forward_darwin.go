package server

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// enableForwarding turns on IPv4 forwarding on macOS, which has no
// per-interface switch: without it the kernel drops what the hub hands
// it for other peers, so phones reach only the server itself.
func enableForwarding(iface string, log *slog.Logger) (undo func()) {
	undo = func() {}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sysctl", "-w", "net.inet.ip.forwarding=1").CombinedOutput()
	if err != nil {
		log.Warn("hub forwarding not enabled; mobile peers cannot reach the mesh", "interface", iface,
			"err", fmt.Errorf("sysctl: %w: %s", err, strings.TrimSpace(string(out))))
		return undo
	}
	log.Debug("hub forwarding enabled", "interface", iface, "sysctl", strings.TrimSpace(string(out)))
	return undo
}
