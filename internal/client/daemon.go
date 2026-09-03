package client

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/netip"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	thawrv1 "github.com/thedatadudech/thawr/internal/api/proto/thawr/v1"
	"github.com/thedatadudech/thawr/internal/control"
	"github.com/thedatadudech/thawr/internal/control/path"
	"github.com/thedatadudech/thawr/internal/wg"
)

// DefaultSocket is the local control socket of the running daemon.
const DefaultSocket = "/var/run/thawr/client.sock"

// DaemonOptions configure Run. Zero values select production defaults.
type DaemonOptions struct {
	StateDir  string
	Socket    string
	Interface string
	// OpenDevice defaults to wg.Open.
	OpenDevice func(ctx context.Context, opts wg.Options) (wg.Device, error)
	Logger     *slog.Logger
	Version    string
	Now        func() time.Time
	// MinBackoff and MaxBackoff bound the reconnect delay (1 s, 60 s).
	MinBackoff time.Duration
	MaxBackoff time.Duration
	// EndpointInterval is the periodic endpoint report (60 s).
	EndpointInterval time.Duration
	// Endpoints overrides local endpoint discovery (tests).
	Endpoints func(port int, ignoreIface string) []netip.AddrPort
	// LocalPoll is how often local addresses are re-read (15 s).
	LocalPoll time.Duration
	// STUN overrides reflexive discovery (tests); STUNTimeout is per
	// attempt (2 s).
	STUN        STUNFunc
	STUNTimeout time.Duration
	// Path tunes the per-peer state machine; ProbeTick and IdleTick are
	// the prober's cadence while probing (250 ms) and otherwise (5 s).
	Path      path.Options
	ProbeTick time.Duration
	IdleTick  time.Duration
	// Trigger overrides the probe packet (tests).
	Trigger TriggerFunc
}

func (o DaemonOptions) withDefaults() DaemonOptions {
	if o.StateDir == "" {
		o.StateDir = DefaultDir()
	}
	if o.Socket == "" {
		o.Socket = DefaultSocket
	}
	if o.Interface == "" {
		o.Interface = "thawr0"
	}
	if o.OpenDevice == nil {
		o.OpenDevice = wg.Open
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Version == "" {
		o.Version = "dev"
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.MinBackoff == 0 {
		o.MinBackoff = time.Second
	}
	if o.MaxBackoff == 0 {
		o.MaxBackoff = 60 * time.Second
	}
	if o.EndpointInterval == 0 {
		o.EndpointInterval = 60 * time.Second
	}
	if o.Endpoints == nil {
		o.Endpoints = LocalEndpoints
	}
	if o.LocalPoll == 0 {
		o.LocalPoll = 15 * time.Second
	}
	if o.STUN == nil {
		o.STUN = discoverSTUN
	}
	if o.STUNTimeout == 0 {
		o.STUNTimeout = 2 * time.Second
	}
	if o.ProbeTick == 0 {
		o.ProbeTick = 250 * time.Millisecond
	}
	if o.IdleTick == 0 {
		o.IdleTick = 5 * time.Second
	}
	if o.Trigger == nil {
		o.Trigger = triggerUDP
	}
	return o
}

// Daemon keeps the WireGuard interface in sync with the server's netmap.
type Daemon struct {
	opts    DaemonOptions
	log     *slog.Logger
	state   State
	overlay netip.Prefix
	selfIP  netip.Addr

	// devMu serialises multi-step device changes (probe re-adds).
	devMu sync.Mutex

	// pmu guards the path prober's state.
	pmu           sync.Mutex
	paths         map[string]*peerPath
	selfAddrs     []netip.Addr
	selfSymmetric bool
	selfEndpoints []control.Endpoint
	pathWake      chan struct{}

	mu        sync.Mutex
	key       wg.Key
	dev       wg.Device
	netmap    *NetMap
	connected bool
	lastError string
	client    thawrv1.ControlClient
	stop      context.CancelFunc
	// mapCh delivers applied netmaps to observers (tests).
	mapCh chan NetMap
}

// NewDaemon loads the enrollment state and key. It fails with
// ErrNotEnrolled when the device has not enrolled.
func NewDaemon(opts DaemonOptions) (*Daemon, error) {
	opts = opts.withDefaults()
	st, err := LoadState(opts.StateDir)
	if err != nil {
		return nil, err
	}
	key, err := LoadKey(opts.StateDir)
	if err != nil {
		return nil, err
	}
	overlay, err := netip.ParsePrefix(st.OverlayCIDR)
	if err != nil {
		return nil, fmt.Errorf("client: overlay %q in state: %w", st.OverlayCIDR, err)
	}
	if st.ListenPort == 0 {
		st.ListenPort, err = randomPort()
		if err != nil {
			return nil, err
		}
		if err := SaveState(opts.StateDir, st); err != nil {
			return nil, err
		}
	}
	selfIP, err := netip.ParseAddr(st.IPv4)
	if err != nil {
		return nil, fmt.Errorf("client: address %q in state: %w", st.IPv4, err)
	}
	return &Daemon{opts: opts, log: opts.Logger.With("peer", st.Name), state: st, overlay: overlay.Masked(), selfIP: selfIP, key: key,
		mapCh: make(chan NetMap, 16), paths: map[string]*peerPath{}, pathWake: make(chan struct{}, 1)}, nil
}

// randomPort picks a listen port in the dynamic range.
func randomPort() (int, error) {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, fmt.Errorf("client: random port: %w", err)
	}
	return 49152 + int(binary.BigEndian.Uint16(b[:]))%(65535-49152), nil
}

// Run brings up the interface, restores the cached netmap, serves the
// local API and syncs with the server until ctx ends.
func (d *Daemon) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	d.mu.Lock()
	d.stop = cancel
	d.mu.Unlock()

	dev, err := d.opts.OpenDevice(ctx, wg.Options{Name: d.opts.Interface, Logger: d.log})
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.dev = dev
	d.mu.Unlock()
	defer func() {
		d.closeSinks()
		if err := dev.Close(); err != nil {
			d.log.Error("close device", "err", err)
		}
	}()

	if nm, ok, err := LoadNetMap(d.opts.StateDir); err != nil {
		d.log.Warn("netmap cache unreadable, ignoring", "err", err)
	} else if ok {
		if err := d.apply(ctx, nm, false); err != nil {
			d.log.Warn("cached netmap not applied", "err", err)
		} else {
			d.log.Info("restored cached netmap", "generation", nm.Generation, "peers", len(nm.Peers))
		}
	} else {
		base := wg.Config{PrivateKey: d.key, ListenPort: d.state.ListenPort,
			Addresses: []netip.Prefix{netip.PrefixFrom(netip.MustParseAddr(d.state.IPv4), d.overlay.Bits())}}
		if err := dev.Configure(ctx, base); err != nil {
			return err
		}
	}

	srv, ln, err := d.listenLocal(ctx)
	if err != nil {
		return err
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			d.log.Error("local api", "err", err)
		}
	}()
	d.log.Info("client ready", "interface", dev.Name(), "backend", dev.Backend(), "ipv4", d.state.IPv4, "listen_port", d.state.ListenPort, "socket", d.opts.Socket)

	pathsDone := make(chan struct{})
	go func() {
		defer close(pathsDone)
		d.pathLoop(ctx)
	}()
	d.syncLoop(ctx)
	<-pathsDone

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelShutdown()
	_ = srv.Shutdown(shutdownCtx)
	_ = os.Remove(d.opts.Socket)
	d.log.Info("client stopped")
	return nil
}

// Stop ends Run.
func (d *Daemon) Stop() {
	d.mu.Lock()
	stop := d.stop
	d.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// Applied returns a channel receiving every netmap applied to the device.
func (d *Daemon) Applied() <-chan NetMap { return d.mapCh }

func (d *Daemon) syncLoop(ctx context.Context) {
	attempt := 0
	for {
		err := d.syncOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		attempt++
		delay := backoffDelay(attempt, d.opts.MinBackoff, d.opts.MaxBackoff)
		switch status.Code(err) {
		case codes.PermissionDenied, codes.Unauthenticated:
			// Removed from the network: keep the daemon (status shows why)
			// but do not hammer the server.
			delay = d.opts.MaxBackoff
			d.setError("removed from the network or credentials rejected; run `thawr client down --forget` and enrol again")
		default:
			d.setError(err.Error())
		}
		d.log.Warn("sync disconnected", "err", err, "retry_in", delay, "attempt", attempt)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if status.Code(err) != codes.PermissionDenied && status.Code(err) != codes.Unauthenticated && attempt > 10 {
			attempt = 10 // cap growth; delay already at max
		}
	}
}

// syncOnce dials, streams netmaps until the stream ends, and returns why.
func (d *Daemon) syncOnce(ctx context.Context) error {
	tlsCfg, err := PinnedTLSConfig(d.state.Fingerprint)
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(d.state.Server,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithPerRPCCredentials(nodeCredentials{secret: d.state.NodeSecret}))
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = conn.Close() }()
	client := thawrv1.NewControlClient(conn)

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := client.Sync(streamCtx, &thawrv1.SyncRequest{Generation: d.generation(), ClientVersion: d.opts.Version})
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	first, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	d.mu.Lock()
	d.client = client
	d.connected = true
	d.lastError = ""
	d.mu.Unlock()
	d.log.Info("sync connected", "server", d.state.Server, "generation", first.GetGeneration())
	defer func() {
		d.mu.Lock()
		d.connected = false
		d.client = nil
		d.mu.Unlock()
	}()
	if err := d.apply(ctx, NetMapFromProto(first, d.opts.Now()), true); err != nil {
		d.log.Error("apply netmap", "err", err)
	}

	go d.reportLoop(streamCtx, client)

	for {
		msg, err := stream.Recv()
		if err != nil {
			return fmt.Errorf("sync: %w", err)
		}
		if msg.GetKeepalive() {
			continue
		}
		if err := d.apply(ctx, NetMapFromProto(msg, d.opts.Now()), true); err != nil {
			d.log.Error("apply netmap", "err", err)
		}
	}
}

// apply configures the device from nm and caches it.
func (d *Daemon) apply(ctx context.Context, nm NetMap, cache bool) error {
	d.mu.Lock()
	key, dev := d.key, d.dev
	d.mu.Unlock()
	cfg, err := BuildConfig(nm, key, d.state.ListenPort, d.overlay)
	if err != nil {
		return err
	}
	d.devMu.Lock()
	err = dev.Configure(ctx, cfg)
	d.devMu.Unlock()
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.netmap = &nm
	d.mu.Unlock()
	if err := d.syncPaths(ctx, nm, cfg); err != nil {
		d.log.Warn("path state", "err", err)
	}
	if cache {
		if err := SaveNetMap(d.opts.StateDir, nm); err != nil {
			d.log.Warn("cache netmap", "err", err)
		}
	}
	d.log.Debug("netmap applied", "generation", nm.Generation, "peers", len(nm.Peers))
	select {
	case d.mapCh <- nm:
	default:
	}
	return nil
}

func (d *Daemon) generation() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.netmap == nil {
		return 0
	}
	return d.netmap.Generation
}

func (d *Daemon) setError(msg string) {
	d.mu.Lock()
	d.lastError = msg
	d.mu.Unlock()
}

// RotateKey generates a new WireGuard key, registers it with the server
// and switches the device to it.
func (d *Daemon) RotateKey(ctx context.Context) error {
	d.mu.Lock()
	client := d.client
	d.mu.Unlock()
	if client == nil {
		return errors.New("client: not connected to the server")
	}
	newKey, err := wg.GenerateKey()
	if err != nil {
		return err
	}
	if _, err := client.RotateKey(ctx, &thawrv1.RotateKeyRequest{NewPublicKey: newKey.PublicKey().String()}); err != nil {
		return fmt.Errorf("client: rotate key: %w", err)
	}
	if err := SaveKey(d.opts.StateDir, newKey); err != nil {
		return err
	}
	d.mu.Lock()
	d.key = newKey
	nm := d.netmap
	d.mu.Unlock()
	if nm != nil {
		if err := d.apply(ctx, *nm, false); err != nil {
			return err
		}
	}
	d.log.Info("key rotated", "key", wg.Fingerprint(newKey.PublicKey()))
	return nil
}

// backoffDelay is exponential from min with ±20 % jitter, capped at max.
func backoffDelay(attempt int, minDelay, maxDelay time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := float64(minDelay) * math.Pow(2, float64(attempt-1))
	if d > float64(maxDelay) {
		d = float64(maxDelay)
	}
	var b [1]byte
	_, _ = rand.Read(b[:])
	jitter := (float64(b[0])/255 - 0.5) * 0.4 // -0.2 .. +0.2
	d *= 1 + jitter
	if d < float64(minDelay) {
		d = float64(minDelay)
	}
	if d > float64(maxDelay) {
		d = float64(maxDelay)
	}
	return time.Duration(d)
}

// nodeCredentials sends the node secret as a bearer token per RPC.
type nodeCredentials struct{ secret string }

func (c nodeCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + c.secret}, nil
}

func (nodeCredentials) RequireTransportSecurity() bool { return true }

func (d *Daemon) listenLocal(ctx context.Context) (*http.Server, net.Listener, error) {
	if err := os.MkdirAll(dirOf(d.opts.Socket), 0o755); err != nil { //nolint:gosec // socket dir is not secret; the socket itself is 0660
		return nil, nil, fmt.Errorf("client: socket dir: %w", err)
	}
	if err := os.Remove(d.opts.Socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("client: remove stale socket: %w", err)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "unix", d.opts.Socket)
	if err != nil {
		return nil, nil, fmt.Errorf("client: listen %s: %w", d.opts.Socket, err)
	}
	_ = os.Chmod(d.opts.Socket, 0o660) //nolint:gosec // group access is intended
	srv := &http.Server{Handler: d.localHandler(), ReadHeaderTimeout: 5 * time.Second}
	return srv, ln, nil
}
