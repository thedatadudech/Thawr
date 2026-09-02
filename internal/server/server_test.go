package server

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thedatadudech/thawr/internal/api"
	"github.com/thedatadudech/thawr/internal/config"
	"github.com/thedatadudech/thawr/internal/wg"
	"github.com/thedatadudech/thawr/internal/wg/wgtest"
)

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// shortTempDir avoids the Unix socket path limit that t.TempDir can hit.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "thawr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func testConfig(t *testing.T) (*config.Config, string) {
	t.Helper()
	dir := shortTempDir(t)
	cfg := config.Default()
	cfg.PublicAddr = "127.0.0.1"
	cfg.DataDir = filepath.Join(dir, "data")
	cfg.Listen.HTTPS = "127.0.0.1:0"
	cfg.Listen.STUN = []string{"127.0.0.1:0"}
	cfg.Listen.WireGuard = "127.0.0.1:0"
	cfg.AdminSocket = filepath.Join(dir, "admin.sock")
	cfg.PolicyFile = filepath.Join(dir, "policy.yaml")
	cfg.Log.Level = "debug"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("test config invalid: %v", err)
	}
	return cfg, dir
}

type harness struct {
	srv    *Server
	fake   *wgtest.Fake
	logs   *syncBuffer
	reload chan struct{}
	cancel context.CancelFunc
	done   chan error
}

func newHarness(t *testing.T, cfg *config.Config) *harness {
	t.Helper()
	logs := &syncBuffer{}
	fake := wgtest.New(cfg.Overlay.Interface)
	srv, err := New(cfg, Deps{
		OpenDevice: func(context.Context, wg.Options) (wg.Device, error) { return fake, nil },
		Logger:     NewLogger(cfg.Log, logs),
		Version:    "test",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &harness{srv: srv, fake: fake, logs: logs, reload: make(chan struct{}, 1)}
}

// start runs the server and waits for readiness.
func (h *harness) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.done = make(chan error, 1)
	go func() { h.done <- h.srv.Run(ctx, h.reload) }()
	select {
	case <-h.srv.Ready():
	case err := <-h.done:
		t.Fatalf("server exited before ready: %v\nlogs:\n%s", err, h.logs.String())
	case <-time.After(10 * time.Second):
		t.Fatalf("server not ready after 10s\nlogs:\n%s", h.logs.String())
	}
}

// runExpectingError runs the server and returns the startup error.
func (h *harness) runExpectingError(t *testing.T) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := h.srv.Run(ctx, h.reload)
	if err == nil {
		t.Fatalf("expected startup error\nlogs:\n%s", h.logs.String())
	}
	return err
}

func (h *harness) stop(t *testing.T) {
	t.Helper()
	started := time.Now()
	h.cancel()
	select {
	case err := <-h.done:
		if err != nil {
			t.Fatalf("Run returned error: %v\nlogs:\n%s", err, h.logs.String())
		}
	case <-time.After(shutdownTimeout + time.Second):
		t.Fatalf("shutdown exceeded %s", shutdownTimeout)
	}
	if d := time.Since(started); d > shutdownTimeout {
		t.Errorf("shutdown took %s", d)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return fi.Mode().Perm()
}

func TestBootstrapCreatesFiles(t *testing.T) {
	cfg, _ := testConfig(t)
	h := newHarness(t, cfg)
	h.start(t)

	if _, err := os.Stat(cfg.AdminSocket); err != nil {
		t.Errorf("admin socket missing while running: %v", err)
	}
	last, ok := h.fake.Last()
	if !ok {
		t.Fatal("hub device never configured")
	}
	if want := netip.MustParsePrefix("100.64.0.1/10"); len(last.Addresses) != 1 || last.Addresses[0] != want {
		t.Errorf("hub addresses %v, want [%s]", last.Addresses, want)
	}
	if len(last.Peers) != 0 {
		t.Errorf("hub should start without peers, got %d", len(last.Peers))
	}
	h.stop(t)

	for _, f := range []struct {
		path string
		mode os.FileMode
	}{
		{filepath.Join(cfg.DataDir, DBFile), 0},
		{filepath.Join(cfg.DataDir, ServerKeyFile), 0o600},
		{filepath.Join(cfg.DataDir, TLSDir, TLSCertFile), 0},
		{filepath.Join(cfg.DataDir, TLSDir, TLSKeyFile), 0o600},
	} {
		got := fileMode(t, f.path)
		if f.mode != 0 && runtime.GOOS != "windows" && got != f.mode {
			t.Errorf("%s mode %o, want %o", f.path, got, f.mode)
		}
	}
	if runtime.GOOS != "windows" {
		if got := fileMode(t, cfg.DataDir); got != 0o700 {
			t.Errorf("data_dir mode %o, want 700", got)
		}
	}
	if _, err := os.Stat(cfg.AdminSocket); !os.IsNotExist(err) {
		t.Errorf("admin socket not removed on shutdown: %v", err)
	}
	if !h.fake.Closed() {
		t.Error("wireguard device not closed on shutdown")
	}
	logs := h.logs.String()
	for _, want := range []string{"server ready", "generated=true", "policy file not found"} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs missing %q:\n%s", want, logs)
		}
	}
}

func TestBootstrapReusesFiles(t *testing.T) {
	cfg, _ := testConfig(t)
	h1 := newHarness(t, cfg)
	h1.start(t)
	h1.stop(t)
	keyPath := filepath.Join(cfg.DataDir, ServerKeyFile)
	key1, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	cert1, _ := os.ReadFile(filepath.Join(cfg.DataDir, TLSDir, TLSCertFile))

	h2 := newHarness(t, cfg)
	h2.start(t)
	h2.stop(t)
	if strings.Contains(h2.logs.String(), "generated=true") {
		t.Errorf("second start regenerated something:\n%s", h2.logs.String())
	}
	key2, _ := os.ReadFile(keyPath)
	cert2, _ := os.ReadFile(filepath.Join(cfg.DataDir, TLSDir, TLSCertFile))
	if !bytes.Equal(key1, key2) || !bytes.Equal(cert1, cert2) {
		t.Error("key or certificate changed across restarts")
	}
}

func TestKeyMismatchRefused(t *testing.T) {
	cfg, _ := testConfig(t)
	h1 := newHarness(t, cfg)
	h1.start(t)
	h1.stop(t)

	other, err := wg.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(cfg.DataDir, ServerKeyFile)
	if err := os.WriteFile(keyPath, []byte(other.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h2 := newHarness(t, cfg)
	err = h2.runExpectingError(t)
	if !strings.Contains(err.Error(), "does not match the database") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestKeyFileModeChecked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix modes on Windows")
	}
	cfg, _ := testConfig(t)
	h1 := newHarness(t, cfg)
	h1.start(t)
	h1.stop(t)
	keyPath := filepath.Join(cfg.DataDir, ServerKeyFile)
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	h2 := newHarness(t, cfg)
	if err := h2.runExpectingError(t); !strings.Contains(err.Error(), "must be mode 0600") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSelfSignedCert(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.PublicAddr = "vpn.example.test:8443"
	h := newHarness(t, cfg)
	h.start(t)
	h.stop(t)

	raw, err := os.ReadFile(filepath.Join(cfg.DataDir, TLSDir, TLSCertFile))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatal("cert.pem is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cert.PublicKey.(*ecdsa.PublicKey); !ok {
		t.Errorf("public key type %T, want ECDSA", cert.PublicKey)
	}
	if len(cert.DNSNames) != 1 || cert.DNSNames[0] != "vpn.example.test" {
		t.Errorf("SAN %v, want [vpn.example.test]", cert.DNSNames)
	}
	if got := time.Until(cert.NotAfter); got < selfSignedValidity-time.Hour || got > selfSignedValidity+time.Hour {
		t.Errorf("validity %s, want about %s", got, selfSignedValidity)
	}
	if !strings.HasPrefix(h.srv.tlsFingerprint, "sha256:") || len(h.srv.tlsFingerprint) != len("sha256:")+64 {
		t.Errorf("fingerprint %q", h.srv.tlsFingerprint)
	}
}

func TestSelfSignedCertIPHost(t *testing.T) {
	certPEM, _, err := generateSelfSigned("203.0.113.9", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(cert.IPAddresses) != 1 || cert.IPAddresses[0].String() != "203.0.113.9" {
		t.Errorf("IP SAN %v", cert.IPAddresses)
	}
}

func TestCheckDoesNotTouchDataDir(t *testing.T) {
	cfg, dir := testConfig(t)
	h := newHarness(t, cfg)
	if err := h.srv.Check(); err != nil {
		t.Errorf("Check with absent policy: %v", err)
	}
	if _, err := os.Stat(cfg.DataDir); !os.IsNotExist(err) {
		t.Errorf("Check created data_dir: %v", err)
	}
	if err := os.WriteFile(cfg.PolicyFile, []byte("version: 7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.srv.Check(); err == nil {
		t.Error("Check accepted an invalid policy")
	}
	cfg.TLS.Mode = config.TLSModeFile
	cfg.TLS.CertFile = filepath.Join(dir, "no.pem")
	cfg.TLS.KeyFile = filepath.Join(dir, "no.key")
	_ = os.Remove(cfg.PolicyFile)
	h2 := newHarness(t, cfg)
	if err := h2.srv.Check(); err == nil {
		t.Error("Check accepted missing TLS files")
	}
}

// getStatus fetches /api/v1/status over the admin socket and returns the
// status code and body.
func getStatus(t *testing.T, socket string) (int, []byte) {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://thawr/api/v1/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET status: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read status body: %v", err)
	}
	return resp.StatusCode, body
}

func TestStatusOverAdminSocket(t *testing.T) {
	cfg, _ := testConfig(t)
	h := newHarness(t, cfg)
	h.start(t)
	defer h.stop(t)

	code, body := getStatus(t, cfg.AdminSocket)
	if code != http.StatusOK {
		t.Fatalf("status code %d", code)
	}
	var st api.Status
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatal(err)
	}
	if st.Version != "test" || st.PeerCount != 0 || st.NetmapGeneration != 0 {
		t.Errorf("unexpected status %+v", st)
	}
	if st.TLSFingerprint != h.srv.tlsFingerprint || st.HubPublicKey != h.srv.hubKey.PublicKey().String() {
		t.Errorf("fingerprint/key mismatch: %+v", st)
	}
	if st.UptimeSeconds < 0 {
		t.Errorf("uptime %d", st.UptimeSeconds)
	}
}

func TestHTTPSListener(t *testing.T) {
	cfg, _ := testConfig(t)
	h := newHarness(t, cfg)
	h.start(t)
	defer h.stop(t)

	addr := h.srv.HTTPSAddr()
	if addr == "" {
		t.Fatal("no https address")
	}
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(context.Background(), "tcp", addr)
	if err != nil {
		t.Fatalf("dial https: %v", err)
	}
	_ = conn.Close()
}

func TestPolicyReload(t *testing.T) {
	cfg, _ := testConfig(t)
	write := func(doc string) {
		if err := os.WriteFile(cfg.PolicyFile, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("version: 1\nacls:\n  - action: accept\n    src: ['*']\n    dst: ['*:*']\n")
	h := newHarness(t, cfg)
	h.start(t)
	defer h.stop(t)
	if got := len(h.srv.Policy().ACLs); got != 1 {
		t.Fatalf("initial rules %d, want 1", got)
	}

	write("version: 1\nacls:\n  - action: deny\n")
	h.reload <- struct{}{}
	waitFor(t, func() bool { return strings.Contains(h.logs.String(), "policy reload failed") })
	if got := len(h.srv.Policy().ACLs); got != 1 {
		t.Errorf("invalid reload replaced policy: rules %d", got)
	}

	write("version: 1\nacls:\n  - action: accept\n    src: ['*']\n    dst: ['*:*']\n  - action: accept\n    src: ['*']\n    dst: ['self:*']\n")
	h.reload <- struct{}{}
	waitFor(t, func() bool { return len(h.srv.Policy().ACLs) == 2 })
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within 5s")
}

func TestInvalidPolicyFatalAtStartup(t *testing.T) {
	cfg, _ := testConfig(t)
	if err := os.WriteFile(cfg.PolicyFile, []byte("version: 1\nacls: [{action: deny}]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, cfg)
	if err := h.runExpectingError(t); !strings.Contains(err.Error(), "policy") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNoSecretsInLogs(t *testing.T) {
	cfg, _ := testConfig(t)
	h := newHarness(t, cfg)
	h.start(t)
	_, _ = getStatus(t, cfg.AdminSocket)
	h.stop(t)

	logs := h.logs.String()
	private := h.srv.hubKey.String()
	if strings.Contains(logs, private) {
		t.Error("wireguard private key appears in logs")
	}
	keyPEM, err := os.ReadFile(filepath.Join(cfg.DataDir, TLSDir, TLSKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(keyPEM), "\n") {
		if line != "" && !strings.HasPrefix(line, "-----") && strings.Contains(logs, line) {
			t.Error("tls private key material appears in logs")
		}
	}
	if strings.Contains(logs, "PRIVATE KEY") {
		t.Error("PEM private key header appears in logs")
	}
}

func TestOpenDeviceErrorAborts(t *testing.T) {
	cfg, _ := testConfig(t)
	logs := &syncBuffer{}
	srv, err := New(cfg, Deps{
		OpenDevice: func(context.Context, wg.Options) (wg.Device, error) {
			return nil, errors.New("no tun")
		},
		Logger: NewLogger(cfg.Log, logs),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Run(ctx, nil); err == nil || !strings.Contains(err.Error(), "no tun") {
		t.Errorf("got %v, want device error", err)
	}
}

func TestRefusesWritableDataDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix modes on Windows")
	}
	cfg, _ := testConfig(t)
	if err := os.MkdirAll(cfg.DataDir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cfg.DataDir, 0o777); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, cfg)
	if err := h.runExpectingError(t); !strings.Contains(err.Error(), "world-writable") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	cfg := config.Default()
	if _, err := New(cfg, Deps{}); err == nil {
		t.Error("expected validation error for missing public_addr")
	}
	if _, err := New(nil, Deps{}); err == nil {
		t.Error("expected error for nil config")
	}
}
