package client

import (
	"context"
	"net/netip"
	"testing"

	"github.com/thedatadudech/thawr/internal/control"
)

func TestNATType(t *testing.T) {
	local := []netip.Addr{netip.MustParseAddr("192.0.2.10")}
	lan := control.Endpoint{Addr: netip.MustParseAddrPort("192.0.2.10:41820"), Kind: control.EndpointLocal}
	refl := control.Endpoint{Addr: netip.MustParseAddrPort("203.0.113.5:4444"), Kind: control.EndpointReflexive}
	same := control.Endpoint{Addr: netip.MustParseAddrPort("192.0.2.10:41820"), Kind: control.EndpointReflexive}
	cases := []struct {
		name      string
		eps       []control.Endpoint
		symmetric bool
		want      string
		reflexive int
	}{
		{"no stun", []control.Endpoint{lan}, false, NATUnknown, 0},
		{"cone", []control.Endpoint{lan, refl}, false, NATCone, 1},
		{"public address", []control.Endpoint{lan, same}, false, NATNone, 1},
		{"symmetric", []control.Endpoint{lan, refl}, true, NATSymmetric, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := natStatus(local, tc.eps, tc.symmetric)
			if got.Type != tc.want || len(got.Reflexive) != tc.reflexive || len(got.Local) != 1 {
				t.Errorf("got %+v, want type %s with %d reflexive", got, tc.want, tc.reflexive)
			}
		})
	}
}

// TestStatusServerStates covers connected → cached when the server goes
// away, and reconnecting when a daemon starts without a netmap.
func TestStatusServerStates(t *testing.T) {
	cp := newControlPlane(t)
	dirA, dirB := t.TempDir(), t.TempDir()
	cp.enrol(dirA, "a")
	cp.enrol(dirB, "b")
	d, _, stop := startDaemon(t, dirA)
	defer stop()
	waitApplied(t, d, func(nm NetMap) bool { return nm.Hub.PublicKey != "" })
	lc := NewLocalClient(d.opts.Socket)
	st, err := lc.Status(context.Background())
	if err != nil || st.Server.State != ServerConnected || st.Server.Attempt != 0 || st.Server.NextRetryAt != nil || st.Server.LastMessageAt == nil {
		t.Fatalf("connected status: %+v err=%v", st.Server, err)
	}
	if st.Hub == nil || st.Hub.Kind != "server" || st.Hub.Path != "unreachable" {
		t.Errorf("hub without handshake: %+v", st.Hub)
	}

	cp.ts.CloseClientConnections()
	cp.ts.Close()
	waitFor(t, "cached state", func() bool {
		st, err := lc.Status(context.Background())
		return err == nil && st.Server.State == ServerCached && st.Server.UnreachableSince != nil && st.Server.Attempt >= 1
	})
	st, _ = lc.Status(context.Background())
	if st.Server.Generation == 0 || st.Server.LastError == "" || st.Server.NextRetryAt == nil {
		t.Errorf("cached status: %+v", st.Server)
	}

	// A fresh daemon with no cache and no server is reconnecting.
	d2, _, stop2 := startDaemon(t, dirB)
	defer stop2()
	lc2 := NewLocalClient(d2.opts.Socket)
	waitFor(t, "reconnecting state", func() bool {
		st, err := lc2.Status(context.Background())
		return err == nil && st.Server.State == ServerReconnecting && st.Server.Attempt >= 1 && st.Server.UnreachableSince != nil && st.Server.Generation == 0
	})
}
