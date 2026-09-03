package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"

	"github.com/thedatadudech/thawr/internal/api"
	"github.com/thedatadudech/thawr/internal/config"
	"github.com/thedatadudech/thawr/internal/control"
	"github.com/thedatadudech/thawr/internal/control/policy"
	"github.com/thedatadudech/thawr/internal/store"
	"github.com/thedatadudech/thawr/internal/wg"
	"github.com/thedatadudech/thawr/web"
)

// DBFile is the SQLite database name inside data_dir.
const DBFile = "thawr.db"

// shutdownTimeout bounds a clean stop; the spec requires exit within 5 s.
const shutdownTimeout = 5 * time.Second

// Deps are the injectable collaborators of a Server. Zero values select
// the production implementations.
type Deps struct {
	// OpenDevice creates the WireGuard hub interface; defaults to wg.Open.
	OpenDevice func(ctx context.Context, opts wg.Options) (wg.Device, error)
	Now        func() time.Time
	Logger     *slog.Logger
	// Version is reported by the status endpoint.
	Version string
	// UI is the embedded admin UI; defaults to web.Static().
	UI fs.FS
	// HubOptions tune presence timing (tests).
	HubOptions control.HubOptions
}

// Server is the composed control server.
type Server struct {
	cfg  *config.Config
	deps Deps
	log  *slog.Logger

	st             *store.Store
	hubKey         wg.Key
	tlsCert        tls.Certificate
	tlsFingerprint string
	device         wg.Device
	policy         atomic.Pointer[policy.Policy]
	startedAt      time.Time

	users     *control.Users
	tokens    *control.Tokens
	enroller  *control.Enroller
	registry  *control.Registry
	sessions  *api.Sessions
	hub       *control.Hub
	endpoints *control.EndpointTable
	paths     *control.PathTable
	netmaps   *control.NetMapBuilder

	ready     chan struct{}
	readyOnce sync.Once
	httpsAddr atomic.Pointer[string]
}

// New validates cfg and prepares a Server. Nothing is touched on disk
// until Run or Check.
func New(cfg *config.Config, deps Deps) (*Server, error) {
	if cfg == nil {
		return nil, errors.New("server: nil config")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if deps.OpenDevice == nil {
		deps.OpenDevice = wg.Open
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Version == "" {
		deps.Version = "dev"
	}
	if deps.UI == nil {
		ui, err := web.Static()
		if err != nil {
			return nil, fmt.Errorf("server: embedded ui: %w", err)
		}
		deps.UI = ui
	}
	return &Server{cfg: cfg, deps: deps, log: deps.Logger, ready: make(chan struct{})}, nil
}

// Ready is closed once every listener is up.
func (s *Server) Ready() <-chan struct{} { return s.ready }

// HTTPSAddr returns the bound HTTPS address once ready (useful when the
// config asked for port 0).
func (s *Server) HTTPSAddr() string {
	if p := s.httpsAddr.Load(); p != nil {
		return *p
	}
	return ""
}

// Policy returns the currently loaded policy.
func (s *Server) Policy() *policy.Policy { return s.policy.Load() }

// Check validates everything that can be validated without starting:
// TLS files in file mode and the policy file. It never touches data_dir.
func (s *Server) Check() error {
	var errs []error
	if s.cfg.TLS.Mode == config.TLSModeFile {
		if _, err := tls.LoadX509KeyPair(s.cfg.TLS.CertFile, s.cfg.TLS.KeyFile); err != nil {
			errs = append(errs, fmt.Errorf("tls: %w", err))
		}
	}
	if _, err := policy.Load(s.cfg.PolicyFile); err != nil && !errors.Is(err, policy.ErrNotFound) {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// Run starts the server and blocks until ctx is cancelled, then shuts
// down within shutdownTimeout. A value on reload re-reads the policy
// file. Run returns nil after a clean shutdown.
func (s *Server) Run(ctx context.Context, reload <-chan struct{}) (err error) {
	s.startedAt = s.deps.Now()
	cfg := s.cfg

	created, err := ensureDataDir(cfg.DataDir)
	if err != nil {
		return err
	}
	if created {
		s.log.Info("data dir created", "path", cfg.DataDir)
	}

	s.st, err = store.Open(ctx, filepath.Join(cfg.DataDir, DBFile))
	if err != nil {
		return err
	}
	defer s.closeQuietly("store", s.st.Close)
	schema, err := s.st.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	s.log.Info("database ready", "path", filepath.Join(cfg.DataDir, DBFile), "schema_version", schema)

	keyPath := filepath.Join(cfg.DataDir, ServerKeyFile)
	s.hubKey, created, err = loadOrCreateServerKey(ctx, keyPath, s.st.Meta())
	if err != nil {
		return err
	}
	s.log.Info("server key ready", "path", keyPath, "generated", created, "fingerprint", wg.Fingerprint(s.hubKey.PublicKey()))

	s.tlsCert, created, err = loadTLS(cfg, s.deps.Now())
	if err != nil {
		return err
	}
	s.tlsFingerprint = tlsFingerprint(s.tlsCert)
	s.log.Info("tls certificate ready", "mode", cfg.TLS.Mode, "generated", created, "fingerprint", s.tlsFingerprint)

	if err := s.startHub(ctx); err != nil {
		return err
	}
	defer s.closeQuietly("wireguard", s.device.Close)

	if err := s.loadPolicy(); err != nil {
		return err
	}

	if err := s.buildServices(ctx); err != nil {
		return err
	}
	grpcSrv, err := api.NewGRPC(api.GRPCDeps{
		Enroller:  s.enroller,
		Hub:       api.HubInfo{PublicKey: s.hubKey.PublicKey().String(), Endpoint: cfg.HubEndpoint(), Overlay: cfg.OverlayPrefix()},
		Version:   s.deps.Version,
		Logger:    s.log,
		NodeAuth:  s.registry,
		NetMaps:   s.netmaps,
		Sync:      s.hub,
		Peers:     s.registry,
		Endpoints: s.endpoints,
		Paths:     s.paths,
	})
	if err != nil {
		return err
	}
	restDeps := api.RESTDeps{
		Status: s, UI: s.deps.UI, Logger: s.log,
		Users: s.users, Auth: s.users, Tokens: s.tokens, Peers: s.registry, Presence: s.hub,
		Join: s.JoinInfo(), Sessions: s.sessions,
	}
	webHandler, err := api.NewREST(restDeps)
	if err != nil {
		return err
	}
	restDeps.Local, restDeps.Sessions = true, nil
	adminHandler, err := api.NewREST(restDeps)
	if err != nil {
		return err
	}
	handler := api.Combine(grpcSrv, webHandler)
	stun, err := s.bindSTUN(ctx)
	if err != nil {
		return err
	}
	defer func() {
		for _, c := range stun {
			_ = c.Close()
		}
	}()
	httpsSrv, httpsLn, err := s.listenHTTPS(ctx, handler)
	if err != nil {
		return err
	}
	adminSrv, adminLn, err := s.listenAdmin(ctx, adminHandler)
	if err != nil {
		_ = httpsLn.Close()
		return err
	}

	hubCtx, stopHub := context.WithCancel(ctx)
	defer stopHub()
	go s.hub.RunSweeper(hubCtx, 5*time.Second)
	go s.followRegistry(hubCtx)

	errCh := make(chan error, 2)
	go func() {
		if e := httpsSrv.ServeTLS(httpsLn, "", ""); e != nil && !errors.Is(e, http.ErrServerClosed) {
			errCh <- fmt.Errorf("server: https: %w", e)
		}
	}()
	go func() {
		if e := adminSrv.Serve(adminLn); e != nil && !errors.Is(e, http.ErrServerClosed) {
			errCh <- fmt.Errorf("server: admin socket: %w", e)
		}
	}()

	s.log.Info("server ready",
		"public_addr", cfg.PublicAddr,
		"tls_fingerprint", s.tlsFingerprint,
		"hub_public_key", s.hubKey.PublicKey().String(),
		"hub_endpoint", cfg.HubEndpoint())
	s.readyOnce.Do(func() { close(s.ready) })

	for {
		select {
		case <-ctx.Done():
			stopGRPC(grpcSrv, shutdownTimeout/2)
			return s.shutdown(httpsSrv, adminSrv)
		case e := <-errCh:
			grpcSrv.Stop()
			_ = s.shutdown(httpsSrv, adminSrv)
			return e
		case <-reload:
			s.reloadPolicy()
		}
	}
}

func (s *Server) startHub(ctx context.Context) error {
	var err error
	s.device, err = s.deps.OpenDevice(ctx, wg.Options{Name: s.cfg.Overlay.Interface, Logger: s.log})
	if err != nil {
		return err
	}
	port, err := listenPort(s.cfg.Listen.WireGuard)
	if err != nil {
		return err
	}
	hubCfg := wg.Config{
		PrivateKey: s.hubKey,
		ListenPort: port,
		Addresses:  []netip.Prefix{s.cfg.HubAddr()},
	}
	if err := s.device.Configure(ctx, hubCfg); err != nil {
		_ = s.device.Close()
		return err
	}
	s.log.Info("wireguard hub ready", "backend", s.device.Backend(), "interface", s.device.Name(),
		"address", s.cfg.HubAddr().String(), "listen", s.cfg.Listen.WireGuard)
	return nil
}

func (s *Server) loadPolicy() error {
	p, err := policy.Load(s.cfg.PolicyFile)
	switch {
	case errors.Is(err, policy.ErrNotFound):
		s.log.Warn("policy file not found, using empty default-deny policy", "path", s.cfg.PolicyFile)
		p = policy.Empty()
	case err != nil:
		return err
	}
	s.policy.Store(p)
	s.log.Info("policy loaded", "path", s.cfg.PolicyFile, "rules", len(p.ACLs))
	return nil
}

// reloadPolicy re-reads the policy file; an invalid file leaves the
// running policy untouched.
func (s *Server) reloadPolicy() {
	p, err := policy.Load(s.cfg.PolicyFile)
	switch {
	case errors.Is(err, policy.ErrNotFound):
		s.log.Warn("policy reload: file not found, keeping current policy", "path", s.cfg.PolicyFile)
		return
	case err != nil:
		s.log.Error("policy reload failed, keeping current policy", "path", s.cfg.PolicyFile, "err", err)
		return
	}
	s.policy.Store(p)
	s.log.Info("policy reloaded", "path", s.cfg.PolicyFile, "rules", len(p.ACLs))
}

func (s *Server) bindSTUN(ctx context.Context) ([]net.PacketConn, error) {
	var (
		lc    net.ListenConfig
		conns []net.PacketConn
	)
	for _, addr := range s.cfg.Listen.STUN {
		c, err := lc.ListenPacket(ctx, "udp", addr)
		if err != nil {
			for _, o := range conns {
				_ = o.Close()
			}
			return nil, fmt.Errorf("server: bind stun %s: %w", addr, err)
		}
		conns = append(conns, c)
		// TODO(2026-09-02): spec 004 replaces the drain with the STUN server.
		go drain(c)
		s.log.Info("stun listener bound (responses arrive with spec 004)", "addr", c.LocalAddr().String())
	}
	return conns, nil
}

// drain reads and discards datagrams until the socket closes.
func drain(c net.PacketConn) {
	buf := make([]byte, 1500)
	for {
		if _, _, err := c.ReadFrom(buf); err != nil {
			return
		}
	}
}

func (s *Server) listenHTTPS(ctx context.Context, h http.Handler) (*http.Server, net.Listener, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", s.cfg.Listen.HTTPS)
	if err != nil {
		return nil, nil, fmt.Errorf("server: listen https %s: %w", s.cfg.Listen.HTTPS, err)
	}
	addr := ln.Addr().String()
	s.httpsAddr.Store(&addr)
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{s.tlsCert},
			NextProtos:   []string{"h2", "http/1.1"},
		},
		ErrorLog: slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
	}
	s.log.Info("https listener ready", "addr", addr)
	return srv, ln, nil
}

func (s *Server) listenAdmin(ctx context.Context, h http.Handler) (*http.Server, net.Listener, error) {
	path := s.cfg.AdminSocket
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("server: remove stale admin socket %s: %w", path, err)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "unix", path)
	if err != nil {
		return nil, nil, fmt.Errorf("server: listen admin socket %s: %w", path, err)
	}
	if err := secureSocket(path, ln); err != nil {
		_ = ln.Close()
		return nil, nil, err
	}
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
	}
	s.log.Info("admin socket ready", "path", path)
	return srv, ln, nil
}

func (s *Server) shutdown(servers ...*http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	var errs []error
	for _, srv := range servers {
		if err := srv.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("server: shutdown: %w", err))
		}
	}
	if err := os.Remove(s.cfg.AdminSocket); err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("server: remove admin socket: %w", err))
	}
	s.log.Info("server stopped")
	return errors.Join(errs...)
}

func (s *Server) closeQuietly(what string, closeFn func() error) {
	if err := closeFn(); err != nil {
		s.log.Error("close failed", "component", what, "err", err)
	}
}

// Status implements api.StatusSource.
func (s *Server) Status(ctx context.Context) (api.Status, error) {
	count, err := s.st.Peers().Count(ctx)
	if err != nil {
		return api.Status{}, err
	}
	st := api.Status{
		Version:        s.deps.Version,
		UptimeSeconds:  int64(s.deps.Now().Sub(s.startedAt).Seconds()),
		PeerCount:      count,
		TLSFingerprint: s.tlsFingerprint,
		HubPublicKey:   s.hubKey.PublicKey().String(),
	}
	if s.hub != nil {
		st.NetmapGeneration = s.hub.Generation()
		st.OnlinePeers = s.hub.OnlineCount()
	} else {
		gen, err := s.st.Meta().Generation(ctx)
		if err != nil {
			return api.Status{}, err
		}
		st.NetmapGeneration = gen
	}
	return st, nil
}

func listenPort(addr string) (int, error) {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, fmt.Errorf("server: wireguard listen %q: %w", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, fmt.Errorf("server: wireguard listen port %q: %w", portStr, err)
	}
	return port, nil
}

// buildServices wires the control-plane services on the open store.
func (s *Server) buildServices(ctx context.Context) error {
	users, err := control.NewUsers(s.st, s.deps.Now, s.log)
	if err != nil {
		return err
	}
	hub, err := control.NewHub(ctx, s.st, s.deps.Now, s.log, s.deps.HubOptions)
	if err != nil {
		return err
	}
	s.users = users
	s.hub = hub
	s.endpoints = control.NewEndpointTable(s.deps.Now)
	s.paths = control.NewPathTable(s.deps.Now)
	s.tokens = control.NewTokens(s.st, s.deps.Now, s.log)
	s.enroller = control.NewEnroller(s.st, s.deps.Now, s.log, s.cfg.OverlayPrefix(), s.cfg.MinClientVersion).WithNotifier(hub)
	s.registry = control.NewRegistry(s.st, s.log).WithNotifier(hub).WithClock(s.deps.Now)
	s.sessions = api.NewSessions(s.deps.Now)
	s.netmaps = control.NewNetMapBuilder(s.st, control.OwnerVisibility{}, s.endpoints, hub, control.HubConfig{
		PublicKey: s.hubKey.PublicKey().String(),
		Endpoint:  s.cfg.HubEndpoint(),
		Address:   s.cfg.HubAddr().Addr(),
		Overlay:   s.cfg.OverlayPrefix(),
	}, hub.Generation)
	return nil
}

// followRegistry keeps the hub WireGuard interface's peer list equal to
// the registered peers, rebuilding it on every hub wake-up.
func (s *Server) followRegistry(ctx context.Context) {
	wake, unsubscribe := s.hub.Subscribe("hub-device")
	defer unsubscribe()
	for {
		if err := s.configureHub(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("hub device update", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		}
	}
}

// configureHub applies the full hub configuration: every registered
// peer is a WireGuard peer with its /32 and no endpoint (peers initiate).
func (s *Server) configureHub(ctx context.Context) error {
	peers, err := s.st.Peers().List(ctx)
	if err != nil {
		return err
	}
	port, err := listenPort(s.cfg.Listen.WireGuard)
	if err != nil {
		return err
	}
	cfg := wg.Config{PrivateKey: s.hubKey, ListenPort: port, Addresses: []netip.Prefix{s.cfg.HubAddr()}}
	for _, p := range peers {
		key, err := wg.ParseKey(p.PublicKey)
		if err != nil {
			s.log.Warn("hub: skipping peer with bad key", "peer", p.Name)
			continue
		}
		ip, err := netip.ParseAddr(p.IPv4)
		if err != nil {
			continue
		}
		cfg.Peers = append(cfg.Peers, wg.Peer{PublicKey: key, AllowedIPs: []netip.Prefix{netip.PrefixFrom(ip, 32)}})
	}
	if err := s.device.Configure(ctx, cfg); err != nil {
		return err
	}
	s.log.Debug("hub device configured", "peers", len(cfg.Peers))
	return nil
}

// Hub exposes the presence hub (tests and status).
func (s *Server) Hub() *control.Hub { return s.hub }

// JoinInfo is the server URL and TLS fingerprint new clients need.
func (s *Server) JoinInfo() api.JoinInfo {
	return api.JoinInfo{ServerURL: s.publicURL(), Fingerprint: s.tlsFingerprint}
}

// publicURL is https://public_addr, with the HTTPS listen port appended
// when public_addr has no port and the listener is not on 443.
func (s *Server) publicURL() string {
	host, port, err := net.SplitHostPort(s.cfg.PublicAddr)
	if err != nil {
		host = s.cfg.PublicAddr
		_, port, _ = net.SplitHostPort(s.cfg.Listen.HTTPS)
	}
	if port == "" || port == "443" {
		return "https://" + host
	}
	return "https://" + net.JoinHostPort(host, port)
}

// stopGRPC drains gracefully but never waits longer than timeout: open
// Sync streams only end when their client goes away.
func stopGRPC(srv *grpc.Server, timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		srv.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		srv.Stop()
		<-done
	}
}
