package client

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"testing/fstest"
	"time"

	"github.com/thedatadudech/thawr/internal/api"
	"github.com/thedatadudech/thawr/internal/control"
	"github.com/thedatadudech/thawr/internal/store"
	"github.com/thedatadudech/thawr/internal/wg"
	"github.com/thedatadudech/thawr/internal/wg/wgtest"
)

// controlPlane is a real server-side control plane over a TLS HTTP/2
// test server, enough for the daemon to enrol and sync.
type controlPlane struct {
	t        *testing.T
	st       *store.Store
	hub      *control.Hub
	registry *control.Registry
	tokens   *control.Tokens
	admin    control.Principal
	ts       *httptest.Server
	fp       string
	hubKey   wg.Key
}

func newControlPlane(t *testing.T) *controlPlane {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	users, err := control.NewUsers(st, time.Now, quiet)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := users.Create(ctx, "markus", store.RoleAdmin, "adminpassword")
	if err != nil {
		t.Fatal(err)
	}
	hub, err := control.NewHub(ctx, st, time.Now, quiet, control.HubOptions{Coalesce: 10 * time.Millisecond, KeepaliveInterval: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	overlay := netip.MustParsePrefix("100.64.0.0/10")
	hubKey, _ := wg.GenerateKey()
	registry := control.NewRegistry(st, quiet).WithNotifier(hub)
	enroller := control.NewEnroller(st, time.Now, quiet, overlay, "").WithNotifier(hub)
	endpoints := control.NewEndpointTable(time.Now)
	hubCfg := control.HubConfig{PublicKey: hubKey.PublicKey().String(), Endpoint: "127.0.0.1:51820", Address: netip.MustParseAddr("100.64.0.1"), Overlay: overlay}
	builder := control.NewNetMapBuilder(st, control.OwnerVisibility{}, endpoints, hub, hubCfg, hub.Generation)
	grpcSrv, err := api.NewGRPC(api.GRPCDeps{
		Enroller: enroller, Hub: api.HubInfo{PublicKey: hubCfg.PublicKey, Endpoint: hubCfg.Endpoint, Overlay: overlay}, Logger: quiet,
		NodeAuth: registry, NetMaps: builder, Sync: hub, Peers: registry, Endpoints: endpoints, Paths: control.NewPathTable(time.Now),
	})
	if err != nil {
		t.Fatal(err)
	}
	rest, err := api.NewREST(api.RESTDeps{Status: statusStub{}, UI: fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("x")}}})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(api.Combine(grpcSrv, rest))
	ts.EnableHTTP2 = true
	ts.TLS = &tls.Config{MinVersion: tls.VersionTLS13, NextProtos: []string{"h2"}}
	ts.StartTLS()
	t.Cleanup(ts.Close)
	return &controlPlane{t: t, st: st, hub: hub, registry: registry, tokens: control.NewTokens(st, time.Now, quiet),
		admin: control.Principal{UserID: admin.ID, Name: admin.Name, Role: admin.Role}, ts: ts, fp: Fingerprint(ts.Certificate().Raw), hubKey: hubKey}
}

// enrol registers a device into dir and returns its state.
func (cp *controlPlane) enrol(dir, host string) State {
	cp.t.Helper()
	tok, err := cp.tokens.Create(context.Background(), cp.admin, control.TokenRequest{OwnerName: "markus", Kind: "human"})
	if err != nil {
		cp.t.Fatal(err)
	}
	st, err := Enroll(context.Background(), Options{Server: cp.ts.URL, Token: tok.Secret, Fingerprint: cp.fp, StateDir: dir, Hostname: host, Version: "0.1.0"})
	if err != nil {
		cp.t.Fatalf("enrol %s: %v", host, err)
	}
	return st
}

func startDaemon(t *testing.T, dir string) (*Daemon, *wgtest.Fake, func()) {
	t.Helper()
	fake := wgtest.New("thawr0")
	d, err := NewDaemon(DaemonOptions{
		StateDir: dir, Socket: filepath.Join(dir, "c.sock"), Interface: "thawr0",
		OpenDevice: func(context.Context, wg.Options) (wg.Device, error) { return fake, nil },
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "0.1.0",
		MinBackoff: 50 * time.Millisecond, MaxBackoff: 200 * time.Millisecond, EndpointInterval: time.Hour,
		Endpoints: func(port int, _ string) []netip.AddrPort {
			return []netip.AddrPort{netip.AddrPortFrom(netip.MustParseAddr("192.0.2.10"), uint16(port))}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	return d, fake, func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("daemon did not stop")
		}
	}
}

func waitApplied(t *testing.T, d *Daemon, cond func(NetMap) bool) NetMap {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case nm := <-d.Applied():
			if cond(nm) {
				return nm
			}
		case <-deadline:
			t.Fatal("no matching netmap within 5s")
		}
	}
}

func TestBuildConfig(t *testing.T) {
	key, _ := wg.GenerateKey()
	hubKey, _ := wg.GenerateKey()
	peerKey, _ := wg.GenerateKey()
	nm := NetMap{
		SelfIPv4: "100.64.0.7",
		Hub:      HubPeer{PublicKey: hubKey.PublicKey().String(), Endpoint: "203.0.113.1:51820", AllowedIPs: []string{"100.64.0.1/32", "100.64.0.21/32"}},
		Peers: []Peer{{Name: "nas", PublicKey: peerKey.PublicKey().String(), IPv4: "100.64.0.3", Endpoints: []string{"192.168.1.5:41820", "10.0.0.5:41820"},
			AllowedIPs: []string{"100.64.0.3/32"}, Keepalive: true}},
	}
	cfg, err := BuildConfig(nm, key, 41820, netip.MustParsePrefix("100.64.0.0/10"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PrivateKey != key || cfg.ListenPort != 41820 || len(cfg.Addresses) != 1 || cfg.Addresses[0].String() != "100.64.0.7/10" {
		t.Errorf("interface config: %+v", cfg)
	}
	if len(cfg.Peers) != 2 {
		t.Fatalf("peers: %d", len(cfg.Peers))
	}
	hub := cfg.Peers[0]
	if hub.PublicKey != hubKey.PublicKey() || hub.Endpoint.String() != "203.0.113.1:51820" || hub.Keepalive != PeerKeepalive || len(hub.AllowedIPs) != 2 {
		t.Errorf("hub: %+v", hub)
	}
	nas := cfg.Peers[1]
	if nas.PublicKey != peerKey.PublicKey() || nas.Endpoint.String() != "192.168.1.5:41820" || nas.Keepalive != PeerKeepalive || nas.AllowedIPs[0].String() != "100.64.0.3/32" {
		t.Errorf("nas: %+v", nas)
	}
	if _, err := BuildConfig(NetMap{SelfIPv4: "bad"}, key, 1, netip.MustParsePrefix("100.64.0.0/10")); err == nil {
		t.Error("bad self address accepted")
	}
	nm.Peers[0].PublicKey = "junk"
	if _, err := BuildConfig(nm, key, 1, netip.MustParsePrefix("100.64.0.0/10")); err == nil {
		t.Error("bad peer key accepted")
	}
}

func TestDaemonSyncAppliesAndCaches(t *testing.T) {
	cp := newControlPlane(t)
	dirA, dirB := t.TempDir(), t.TempDir()
	cp.enrol(dirA, "a")
	d, fake, stop := startDaemon(t, dirA)
	defer stop()

	first := waitApplied(t, d, func(nm NetMap) bool { return nm.Hub.PublicKey != "" })
	if first.SelfName != "a" || first.Hub.PublicKey != cp.hubKey.PublicKey().String() || len(first.Peers) != 0 {
		t.Errorf("first map: %+v", first)
	}
	last, _ := fake.Last()
	if len(last.Peers) != 1 || last.Peers[0].PublicKey != cp.hubKey.PublicKey() || last.Addresses[0].String() != "100.64.0.2/10" {
		t.Errorf("device after first map: %+v", last)
	}

	stB := cp.enrol(dirB, "b")
	withB := waitApplied(t, d, func(nm NetMap) bool { return len(nm.Peers) == 1 })
	if withB.Peers[0].Name != "b" || withB.Peers[0].IPv4 != stB.IPv4 {
		t.Errorf("map with b: %+v", withB.Peers)
	}
	last, _ = fake.Last()
	if len(last.Peers) != 2 {
		t.Errorf("device peers after b: %d", len(last.Peers))
	}
	cached, ok, err := LoadNetMap(dirA)
	if err != nil || !ok || cached.Generation != withB.Generation {
		t.Errorf("cache: ok=%v gen=%d err=%v", ok, cached.Generation, err)
	}
	if fi, _ := os.Stat(filepath.Join(dirA, NetMapFile)); runtime.GOOS != "windows" && fi != nil && fi.Mode().Perm() != 0o600 {
		t.Errorf("netmap cache mode %o", fi.Mode().Perm())
	}

	// Status over the socket.
	lc := NewLocalClient(filepath.Join(dirA, "c.sock"))
	st, err := lc.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !st.Connected || st.Name != "a" || len(st.Peers) != 1 || st.Peers[0].Name != "b" || st.Hub == nil || st.Backend != "fake" {
		t.Errorf("status: %+v", st)
	}

	// Peer removal reaches the device within 5 s.
	start := time.Now()
	if err := cp.registry.Delete(context.Background(), cp.admin, "b"); err != nil {
		t.Fatal(err)
	}
	waitApplied(t, d, func(nm NetMap) bool { return len(nm.Peers) == 0 })
	if time.Since(start) > 5*time.Second {
		t.Error("removal took longer than 5s")
	}
	last, _ = fake.Last()
	if len(last.Peers) != 1 {
		t.Errorf("device still has removed peer: %+v", last.Peers)
	}
}

func TestDaemonAppliesCachedNetMapFirst(t *testing.T) {
	cp := newControlPlane(t)
	dir := t.TempDir()
	st := cp.enrol(dir, "a")
	cached := NetMap{Generation: 9, SelfID: st.PeerID, SelfName: st.Name, SelfIPv4: st.IPv4, Overlay: st.OverlayCIDR,
		Hub: HubPeer{PublicKey: cp.hubKey.PublicKey().String(), Endpoint: "127.0.0.1:51820", AllowedIPs: []string{"100.64.0.1/32"}}}
	if err := SaveNetMap(dir, cached); err != nil {
		t.Fatal(err)
	}
	cp.ts.Close() // server unreachable
	d, fake, stop := startDaemon(t, dir)
	defer stop()
	nm := waitApplied(t, d, func(nm NetMap) bool { return nm.Generation == 9 })
	if nm.Hub.PublicKey != cached.Hub.PublicKey {
		t.Errorf("cached map not applied: %+v", nm)
	}
	if cfgs := len(fake.Configs); cfgs < 1 {
		t.Errorf("device not configured from cache")
	}
	time.Sleep(300 * time.Millisecond)
	lc := NewLocalClient(filepath.Join(dir, "c.sock"))
	status, err := lc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Connected || status.LastError == "" || status.Generation != 9 {
		t.Errorf("status while offline: %+v", status)
	}
}

func TestDaemonRotateKeyAndDown(t *testing.T) {
	cp := newControlPlane(t)
	dir := t.TempDir()
	st := cp.enrol(dir, "a")
	d, fake, stop := startDaemon(t, dir)
	waitApplied(t, d, func(nm NetMap) bool { return nm.Hub.PublicKey != "" })
	oldKey, _ := LoadKey(dir)

	lc := NewLocalClient(filepath.Join(dir, "c.sock"))
	if err := lc.RotateKey(context.Background()); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	newKey, _ := LoadKey(dir)
	if newKey == oldKey {
		t.Error("key file not rotated")
	}
	stored, _ := cp.st.Peers().GetByID(context.Background(), st.PeerID)
	if stored.PublicKey != newKey.PublicKey().String() {
		t.Error("server does not have the new key")
	}
	last, _ := fake.Last()
	if last.PrivateKey != newKey {
		t.Error("device not reconfigured with the new key")
	}

	if err := lc.Down(context.Background()); err != nil {
		t.Fatalf("down: %v", err)
	}
	stop()
	if _, err := os.Stat(filepath.Join(dir, "c.sock")); !errors.Is(err, os.ErrNotExist) {
		t.Error("socket not removed after stop")
	}
	if !fake.Closed() {
		t.Error("device not closed after stop")
	}
}

func TestDaemonRemovedPeerBacksOff(t *testing.T) {
	cp := newControlPlane(t)
	dir := t.TempDir()
	cp.enrol(dir, "a")
	d, _, stop := startDaemon(t, dir)
	defer stop()
	waitApplied(t, d, func(nm NetMap) bool { return nm.Hub.PublicKey != "" })
	if err := cp.registry.Delete(context.Background(), cp.admin, "a"); err != nil {
		t.Fatal(err)
	}
	lc := NewLocalClient(filepath.Join(dir, "c.sock"))
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, err := lc.Status(context.Background())
		if err == nil && !st.Connected && st.LastError != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon did not report removal: %+v", st)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestBackoffDelay(t *testing.T) {
	minD, maxD := time.Second, 60*time.Second
	prev := time.Duration(0)
	for attempt := 1; attempt <= 10; attempt++ {
		d := backoffDelay(attempt, minD, maxD)
		if d < minD || d > maxD {
			t.Errorf("attempt %d: %s out of bounds", attempt, d)
		}
		if attempt <= 6 && d < prev/2 {
			t.Errorf("attempt %d: %s not growing from %s", attempt, d, prev)
		}
		prev = d
	}
	if d := backoffDelay(20, minD, maxD); d < 48*time.Second {
		t.Errorf("late attempt %s should sit at the cap", d)
	}
}

func TestNewDaemonRequiresEnrollment(t *testing.T) {
	if _, err := NewDaemon(DaemonOptions{StateDir: t.TempDir()}); !errors.Is(err, ErrNotEnrolled) {
		t.Errorf("got %v, want ErrNotEnrolled", err)
	}
}

func TestLocalEndpoints(t *testing.T) {
	eps := LocalEndpoints(41820, "")
	for _, e := range eps {
		if e.Port() != 41820 || e.Addr().IsLoopback() || !e.Addr().Is4() {
			t.Errorf("bad candidate %s", e)
		}
	}
	var _ http.Handler = http.NewServeMux()
}
