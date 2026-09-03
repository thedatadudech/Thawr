//go:build integration && linux

package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestServerBootsInNetns starts the real binary in a namespace with no
// route to anywhere, waits for readiness, queries the admin socket, and
// verifies a clean SIGTERM shutdown that removes the interface.
// Spec 001 acceptance: offline start, status endpoint, shutdown < 5 s.
func TestServerBootsInNetns(t *testing.T) {
	requireNetns(t)
	bin := thawrBinary(t)
	dir := shortTempDir(t)
	writeFile(t, filepath.Join(dir, "server.yaml"), serverConfig(dir))
	ns := newNetns(t, "srv")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := ns.cmd(ctx, bin, "server", "--config", filepath.Join(dir, "server.yaml"))
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	logs := waitForReady(t, stderr)

	if !strings.Contains(logs, "backend=") {
		t.Errorf("no wireguard backend logged:\n%s", logs)
	}
	if out := ns.run(t, "ip", "-o", "link", "show", "thawr0"); !strings.Contains(out, "thawr0") {
		t.Errorf("thawr0 missing inside namespace: %s", out)
	}
	if out := ns.run(t, "ip", "-o", "addr", "show", "thawr0"); !strings.Contains(out, "100.64.0.1/10") {
		t.Errorf("hub address missing: %s", out)
	}

	status := getStatus(t, filepath.Join(dir, "admin.sock"))
	if status["peer_count"] != float64(0) || !strings.HasPrefix(status["tls_fingerprint"].(string), "sha256:") {
		t.Errorf("unexpected status: %v", status)
	}

	started := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	rest, _ := io.ReadAll(stderr)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("server exited with error: %v\n%s%s", err, logs, rest)
	}
	if d := time.Since(started); d > 5*time.Second {
		t.Errorf("shutdown took %s, want < 5s", d)
	}
	if _, err := os.Stat(filepath.Join(dir, "admin.sock")); !os.IsNotExist(err) {
		t.Errorf("admin socket not removed: %v", err)
	}
	if out, err := ns.cmd(context.Background(), "ip", "link", "show", "thawr0").CombinedOutput(); err == nil {
		t.Errorf("thawr0 still present after shutdown: %s", out)
	}
	for _, secret := range []string{"PRIVATE KEY"} {
		if strings.Contains(logs+string(rest), secret) {
			t.Errorf("logs contain %q", secret)
		}
	}
}

// waitForReady consumes stderr until the "server ready" line and returns
// everything read so far.
func waitForReady(t *testing.T, r io.Reader) string {
	t.Helper()
	var sb strings.Builder
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			sb.WriteString(sc.Text() + "\n")
			if strings.Contains(sc.Text(), "server ready") {
				close(done)
				return
			}
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatalf("server not ready after 15s:\n%s", sb.String())
	}
	if !strings.Contains(sb.String(), "server ready") {
		t.Fatalf("server exited before ready:\n%s", sb.String())
	}
	return sb.String()
}

func getStatus(t *testing.T, socket string) map[string]any {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://thawr/api/v1/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("status over admin socket: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}
