//go:build integration && linux

package tests

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// requireNetns skips the test unless it can create network namespaces:
// Linux, root, and the iproute2 `ip` binary on PATH.
func requireNetns(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("network namespaces need Linux")
	}
	if os.Geteuid() != 0 {
		t.Skip("network namespaces need root (CAP_NET_ADMIN)")
	}
	if _, err := exec.LookPath("ip"); err != nil {
		t.Skip("iproute2 `ip` binary not found")
	}
}

// netns is a throwaway network namespace with loopback up.
type netns struct {
	name string
}

// newNetns creates a namespace named after the test and deletes it on
// cleanup.
func newNetns(t *testing.T, suffix string) *netns {
	t.Helper()
	name := "thawr-" + strings.ToLower(strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())) + "-" + suffix
	if len(name) > 15 {
		name = name[len(name)-15:]
	}
	run(t, "ip", "netns", "add", name)
	ns := &netns{name: name}
	t.Cleanup(func() { _ = exec.Command("ip", "netns", "del", name).Run() })
	ns.run(t, "ip", "link", "set", "lo", "up")
	return ns
}

// cmd builds a command that executes inside the namespace.
func (n *netns) cmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	full := append([]string{"netns", "exec", n.name, name}, args...)
	return exec.CommandContext(ctx, "ip", full...)
}

func (n *netns) run(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := n.cmd(context.Background(), name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("in %s: %s %v: %v\n%s", n.name, name, args, err, out)
	}
	return string(out)
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// thawrBinary returns the path of the built binary, building it if
// needed so `go test -tags integration ./tests/...` is self-contained.
func thawrBinary(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin", "thawr")
	build := exec.Command("go", "build", "-o", bin, "./cmd/thawr")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build thawr: %v\n%s", err, out)
	}
	return bin
}

// shortTempDir keeps Unix socket paths under the kernel's length limit.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "thawr-it")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func serverConfig(dir string) string {
	return fmt.Sprintf(`public_addr: 127.0.0.1
data_dir: %s/data
listen:
  https: "127.0.0.1:8443"
  stun: ["127.0.0.1:3478", "127.0.0.1:3479"]
  wireguard: "127.0.0.1:51820"
admin_socket: %s/admin.sock
policy_file: %s/policy.yaml
`, dir, dir, dir)
}
