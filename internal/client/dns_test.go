package client

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thedatadudech/thawr/internal/dns"
)

// fakeRegistrar records the registrar calls in order.
type fakeRegistrar struct {
	mu      sync.Mutex
	calls   []string
	entries []dns.Entry
	fail    error
}

func (f *fakeRegistrar) Register(_ context.Context, iface string, server netip.Addr) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "register "+iface+" "+server.String())
	if f.fail != nil {
		return dns.MethodHosts, f.fail
	}
	return dns.MethodHosts, nil
}

func (f *fakeRegistrar) Update(_ context.Context, entries []dns.Entry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "update")
	f.entries = entries
	return nil
}

func (f *fakeRegistrar) Unregister(_ context.Context, iface string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "unregister "+iface)
	return nil
}

func (f *fakeRegistrar) snapshot() ([]string, []dns.Entry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...), append([]dns.Entry(nil), f.entries...)
}

// loopbackDNS binds the resolver on 127.0.0.1 instead of the overlay
// address the fake device never carries, and remembers where.
type loopbackDNS struct {
	mu   sync.Mutex
	addr string
}

func (l *loopbackDNS) listen(ctx context.Context, _ netip.AddrPort) (net.PacketConn, net.Listener, error) {
	udp, tcp, err := dns.Listen(ctx, netip.MustParseAddrPort("127.0.0.1:0"))
	if err != nil {
		return nil, nil, err
	}
	l.mu.Lock()
	l.addr = udp.LocalAddr().String()
	l.mu.Unlock()
	return udp, tcp, nil
}

func (l *loopbackDNS) resolver() *net.Resolver {
	l.mu.Lock()
	addr := l.addr
	l.mu.Unlock()
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "udp", addr)
	}}
}

// TestDaemonServesNames enrols two peers and resolves self, hub, the
// other peer and a reverse name through the running daemon; the
// registrar sees unregister, register, then an update per netmap, and
// a final unregister on shutdown.
func TestDaemonServesNames(t *testing.T) {
	cp := newControlPlane(t)
	dirA, dirB := t.TempDir(), t.TempDir()
	stA := cp.enrol(dirA, "a")
	reg := &fakeRegistrar{}
	lb := &loopbackDNS{}
	d, _, stopDaemon := startDaemon(t, dirA, func(o *DaemonOptions) {
		o.DNS = DNSOptions{Mode: DNSOn, Registrar: reg, Listen: lb.listen}
	})
	stop := sync.OnceFunc(stopDaemon)
	t.Cleanup(stop)
	waitApplied(t, d, func(nm NetMap) bool { return nm.Hub.PublicKey != "" })
	stB := cp.enrol(dirB, "b")
	waitApplied(t, d, func(nm NetMap) bool { return len(nm.Peers) == 1 })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := lb.resolver()
	lookup := func(name string) string {
		ips, err := r.LookupNetIP(ctx, "ip4", name)
		if err != nil {
			t.Fatalf("lookup %s: %v", name, err)
		}
		return ips[0].String()
	}
	if got := lookup("a.thawr"); got != stA.IPv4 {
		t.Errorf("self: %s, want %s", got, stA.IPv4)
	}
	if got := lookup("b.thawr"); got != stB.IPv4 {
		t.Errorf("peer: %s, want %s", got, stB.IPv4)
	}
	if got := lookup("hub.thawr"); got != "100.64.0.1" {
		t.Errorf("hub: %s", got)
	}
	if names, err := r.LookupAddr(ctx, stB.IPv4); err != nil || len(names) != 1 || names[0] != "b.thawr." {
		t.Errorf("reverse: %v %v", names, err)
	}
	if _, err := r.LookupNetIP(ctx, "ip4", "nobody.thawr"); err == nil || !strings.Contains(err.Error(), "no such host") {
		t.Errorf("unknown name: %v", err)
	}

	lc := NewLocalClient(d.opts.Socket)
	st, err := lc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.DNS == nil || st.DNS.State != DNSServing || st.DNS.Method != dns.MethodHosts || st.DNS.Names != 3 || st.DNS.Listen != stA.IPv4+":53" {
		t.Errorf("dns status: %+v", st.DNS)
	}
	calls, entries := reg.snapshot()
	if len(calls) < 3 || calls[0] != "unregister thawr0" || calls[1] != "register thawr0 "+stA.IPv4 || calls[2] != "update" {
		t.Errorf("registrar calls: %v", calls)
	}
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "a,b,hub" {
		t.Errorf("entries: %v", names)
	}
	// Ping accepts the zone suffix.
	if _, err := d.Ping(ctx, "nobody.thawr"); !errors.Is(err, ErrUnknownPeer) || !strings.HasSuffix(err.Error(), ": nobody") {
		t.Errorf("ping with suffix: %v", err)
	}

	stop()
	calls, _ = reg.snapshot()
	if calls[len(calls)-1] != "unregister thawr0" {
		t.Errorf("no unregister on shutdown: %v", calls)
	}
}

// TestDaemonDNSOffAndBindFailure: off means no status object; a bind
// failure is reported, not fatal.
func TestDaemonDNSOffAndBindFailure(t *testing.T) {
	cp := newControlPlane(t)
	dir := t.TempDir()
	cp.enrol(dir, "a")
	d, _, stop := startDaemon(t, dir, func(o *DaemonOptions) { o.DNS = DNSOptions{Mode: DNSOff} })
	waitApplied(t, d, func(nm NetMap) bool { return nm.Hub.PublicKey != "" })
	if st := d.Status(context.Background()); st.DNS != nil {
		stop()
		t.Fatalf("dns status with --dns off: %+v", st.DNS)
	}
	stop()

	reg := &fakeRegistrar{}
	d, _, stop = startDaemon(t, dir, func(o *DaemonOptions) {
		o.DNS = DNSOptions{Mode: DNSOn, Registrar: reg, Listen: func(context.Context, netip.AddrPort) (net.PacketConn, net.Listener, error) {
			return nil, nil, errors.New("address in use")
		}}
	})
	defer stop()
	// The cached netmap is applied first; wait for the live connection.
	var st Status
	for deadline := time.Now().Add(5 * time.Second); ; {
		st = d.Status(context.Background())
		if st.Server.State == ServerConnected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon not connected after a dns bind failure: %+v", st.Server)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st.DNS == nil || st.DNS.State != DNSError || st.DNS.Error != "address in use" || st.DNS.Method != dns.MethodHosts {
		t.Errorf("dns status after bind failure: %+v", st.DNS)
	}
}

func TestStripZone(t *testing.T) {
	for in, want := range map[string]string{"nas": "nas", "nas.thawr": "nas", "NAS.thawr.": "nas", "nas.thawr.thawr": "nas.thawr", " nas ": "nas"} {
		if got := stripZone(in); got != want {
			t.Errorf("stripZone(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewDaemonRejectsBadDNS(t *testing.T) {
	cp := newControlPlane(t)
	dir := t.TempDir()
	cp.enrol(dir, "a")
	if _, err := NewDaemon(DaemonOptions{StateDir: dir, DNS: DNSOptions{Mode: "maybe"}}); err == nil || !strings.Contains(err.Error(), "dns mode") {
		t.Errorf("mode: %v", err)
	}
	if _, err := NewDaemon(DaemonOptions{StateDir: dir, DNS: DNSOptions{Mode: DNSServe, Port: 70000}}); err == nil || !strings.Contains(err.Error(), "port") {
		t.Errorf("port: %v", err)
	}
}
