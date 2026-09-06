//go:build integration && linux

package tests

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestKeyPinningHoldsRotatedPeer: bob rotates his key; alice's client
// holds bob out of the tunnel and names him in status until
// `client trust bob-box` runs there, after which the path comes back.
// The server's audit log carries the rotation. Spec 011 acceptance.
func TestKeyPinningHoldsRotatedPeer(t *testing.T) {
	m := newStarMesh(t, "version: 1\nacls:\n  - action: accept\n    src: ['*']\n    dst: ['*:*']\n", false)
	ctx := context.Background()
	aliceSock := filepath.Join(m.dir, "alice-box.sock")
	bobSock := filepath.Join(m.dir, "bob-box.sock")
	waitPeers := func(cond func(clientStatus) bool, what string) clientStatus {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for {
			st := m.status(0)
			if cond(st) {
				return st
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: %+v", what, st)
			}
			time.Sleep(300 * time.Millisecond)
		}
	}
	waitPeers(func(st clientStatus) bool { return len(st.Peers) == 1 && st.Peers[0].Name == "bob-box" && len(st.Held) == 0 }, "alice never saw bob")
	if state, _, err := pingPathOnce(ctx, m.clients[0], m.bin, aliceSock, "bob-box"); err != nil || state != "direct" {
		t.Fatalf("path before rotation: %s %v", state, err)
	}

	if out, err := m.clients[1].cmd(ctx, m.bin, "client", "rotate-key", "--socket", bobSock).CombinedOutput(); err != nil || !strings.Contains(string(out), "thawr client trust bob-box") {
		t.Fatalf("rotate-key: %v\n%s", err, out)
	}
	held := waitPeers(func(st clientStatus) bool { return len(st.Held) == 1 && st.Held[0].Name == "bob-box" }, "alice did not hold bob after the rotation")
	if held.Peers[0].Path != "key_changed" || held.Held[0].PinnedKey == held.Held[0].OfferedKey {
		t.Errorf("held status: %+v", held)
	}
	out, err := m.clients[0].cmd(ctx, m.bin, "client", "status", "--socket", aliceSock).CombinedOutput()
	if !strings.Contains(string(out), "1 key changed: thawr client trust bob-box") || !strings.Contains(string(out), "key changed") {
		t.Errorf("status text: %v\n%s", err, out)
	}
	if _, _, err := pingPathOnce(ctx, m.clients[0], m.bin, aliceSock, "bob-box"); err == nil {
		t.Error("ping of a held peer succeeded")
	}
	if out, err := m.clients[0].cmd(ctx, m.bin, "client", "trust", "nobody", "--socket", aliceSock).CombinedOutput(); err == nil {
		t.Errorf("trusting an unknown name succeeded:\n%s", out)
	}

	if out, err := m.clients[0].cmd(ctx, m.bin, "client", "trust", "bob-box", "--socket", aliceSock).CombinedOutput(); err != nil || !strings.Contains(string(out), "trusted bob-box") {
		t.Fatalf("trust: %v\n%s", err, out)
	}
	waitPeers(func(st clientStatus) bool { return len(st.Held) == 0 && len(st.Peers) == 1 && st.Peers[0].Path != "key_changed" }, "bob not released after trust")
	deadline := time.Now().Add(20 * time.Second)
	for {
		state, _, err := pingPathOnce(ctx, m.clients[0], m.bin, aliceSock, "bob-box")
		if err == nil && state == "direct" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("path after trust: %s %v", state, err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	raw, err := m.admin("audit", "--action", "peer.rotate_key", "--json")
	if err != nil {
		t.Fatalf("admin audit: %v\n%s", err, raw)
	}
	var entries []struct {
		Actor   string            `json:"actor"`
		Details map[string]string `json:"details"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil || len(entries) != 1 || entries[0].Actor != "peer:bob-box" || len(entries[0].Details["key"]) != 8 {
		t.Errorf("audit rows: %v %s", err, raw)
	}
}
