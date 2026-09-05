package dns

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
)

// resolverDir is where macOS reads per-domain resolver settings.
const resolverDir = "/etc/resolver"

// resolverFile registers the zone through /etc/resolver/<zone>, which
// macOS's mDNSResponder picks up on its own.
type resolverFile struct {
	opts RegistrarOptions
}

func (r *resolverFile) path() string {
	return filepath.Join(r.opts.Root, resolverDir, r.opts.Zone)
}

func (r *resolverFile) Register(ctx context.Context, _ string, server netip.Addr) (string, error) {
	dir := filepath.Dir(r.path())
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // mDNSResponder reads /etc/resolver as an unprivileged user
		return MethodResolverFile, fmt.Errorf("dns: create %s: %w", dir, err)
	}
	content := fmt.Sprintf("nameserver %s\nport 53\n", server)
	if err := os.WriteFile(r.path(), []byte(content), 0o644); err != nil { //nolint:gosec // resolver files are world-readable by design
		return MethodResolverFile, fmt.Errorf("dns: write %s: %w", r.path(), err)
	}
	r.flush(ctx)
	return MethodResolverFile, nil
}

func (r *resolverFile) Update(context.Context, []Entry) error { return nil }

func (r *resolverFile) Unregister(ctx context.Context, _ string) error {
	if err := os.Remove(r.path()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("dns: remove %s: %w", r.path(), err)
	}
	r.flush(ctx)
	return nil
}

// flush drops the system resolver cache; failures are not errors.
func (r *resolverFile) flush(ctx context.Context) {
	if _, err := r.opts.Runner(ctx, "dscacheutil", "-flushcache"); err != nil {
		r.opts.Logger.Debug("dns: flush resolver cache", "err", err)
	}
}
