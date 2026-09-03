package relay

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Protocol is the HTTP Upgrade token of the relay.
const Protocol = "thawr-relay/1"

// ErrUnauthorized means the server refused the node secret.
var ErrUnauthorized = errors.New("relay: node secret rejected")

// Dial opens GET /relay on serverURL with the HTTP Upgrade handshake
// and returns the raw frame stream. tlsCfg is the pinned configuration
// the client uses for every server connection; HTTP/1.1 is forced
// because Go's server only hands out hijacked connections there.
func Dial(ctx context.Context, serverURL string, tlsCfg *tls.Config, nodeSecret string) (net.Conn, error) {
	if !strings.Contains(serverURL, "://") {
		// The enrollment state keeps the server as host:port.
		serverURL = "https://" + serverURL
	}
	u, err := url.Parse(serverURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("relay: server url %q: %w", serverURL, err)
	}
	host := u.Host
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, "443")
	}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	raw, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, fmt.Errorf("relay: dial %s: %w", host, err)
	}
	cfg := tlsCfg.Clone()
	cfg.NextProtos = []string{"http/1.1"}
	if cfg.ServerName == "" {
		cfg.ServerName = u.Hostname()
	}
	conn := tls.Client(raw, cfg)
	if err := conn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("relay: tls handshake with %s: %w", host, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(serverURL, "/")+"/relay", nil)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("relay: request: %w", err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", Protocol)
	req.Header.Set("Authorization", "Bearer "+nodeSecret)
	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("relay: send upgrade: %w", err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("relay: read upgrade response: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		_ = conn.Close()
		return nil, ErrUnauthorized
	case resp.StatusCode != http.StatusSwitchingProtocols || !strings.EqualFold(resp.Header.Get("Upgrade"), Protocol):
		_ = conn.Close()
		return nil, fmt.Errorf("relay: upgrade refused: %s", resp.Status)
	}
	return BufferedConn(conn, br), nil
}

// BufferedConn returns conn reading first from r, which holds bytes the
// HTTP layer buffered past the upgrade response or request.
func BufferedConn(conn net.Conn, r *bufio.Reader) net.Conn {
	return &bufferedConn{Conn: conn, r: r}
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }
