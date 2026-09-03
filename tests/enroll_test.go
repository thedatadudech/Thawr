//go:build integration && linux

package tests

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestEnrollTwoClients boots a server in one namespace and enrols two
// clients from their own namespaces over a veth "internet"; both must
// appear with distinct addresses. Spec 002 integration test.
func TestEnrollTwoClients(t *testing.T) {
	requireNetns(t)
	bin := thawrBinary(t)
	dir := shortTempDir(t)

	srvNs := newNetns(t, "srv")
	c1Ns := newNetns(t, "c1")
	c2Ns := newNetns(t, "c2")
	// Server 10.9.0.1, clients 10.9.0.2 and 10.9.0.3 on a bridge-less
	// star: one veth pair per client into the server namespace.
	for i, ns := range []*netns{c1Ns, c2Ns} {
		veth := "v" + string(rune('a'+i))
		ip(t, "link", "add", veth+"s", "type", "veth", "peer", "name", veth+"c")
		ip(t, "link", "set", veth+"s", "netns", srvNs.name)
		ip(t, "link", "set", veth+"c", "netns", ns.name)
		srvNs.ip(t, "addr", "add", "10.9."+string(rune('0'+i))+".1/24", "dev", veth+"s")
		srvNs.ip(t, "link", "set", veth+"s", "up")
		ns.ip(t, "addr", "add", "10.9."+string(rune('0'+i))+".2/24", "dev", veth+"c")
		ns.ip(t, "link", "set", veth+"c", "up")
	}

	writeFile(t, filepath.Join(dir, "server.yaml"), strings.NewReplacer("127.0.0.1", "0.0.0.0").Replace(serverConfig(dir)))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	srv := srvNs.cmd(ctx, bin, "server", "--config", filepath.Join(dir, "server.yaml"))
	stderr, err := srv.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Process.Signal(syscall.SIGTERM); _ = srv.Wait() }()
	logs := waitForReady(t, stderr)
	fingerprint := fingerprintFromLogs(t, logs)
	socket := filepath.Join(dir, "admin.sock")

	admin := func(args ...string) string {
		cmd := srvNs.cmd(ctx, bin, append([]string{"admin", "--socket", socket}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("admin %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	writeFile(t, filepath.Join(dir, "pw"), "integrationpassword\n")
	cmd := srvNs.cmd(ctx, bin, "admin", "--socket", socket, "user", "create", "alice", "--role", "member")
	cmd.Env = append(os.Environ(), "THAWR_PASSWORD_FILE="+filepath.Join(dir, "pw"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("user create: %v\n%s", err, out)
	}

	for i, ns := range []*netns{c1Ns, c2Ns} {
		var tok struct {
			Secret string `json:"secret"`
		}
		if err := json.Unmarshal([]byte(admin("token", "create", "--owner", "alice", "--json")), &tok); err != nil {
			t.Fatal(err)
		}
		server := "https://10.9." + string(rune('0'+i)) + ".1:8443"
		out, err := ns.cmd(ctx, bin, "client", "up", "--server", server, "--token", tok.Secret, "--fingerprint", fingerprint,
			"--state-dir", filepath.Join(dir, "client"+string(rune('1'+i))), "--name", "client-"+string(rune('1'+i))).CombinedOutput()
		if err != nil {
			t.Fatalf("client %d up: %v\n%s", i+1, err, out)
		}
		if !strings.Contains(string(out), "enrolled as client-"+string(rune('1'+i))) {
			t.Errorf("client %d output: %s", i+1, out)
		}
	}

	var peers []struct {
		Name string `json:"name"`
		IPv4 string `json:"ipv4"`
	}
	if err := json.Unmarshal([]byte(admin("peer", "list", "--json")), &peers); err != nil {
		t.Fatal(err)
	}
	if len(peers) != 2 || peers[0].IPv4 == peers[1].IPv4 || peers[0].IPv4 == "" {
		t.Errorf("peers: %+v", peers)
	}
}

// fingerprintFromLogs extracts tls_fingerprint=sha256:... from the
// "server ready" line.
func fingerprintFromLogs(t *testing.T, logs string) string {
	t.Helper()
	for _, line := range strings.Split(logs, "\n") {
		if !strings.Contains(line, "server ready") {
			continue
		}
		for _, field := range strings.Fields(line) {
			if v, ok := strings.CutPrefix(field, "tls_fingerprint="); ok {
				return strings.Trim(v, `"`)
			}
		}
	}
	t.Fatal("no tls_fingerprint in server logs")
	return ""
}
