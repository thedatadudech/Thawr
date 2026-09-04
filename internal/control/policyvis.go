package control

import (
	"net/netip"

	"github.com/thedatadudech/thawr/internal/control/policy"
	"github.com/thedatadudech/thawr/internal/store"
)

// PolicyVisibility answers visibility and filter questions from the
// compiled policy. Load returns the current compilation (the server
// recompiles when the policy or the registry changes); a nil result
// means nothing is visible.
type PolicyVisibility struct {
	Load func() *policy.Compiled
}

// Visible implements Visibility.
func (v PolicyVisibility) Visible(a, b store.Peer) bool {
	c := v.Load()
	return c != nil && c.Visible(a.ID, b.ID)
}

// FilterFor implements Visibility.
func (v PolicyVisibility) FilterFor(dst store.Peer) []FilterRule {
	c := v.Load()
	if c == nil {
		return nil
	}
	return filterRules(c.FilterFor(dst.ID))
}

// filterRules converts compiled rules to netmap rules.
func filterRules(rules []policy.FilterRule) []FilterRule {
	out := make([]FilterRule, 0, len(rules))
	for _, r := range rules {
		out = append(out, FilterRule{SrcIPv4: r.Src, Proto: r.Proto, PortLo: r.Lo, PortHi: r.Hi})
	}
	return out
}

// PolicyPeers converts registered peers into what the compiler needs;
// names resolves owner IDs to user names.
func PolicyPeers(peers []store.Peer, names map[string]string) []policy.Peer {
	out := make([]policy.Peer, 0, len(peers))
	for _, p := range peers {
		pp := policy.Peer{ID: p.ID, Name: p.Name, Owner: names[p.OwnerID], Tags: p.Tags}
		if a, err := netip.ParseAddr(p.IPv4); err == nil {
			pp.IPv4 = a
		}
		out = append(out, pp)
	}
	return out
}
