package config

import (
	"fmt"
	"net"
	"net/netip"
	"path/filepath"
)

// Config is the complete server configuration. Every field has a default
// except PublicAddr. Field names and YAML keys mirror
// config/server.example.yaml.
type Config struct {
	// PublicAddr is the host or host:port peers use to reach the server.
	PublicAddr string `yaml:"public_addr"`
	// DataDir holds the SQLite database, keys, TLS files and admin socket.
	DataDir string  `yaml:"data_dir"`
	Listen  Listen  `yaml:"listen"`
	Overlay Overlay `yaml:"overlay"`
	TLS     TLS     `yaml:"tls"`
	// PolicyFile is the YAML ACL file. Absent means empty policy.
	PolicyFile string `yaml:"policy_file"`
	// AdminSocket is the Unix socket path for the local admin API.
	AdminSocket string `yaml:"admin_socket"`
	Log         Log    `yaml:"log"`
	// MinClientVersion is MAJOR.MINOR; empty means the server's own.
	MinClientVersion string `yaml:"min_client_version"`
	Relay            Relay  `yaml:"relay"`
}

// Relay tunes the packet relay built into the server.
type Relay struct {
	// MaxBytesPerSecond limits what one peer may send through the
	// relay; 0 means unlimited.
	MaxBytesPerSecond int `yaml:"max_bytes_per_second"`
}

// Listen holds the listener addresses.
type Listen struct {
	// HTTPS serves gRPC, REST, the admin UI and the relay.
	HTTPS string `yaml:"https"`
	// STUN lists one or two UDP addresses; two let clients detect
	// endpoint-dependent NAT.
	STUN []string `yaml:"stun"`
	// WireGuard is the UDP address of the server's own WireGuard hub.
	WireGuard string `yaml:"wireguard"`
}

// Overlay describes the private address space.
type Overlay struct {
	CIDR      string `yaml:"cidr"`
	Interface string `yaml:"interface"`
}

// TLS selects how the HTTPS certificate is obtained.
type TLS struct {
	// Mode is "self-signed" (generated into DataDir) or "file".
	Mode     string `yaml:"mode"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// Log configures slog output.
type Log struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// TLS modes.
const (
	TLSModeSelfSigned = "self-signed"
	TLSModeFile       = "file"
)

// DefaultDataDir is used when data_dir is not set.
const DefaultDataDir = "/var/lib/thawr"

// Default returns a Config with every default applied and PublicAddr
// empty. Validate fails on it until PublicAddr is set.
func Default() *Config {
	return &Config{
		DataDir: DefaultDataDir,
		Listen: Listen{
			HTTPS:     ":443",
			STUN:      []string{":3478", ":3479"},
			WireGuard: ":51820",
		},
		Overlay: Overlay{
			CIDR:      "100.64.0.0/10",
			Interface: "thawr0",
		},
		TLS:         TLS{Mode: TLSModeSelfSigned},
		PolicyFile:  "/etc/thawr/policy.yaml",
		AdminSocket: filepath.Join(DefaultDataDir, "admin.sock"),
		Log:         Log{Level: "info", Format: "text"},
	}
}

// PublicHost returns the host part of PublicAddr without a port.
func (c *Config) PublicHost() string {
	host, _, err := net.SplitHostPort(c.PublicAddr)
	if err != nil {
		return c.PublicAddr
	}
	return host
}

// OverlayPrefix returns the parsed overlay CIDR. It panics only if the
// config was not validated, which is a programmer error.
func (c *Config) OverlayPrefix() netip.Prefix {
	p, err := netip.ParsePrefix(c.Overlay.CIDR)
	if err != nil {
		panic(fmt.Sprintf("config: OverlayPrefix on unvalidated config: %v", err))
	}
	return p.Masked()
}

// HubAddr is the first usable address of the overlay, assigned to the
// server's own WireGuard interface, with the overlay's prefix length.
func (c *Config) HubAddr() netip.Prefix {
	p := c.OverlayPrefix()
	return netip.PrefixFrom(p.Addr().Next(), p.Bits())
}

// STUNEndpoints are the public host joined with each STUN listen port,
// as advertised to peers.
func (c *Config) STUNEndpoints() []string {
	out := make([]string, 0, len(c.Listen.STUN))
	for _, addr := range c.Listen.STUN {
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			continue
		}
		out = append(out, net.JoinHostPort(c.PublicHost(), port))
	}
	return out
}

// HubEndpoint is the public host joined with the WireGuard listen port,
// as advertised to peers.
func (c *Config) HubEndpoint() string {
	_, port, err := net.SplitHostPort(c.Listen.WireGuard)
	if err != nil {
		port = "51820"
	}
	return net.JoinHostPort(c.PublicHost(), port)
}
