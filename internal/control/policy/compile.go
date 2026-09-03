package policy

import (
	"net/netip"
	"sort"
)

// Peer is what compilation needs to know about a registered peer.
type Peer struct {
	ID    string
	Name  string
	Owner string // user name; empty for ownerless peers
	Tags  []string
	IPv4  netip.Addr
}

// PortRule is one allowed (proto, port range) between two peers.
type PortRule struct {
	Proto string
	Lo    uint16
	Hi    uint16
}

// FilterRule allows Src to reach the destination on a port range; it
// is what the netmap carries to the receiving peer.
type FilterRule struct {
	Src   netip.Addr
	Proto string
	Lo    uint16
	Hi    uint16
}

// Summary describes a compiled policy in one line.
type Summary struct {
	Rules        int `json:"rules"`
	Peers        int `json:"peers"`
	VisiblePairs int `json:"visible_pairs"`
}

// Compiled is a policy evaluated against a peer list. It is immutable
// and safe for concurrent use.
type Compiled struct {
	Hash  string
	peers []Peer
	index map[string]int
	rules []crule
	// tagOwners maps tag:name to the set of user names that may use it.
	tagOwners map[string]map[string]bool
	summary   Summary
}

// crule is one (rule, dst entry) pair with resolved bitsets.
type crule struct {
	src   bitset
	dst   bitset
	self  bool
	proto string
	ports []PortRange
}

type bitset []uint64

func newBitset(n int) bitset { return make(bitset, (n+63)/64) }

func (b bitset) set(i int)      { b[i/64] |= 1 << (uint(i) % 64) }
func (b bitset) has(i int) bool { return b[i/64]&(1<<(uint(i)%64)) != 0 }

// Compile resolves every selector against peers. Unknown names resolve
// to nothing (Validate reports them separately).
func Compile(p *Policy, peers []Peer) *Compiled {
	c := &Compiled{Hash: p.Hash, peers: append([]Peer(nil), peers...), index: make(map[string]int, len(peers)), tagOwners: map[string]map[string]bool{}}
	for i, pe := range c.peers {
		c.index[pe.ID] = i
	}
	groups := make(map[string]map[string]bool, len(p.Groups))
	for name, members := range p.Groups {
		groups[name] = set(members)
	}
	resolve := func(sel Selector) bitset {
		b := newBitset(len(peers))
		for i, pe := range peers {
			switch sel.Kind {
			case SelAny:
				b.set(i)
			case SelUser:
				if pe.Owner != "" && pe.Owner == sel.Name {
					b.set(i)
				}
			case SelGroup:
				if pe.Owner != "" && groups[sel.Name][pe.Owner] {
					b.set(i)
				}
			case SelTag:
				for _, t := range pe.Tags {
					if t == "tag:"+sel.Name {
						b.set(i)
					}
				}
			case SelPeer:
				if pe.Name == sel.Name {
					b.set(i)
				}
			case SelCIDR:
				if pe.IPv4.IsValid() && sel.Prefix.Contains(pe.IPv4) {
					b.set(i)
				}
			case SelSelf:
			}
		}
		return b
	}
	union := func(sels []Selector) bitset {
		b := newBitset(len(peers))
		for _, s := range sels {
			for i, w := range resolve(s) {
				b[i] |= w
			}
		}
		return b
	}
	for _, r := range p.rules {
		src := union(r.src)
		for _, d := range r.dst {
			cr := crule{src: src, proto: r.proto, ports: d.Ports}
			if d.Host.Kind == SelSelf {
				cr.self = true
			} else {
				cr.dst = resolve(d.Host)
			}
			c.rules = append(c.rules, cr)
		}
	}
	for tag, owners := range p.TagOwners {
		allowed := map[string]bool{}
		for _, o := range owners {
			sel, err := ParseSelector(o, false)
			if err != nil {
				continue
			}
			switch sel.Kind {
			case SelUser:
				allowed[sel.Name] = true
			case SelGroup:
				for u := range groups[sel.Name] {
					allowed[u] = true
				}
			default:
			}
		}
		c.tagOwners[tag] = allowed
	}
	c.summary = Summary{Rules: len(p.ACLs), Peers: len(peers)}
	for i := range peers {
		for j := i + 1; j < len(peers); j++ {
			if c.visibleIdx(i, j) {
				c.summary.VisiblePairs++
			}
		}
	}
	return c
}

// matches reports whether rule r lets src reach dst (indices).
func (c *Compiled) matches(r *crule, src, dst int) bool {
	if !r.src.has(src) {
		return false
	}
	if r.self {
		a, b := c.peers[src], c.peers[dst]
		return a.Owner != "" && a.Owner == b.Owner && src != dst
	}
	return r.dst.has(dst)
}

// Allowed returns the union of port rules letting src reach dst, merged
// per protocol; nil when nothing is allowed.
func (c *Compiled) Allowed(src, dst string) []PortRule {
	i, ok := c.index[src]
	j, ok2 := c.index[dst]
	if !ok || !ok2 || i == j {
		return nil
	}
	return c.allowedIdx(i, j)
}

func (c *Compiled) allowedIdx(i, j int) []PortRule {
	byProto := map[string][]PortRange{}
	for r := range c.rules {
		if c.matches(&c.rules[r], i, j) {
			byProto[c.rules[r].proto] = append(byProto[c.rules[r].proto], c.rules[r].ports...)
		}
	}
	var out []PortRule
	for _, proto := range []string{ProtoAny, ProtoTCP, ProtoUDP, ProtoICMP} {
		for _, pr := range mergeRanges(byProto[proto]) {
			out = append(out, PortRule{Proto: proto, Lo: pr.Lo, Hi: pr.Hi})
		}
	}
	return out
}

func (c *Compiled) anyIdx(i, j int) bool {
	for r := range c.rules {
		if c.matches(&c.rules[r], i, j) {
			return true
		}
	}
	return false
}

func (c *Compiled) visibleIdx(i, j int) bool { return c.anyIdx(i, j) || c.anyIdx(j, i) }

// Visible reports whether a and b may exchange keys: at least one
// direction allows something. Symmetric by construction.
func (c *Compiled) Visible(a, b string) bool {
	i, ok := c.index[a]
	j, ok2 := c.index[b]
	if !ok || !ok2 || i == j {
		return false
	}
	return c.visibleIdx(i, j)
}

// FilterFor lists, for every peer that may reach dst, the allowed
// protocol and port ranges by source address. ICMP echo between visible
// peers is implicit and not listed.
func (c *Compiled) FilterFor(dst string) []FilterRule {
	j, ok := c.index[dst]
	if !ok {
		return nil
	}
	var out []FilterRule
	for i, src := range c.peers {
		if i == j || !src.IPv4.IsValid() {
			continue
		}
		for _, pr := range c.allowedIdx(i, j) {
			out = append(out, FilterRule{Src: src.IPv4, Proto: pr.Proto, Lo: pr.Lo, Hi: pr.Hi})
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Src.Less(out[b].Src) })
	return out
}

// MayUseTag reports whether user may create tokens carrying tag
// ("tag:name"). Tags with no tagOwners entry belong to nobody.
func (c *Compiled) MayUseTag(user, tag string) bool {
	return c.tagOwners[tag][user]
}

// Summary reports rule, peer and visible pair counts.
func (c *Compiled) Summary() Summary { return c.summary }

// Peers returns the peers the policy was compiled against.
func (c *Compiled) Peers() []Peer { return c.peers }
