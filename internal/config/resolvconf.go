package config

import (
	"bufio"
	"io"
	"net/netip"
	"os"
	"runtime"
	"strings"
)

// resolvConfPath is where Unix-like systems list their nameservers.
const resolvConfPath = "/etc/resolv.conf"

// ParseResolvConf returns the nameserver entries of a resolv.conf, port
// 53, in file order; comments, search domains and options are ignored.
// Loopback stubs (systemd-resolved's 127.0.0.53) are kept: they are the
// host's working resolver.
func ParseResolvConf(r io.Reader) []netip.AddrPort {
	var out []netip.AddrPort
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexAny(line, "#;"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		// glibc accepts "addr%iface" for link-local IPv6; drop the zone.
		addr, _, _ := strings.Cut(fields[1], "%")
		if a, err := netip.ParseAddr(addr); err == nil {
			out = append(out, netip.AddrPortFrom(a, 53))
		}
	}
	return out
}

// SystemUpstreams reads the host's resolvers from /etc/resolv.conf on
// Unix-like systems; on Windows, or when the file is unreadable or
// lists none, it returns nil.
func SystemUpstreams() []netip.AddrPort {
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := os.Open(resolvConfPath)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	return ParseResolvConf(f)
}
