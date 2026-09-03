package client

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path/filepath"
	"time"

	"github.com/thedatadudech/thawr/internal/wg"
)

// Status is the daemon's state as served on the local socket. Spec 007
// renders it; this spec defines the fields.
type Status struct {
	Version    string     `json:"version"`
	Name       string     `json:"name"`
	PeerID     string     `json:"peer_id"`
	IPv4       string     `json:"ipv4"`
	Server     string     `json:"server"`
	Connected  bool       `json:"connected"`
	LastError  string     `json:"last_error,omitempty"`
	Generation int64      `json:"generation"`
	NetMapAt   *time.Time `json:"netmap_at,omitempty"`
	Backend    string     `json:"backend"`
	Interface  string     `json:"interface"`
	ListenPort int        `json:"listen_port"`
	// Endpoints are this device's own candidates as last reported;
	// Symmetric is the NAT verdict.
	Endpoints   []string     `json:"endpoints"`
	Symmetric   bool         `json:"symmetric"`
	Relay       RelayStatus  `json:"relay"`
	Hub         *PeerStatus  `json:"hub,omitempty"`
	Peers       []PeerStatus `json:"peers"`
	RetrievedAt time.Time    `json:"retrieved_at"`
}

// RelayStatus describes the relay connection.
type RelayStatus struct {
	Connected bool `json:"connected"`
	// Peers counts peers currently reached through the relay.
	Peers int `json:"peers"`
}

// PeerStatus joins netmap knowledge with device counters.
type PeerStatus struct {
	Name      string `json:"name"`
	IPv4      string `json:"ipv4"`
	Kind      string `json:"kind,omitempty"`
	Online    bool   `json:"online"`
	PublicKey string `json:"public_key"`
	Endpoint  string `json:"endpoint,omitempty"`
	// Path is idle, probing, direct or unreachable; PathEndpoint is the
	// address the path uses.
	Path         string `json:"path,omitempty"`
	PathEndpoint string `json:"path_endpoint,omitempty"`
	// Probes counts candidates tried since the peer appeared;
	// Candidates are its addresses as delivered by the server.
	Probes        int        `json:"probes"`
	Candidates    []string   `json:"candidates"`
	LastHandshake *time.Time `json:"last_handshake,omitempty"`
	RxBytes       uint64     `json:"rx_bytes"`
	TxBytes       uint64     `json:"tx_bytes"`
}

// Status assembles the current status.
func (d *Daemon) Status(ctx context.Context) Status {
	d.mu.Lock()
	nm, dev, connected, lastErr := d.netmap, d.dev, d.connected, d.lastError
	d.mu.Unlock()
	st := Status{
		Version: d.opts.Version, Name: d.state.Name, PeerID: d.state.PeerID, IPv4: d.state.IPv4, Server: d.state.Server,
		Connected: connected, LastError: lastErr, Interface: d.opts.Interface, ListenPort: d.state.ListenPort,
		Peers: []PeerStatus{}, Endpoints: []string{}, RetrievedAt: d.opts.Now(),
	}
	d.pmu.Lock()
	for _, e := range d.selfEndpoints {
		st.Endpoints = append(st.Endpoints, e.Addr.String())
	}
	st.Symmetric = d.selfSymmetric
	d.pmu.Unlock()
	st.Relay = RelayStatus{Connected: d.relay.Connected(), Peers: d.relay.Peers()}
	stats := map[string]wg.PeerStats{}
	if dev != nil {
		st.Backend = dev.Backend()
		st.Interface = dev.Name()
		if list, err := dev.Stats(ctx); err == nil {
			for _, s := range list {
				stats[s.PublicKey.String()] = s
			}
		}
	}
	if nm == nil {
		return st
	}
	st.Generation = nm.Generation
	if !nm.ReceivedAt.IsZero() {
		t := nm.ReceivedAt
		st.NetMapAt = &t
	}
	fill := func(ps *PeerStatus) {
		if s, ok := stats[ps.PublicKey]; ok {
			if !s.LastHandshake.IsZero() {
				t := s.LastHandshake
				ps.LastHandshake = &t
			}
			ps.RxBytes, ps.TxBytes = s.RxBytes, s.TxBytes
			if s.Endpoint.IsValid() {
				ps.Endpoint = s.Endpoint.String()
			}
		}
	}
	if nm.Hub.PublicKey != "" {
		hub := &PeerStatus{Name: "hub", PublicKey: nm.Hub.PublicKey, Endpoint: nm.Hub.Endpoint, Online: true}
		if len(nm.Hub.AllowedIPs) > 0 {
			hub.IPv4 = nm.Hub.AllowedIPs[0]
		}
		fill(hub)
		st.Hub = hub
	}
	for _, p := range nm.Peers {
		ps := PeerStatus{Name: p.Name, IPv4: p.IPv4, Kind: p.Kind, Online: p.Online, PublicKey: p.PublicKey, Candidates: []string{}}
		for _, e := range p.Endpoints {
			ps.Candidates = append(ps.Candidates, e.Addr+" ("+e.Kind+")")
		}
		fill(&ps)
		if state, ep, probes, ok := d.pathOf(p.ID); ok {
			ps.Path, ps.Probes = string(state), probes
			if ep.IsValid() {
				ps.PathEndpoint = ep.String()
			}
		}
		if ep, err := netip.ParseAddrPort(ps.Endpoint); err == nil && ep.Addr().IsLoopback() {
			ps.Endpoint = "" // the sink, not a real address
		}
		st.Peers = append(st.Peers, ps)
	}
	return st
}

func (d *Daemon) localHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, d.Status(r.Context()))
	})
	mux.HandleFunc("POST /down", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		go d.Stop()
	})
	mux.HandleFunc("POST /ping/{name}", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		res, err := d.Ping(ctx, r.PathValue("name"))
		if errors.Is(err, ErrUnknownPeer) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST /rotate-key", func(w http.ResponseWriter, r *http.Request) {
		if err := d.RotateKey(r.Context()); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func dirOf(path string) string { return filepath.Dir(path) }

// LocalClient talks to a running daemon over its socket.
type LocalClient struct {
	http *http.Client
}

// NewLocalClient returns a client for the socket.
func NewLocalClient(socket string) *LocalClient {
	return &LocalClient{http: &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}}}
}

// Status fetches the daemon status.
func (c *LocalClient) Status(ctx context.Context) (Status, error) {
	var st Status
	return st, c.do(ctx, http.MethodGet, "/status", &st)
}

// Down asks the daemon to stop.
func (c *LocalClient) Down(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/down", nil)
}

// RotateKey asks the daemon to rotate its WireGuard key.
func (c *LocalClient) RotateKey(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/rotate-key", nil)
}

// Ping asks the daemon to establish a path to the named peer and
// returns its state once settled.
func (c *LocalClient) Ping(ctx context.Context, name string) (PathResult, error) {
	var res PathResult
	return res, c.do(ctx, http.MethodPost, "/ping/"+url.PathEscape(name), &res)
}

func (c *LocalClient) do(ctx context.Context, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, "http://thawr"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return &LocalError{Status: resp.StatusCode, Message: e.Error}
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// LocalError is a non-2xx answer from the daemon.
type LocalError struct {
	Status  int
	Message string
}

func (e *LocalError) Error() string { return e.Message }
