package dns

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner records every command line and fails the ones listed.
type fakeRunner struct {
	calls []string
	fail  map[string]error
}

func (f *fakeRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	line := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, line)
	if err, ok := f.fail[line]; ok {
		return nil, err
	}
	return nil, nil
}

func entries() []Entry {
	return []Entry{
		{Name: "nas", Addr: netip.MustParseAddr("100.64.0.3")},
		{Name: "hub", Addr: netip.MustParseAddr("100.64.0.1")},
		{Name: "alice-laptop", Addr: netip.MustParseAddr("100.64.0.7")},
		{Name: "", Addr: netip.MustParseAddr("100.64.0.9")}, // skipped
	}
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("THAWR_UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set THAWR_UPDATE_GOLDEN=1 to create it)", err)
	}
	if w := strings.ReplaceAll(string(want), "\r\n", "\n"); got != w {
		t.Errorf("%s differs from golden:\n%s", name, got)
	}
}

func TestHostsBlockRender(t *testing.T) {
	checkGolden(t, "hosts.golden", renderHostsBlock("thawr", entries()))
	if renderHostsBlock("thawr", nil) != "" {
		t.Error("empty entries render a block")
	}
}

func TestHostsBlockInsertUpdateRemove(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "etc", "hosts")
	original := "127.0.0.1 localhost\n::1 localhost\n\n# my printer\n192.168.1.9 printer"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newHostsFile(RegistrarOptions{Root: root, Zone: "thawr"})
	ctx := context.Background()
	if method, err := h.Register(ctx, "thawr0", netip.MustParseAddr("100.64.0.7")); err != nil || method != MethodHosts {
		t.Fatalf("register: %s %v", method, err)
	}
	if err := h.Update(ctx, entries()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := original + "\n" + renderHostsBlock("thawr", entries())
	if string(got) != want {
		t.Fatalf("after insert:\n%s", got)
	}
	// Update replaces the block in place and touches nothing else.
	if err := h.Update(ctx, entries()[:1]); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != original+"\n"+renderHostsBlock("thawr", entries()[:1]) {
		t.Fatalf("after update:\n%s", got)
	}
	if strings.Count(string(got), hostsBegin) != 1 {
		t.Fatal("duplicate block")
	}
	// A block in the middle of the file is replaced where it is.
	middle := "127.0.0.1 localhost\n" + renderHostsBlock("thawr", entries()[:1]) + "192.168.1.9 printer\n"
	if err := os.WriteFile(path, []byte(middle), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.Update(ctx, entries()); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != "127.0.0.1 localhost\n"+renderHostsBlock("thawr", entries())+"192.168.1.9 printer\n" {
		t.Fatalf("after middle update:\n%s", got)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode changed to %v", info.Mode().Perm())
	}
	if err := h.Unregister(ctx, "thawr0"); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != "127.0.0.1 localhost\n192.168.1.9 printer\n" {
		t.Fatalf("after remove:\n%s", got)
	}
	// Removing when no block exists is a no-op, even without the file.
	if err := h.Unregister(ctx, "thawr0"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := h.Unregister(ctx, "thawr0"); err != nil {
		t.Fatalf("unregister without file: %v", err)
	}
	if err := h.Update(ctx, entries()); err == nil {
		t.Fatal("update without a hosts file succeeded")
	}
}

func TestResolvedCommands(t *testing.T) {
	root := t.TempDir()
	fr := &fakeRunner{}
	opts := RegistrarOptions{Root: root, Zone: "thawr", Runner: fr.run, LookPath: func(string) (string, error) { return "/usr/bin/resolvectl", nil }}
	l := newLinuxRegistrar(opts.withDefaults())
	ctx := context.Background()
	// Without systemd-resolved the hosts file is used.
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc", "hosts"), []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	method, err := l.Register(ctx, "thawr0", netip.MustParseAddr("100.64.0.7"))
	if err != nil || method != MethodHosts {
		t.Fatalf("without resolved: %s %v", method, err)
	}
	if len(fr.calls) != 0 {
		t.Fatalf("commands run without resolved: %v", fr.calls)
	}
	if err := l.Unregister(ctx, "thawr0"); err != nil {
		t.Fatal(err)
	}
	// With it, resolvectl routes the zone.
	if err := os.MkdirAll(filepath.Join(root, resolvedDir), 0o755); err != nil {
		t.Fatal(err)
	}
	method, err = l.Register(ctx, "thawr0", netip.MustParseAddr("100.64.0.7"))
	if err != nil || method != MethodResolved {
		t.Fatalf("with resolved: %s %v", method, err)
	}
	want := []string{
		"resolvectl dns thawr0 100.64.0.7",
		"resolvectl domain thawr0 ~thawr",
		"resolvectl default-route thawr0 false",
	}
	if strings.Join(fr.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s", strings.Join(fr.calls, "\n"))
	}
	if err := l.Update(ctx, entries()); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "etc", "hosts")); strings.Contains(string(data), hostsBegin) {
		t.Fatal("hosts file written in resolved mode")
	}
	fr.calls = nil
	if err := l.Unregister(ctx, "thawr0"); err != nil {
		t.Fatal(err)
	}
	if len(fr.calls) != 1 || fr.calls[0] != "resolvectl revert thawr0" {
		t.Fatalf("unregister calls %v", fr.calls)
	}
	// A failing resolvectl is reported with the step.
	fr.fail = map[string]error{"resolvectl domain thawr0 ~thawr": errors.New("boom")}
	if _, err := l.Register(ctx, "thawr0", netip.MustParseAddr("100.64.0.7")); err == nil || !strings.Contains(err.Error(), "resolvectl domain") {
		t.Fatalf("error %v", err)
	}
	// resolvectl missing from PATH means hosts mode even with the directory.
	opts.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	l = newLinuxRegistrar(opts.withDefaults())
	if method, _ := l.Register(ctx, "thawr0", netip.MustParseAddr("100.64.0.7")); method != MethodHosts {
		t.Fatalf("method %s without resolvectl", method)
	}
}

func TestResolverFile(t *testing.T) {
	root := t.TempDir()
	fr := &fakeRunner{}
	r := &resolverFile{opts: RegistrarOptions{Root: root, Zone: "thawr", Runner: fr.run}.withDefaults()}
	ctx := context.Background()
	method, err := r.Register(ctx, "utun4", netip.MustParseAddr("100.64.0.2"))
	if err != nil || method != MethodResolverFile {
		t.Fatalf("%s %v", method, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "etc", "resolver", "thawr"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "nameserver 100.64.0.2\nport 53\n" {
		t.Fatalf("content %q", data)
	}
	if len(fr.calls) != 1 || fr.calls[0] != "dscacheutil -flushcache" {
		t.Fatalf("calls %v", fr.calls)
	}
	if err := r.Unregister(ctx, "utun4"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "etc", "resolver", "thawr")); !os.IsNotExist(err) {
		t.Fatal("resolver file kept")
	}
	if err := r.Unregister(ctx, "utun4"); err != nil {
		t.Fatalf("second unregister: %v", err)
	}
}

func TestNRPTCommands(t *testing.T) {
	fr := &fakeRunner{}
	n := &nrpt{opts: RegistrarOptions{Zone: "thawr", Runner: fr.run}.withDefaults()}
	ctx := context.Background()
	method, err := n.Register(ctx, "thawr", netip.MustParseAddr("100.64.0.2"))
	if err != nil || method != MethodNRPT {
		t.Fatalf("%s %v", method, err)
	}
	if err := n.Unregister(ctx, "thawr"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"powershell -NoProfile -NonInteractive -Command Add-DnsClientNrptRule -Namespace '.thawr' -NameServers '100.64.0.2' -Comment 'thawr'",
		"powershell -NoProfile -NonInteractive -Command Get-DnsClientNrptRule | Where-Object { $_.Comment -eq 'thawr' } | Remove-DnsClientNrptRule -Force",
	}
	if strings.Join(fr.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s", strings.Join(fr.calls, "\n"))
	}
}

func TestUnsupportedRegistrar(t *testing.T) {
	var u unsupported
	method, err := u.Register(context.Background(), "x", netip.MustParseAddr("100.64.0.2"))
	if !errors.Is(err, ErrUnsupported) || method != MethodNone {
		t.Fatalf("%s %v", method, err)
	}
	if err := u.Unregister(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
}
