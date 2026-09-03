package policy

import (
	"fmt"
	"net/netip"
	"strings"
	"testing"
)

const examplePolicy = `version: 1
groups:
  admins: [markus]
  devs: [alice, bob]
tagOwners:
  tag:prod: [group:admins]
  tag:ci: [group:admins, alice]
acls:
  - action: accept
    src: [group:devs]
    dst: ["tag:prod:22,443"]
  - action: accept
    src: [group:devs]
    dst: ["tag:ci:*"]
    proto: tcp
  - action: accept
    src: [group:admins]
    dst: ["*:*"]
  - action: accept
    src: ["*"]
    dst: ["self:*"]
`

func ip(s string) netip.Addr { return netip.MustParseAddr(s) }

var examplePeers = []Peer{
	{ID: "p1", Name: "markus-box", Owner: "markus", IPv4: ip("100.64.0.2")},
	{ID: "p2", Name: "alice-laptop", Owner: "alice", IPv4: ip("100.64.0.3")},
	{ID: "p3", Name: "alice-phone", Owner: "alice", IPv4: ip("100.64.0.4")},
	{ID: "p4", Name: "bob-box", Owner: "bob", IPv4: ip("100.64.0.5")},
	{ID: "p5", Name: "prod-1", Owner: "ops", Tags: []string{"tag:prod"}, IPv4: ip("100.64.0.6")},
	{ID: "p6", Name: "ci-1", Owner: "ops", Tags: []string{"tag:ci"}, IPv4: ip("100.64.0.7")},
}

func compileExample(t *testing.T) *Compiled {
	t.Helper()
	p, err := Parse([]byte(examplePolicy))
	if err != nil {
		t.Fatal(err)
	}
	return Compile(p, examplePeers)
}

// allows reports whether src may reach dst on proto/port.
func allows(c *Compiled, src, dst, proto string, port uint16) bool {
	for _, r := range c.Allowed(src, dst) {
		if (r.Proto == ProtoAny || r.Proto == proto) && port >= r.Lo && port <= r.Hi {
			return true
		}
	}
	return false
}

func TestPolicyParse(t *testing.T) {
	p, err := Parse([]byte(examplePolicy))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.rules) != 4 || p.Hash == "" || len(p.Hash) != 12 {
		t.Fatalf("rules=%d hash=%q", len(p.rules), p.Hash)
	}
	if p.rules[1].proto != ProtoTCP || p.rules[0].proto != ProtoAny {
		t.Errorf("protos: %+v", p.rules)
	}
	if d := p.rules[0].dst[0]; d.Host.Kind != SelTag || d.Host.Name != "prod" || len(d.Ports) != 2 || d.Ports[1].Lo != 443 {
		t.Errorf("dst: %+v", d)
	}
	cases := map[string]Selector{
		"*":             {Kind: SelAny},
		"alice":         {Kind: SelUser, Name: "alice"},
		"user:alice":    {Kind: SelUser, Name: "alice"},
		"group:devs":    {Kind: SelGroup, Name: "devs"},
		"tag:prod":      {Kind: SelTag, Name: "prod"},
		"peer:prod-1":   {Kind: SelPeer, Name: "prod-1"},
		"100.64.0.5":    {Kind: SelCIDR, Prefix: netip.MustParsePrefix("100.64.0.5/32")},
		"100.64.0.0/24": {Kind: SelCIDR, Prefix: netip.MustParsePrefix("100.64.0.0/24")},
	}
	for raw, want := range cases {
		got, err := ParseSelector(raw, false)
		if err != nil || got != want {
			t.Errorf("ParseSelector(%q) = %+v, %v; want %+v", raw, got, err, want)
		}
	}
	if _, err := ParseSelector("self", false); err == nil {
		t.Error("self accepted in src")
	}
	if s, err := ParseSelector("self", true); err != nil || s.Kind != SelSelf {
		t.Errorf("self in dst: %+v %v", s, err)
	}
	ports := map[string][]PortRange{
		"*":               {AllPorts},
		"22":              {{22, 22}},
		"22,443":          {{22, 22}, {443, 443}},
		"8000-8100":       {{8000, 8100}},
		"443,80,22-25,23": {{22, 25}, {80, 80}, {443, 443}},
	}
	for raw, want := range ports {
		got, err := ParsePorts(raw)
		if err != nil || fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("ParsePorts(%q) = %v, %v; want %v", raw, got, err, want)
		}
	}
	if _, err := Parse([]byte("")); err == nil {
		t.Error("empty document accepted")
	}
}

func TestPolicyValidateErrors(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{"missing version", "acls: []\n", "version"},
		{"deny action", "version: 1\nacls:\n  - action: deny\n    src: ['*']\n    dst: ['*:*']\n", "acls[0].action"},
		{"bad port range", "version: 1\nacls:\n  - action: accept\n    src: ['*']\n    dst: ['*:70000']\n", "acls[0].dst[0]"},
		{"reversed range", "version: 1\nacls:\n  - action: accept\n    src: ['*']\n    dst: ['*:100-50']\n", "reversed"},
		{"self in src", "version: 1\nacls:\n  - action: accept\n    src: ['self']\n    dst: ['*:*']\n", "acls[0].src[0]"},
		{"missing ports", "version: 1\nacls:\n  - action: accept\n    src: ['*']\n    dst: ['tag:prod']\n", "acls[0].dst[0]"},
		{"bad proto", "version: 1\nacls:\n  - action: accept\n    src: ['*']\n    dst: ['*:*']\n    proto: sctp\n", "acls[0].proto"},
		{"icmp with ports", "version: 1\nacls:\n  - action: accept\n    src: ['*']\n    dst: ['*:22']\n    proto: icmp\n", "icmp"},
		{"bad group member", "version: 1\ngroups:\n  devs: ['group:x']\n", "groups.devs[0]"},
		{"bad tag owner key", "version: 1\ntagOwners:\n  prod: [alice]\n", "tagOwners.prod"},
		{"unknown selector kind", "version: 1\nacls:\n  - action: accept\n    src: ['host:x']\n    dst: ['*:*']\n", "unknown selector kind"},
		{"duplicate group", "version: 1\ngroups:\n  devs: [alice]\n  devs: [bob]\n", "already defined"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.doc))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
	// Semantic validation against the registry.
	p, err := Parse([]byte(examplePolicy))
	if err != nil {
		t.Fatal(err)
	}
	warnings, err := p.Validate(Registry{Users: []string{"markus", "alice", "bob"}, Peers: []string{"prod-1"}, Tags: []string{"tag:prod"}})
	if err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "tag:ci") {
		t.Errorf("warnings: %v", warnings)
	}
	_, err = p.Validate(Registry{Users: []string{"markus", "alice"}})
	if err == nil || !strings.Contains(err.Error(), `unknown user "bob"`) || !strings.Contains(err.Error(), "groups.devs[1]") {
		t.Errorf("unknown user not reported: %v", err)
	}
	p2, _ := Parse([]byte("version: 1\nacls:\n  - action: accept\n    src: ['group:nobody']\n    dst: ['peer:x:*']\n"))
	_, err = p2.Validate(Registry{})
	if err == nil || !strings.Contains(err.Error(), `acls[0].src[0]: unknown group "nobody"`) {
		t.Errorf("unknown group not reported: %v", err)
	}
}

func TestCompileAllowed(t *testing.T) {
	c := compileExample(t)
	cases := []struct {
		src, dst, proto string
		port            uint16
		want            bool
	}{
		{"p2", "p5", "tcp", 22, true},    // alice -> prod-1:22
		{"p2", "p5", "tcp", 443, true},   // alice -> prod-1:443
		{"p2", "p5", "tcp", 5432, false}, // alice -> prod-1:5432
		{"p2", "p6", "udp", 53, false},   // alice -> ci-1:53/udp (rule is tcp)
		{"p2", "p6", "tcp", 53, true},    // alice -> ci-1:53/tcp
		{"p1", "p5", "tcp", 5432, true},  // markus -> anything
		{"p1", "p2", "udp", 1, true},
		{"p4", "p2", "tcp", 22, false},  // bob -> alice-laptop: no rule
		{"p2", "p3", "tcp", 8080, true}, // alice-laptop -> alice-phone: self
		{"p5", "p2", "tcp", 22, false},  // prod-1 (owner ops) -> alice-laptop: no rule
		{"p5", "p6", "tcp", 22, true},   // prod-1 -> ci-1: self (same owner ops)
		{"p5", "p1", "tcp", 22, false},  // prod-1 -> markus-box: no rule
	}
	for _, tc := range cases {
		if got := allows(c, tc.src, tc.dst, tc.proto, tc.port); got != tc.want {
			t.Errorf("%s -> %s %s/%d = %v, want %v (rules %+v)", tc.src, tc.dst, tc.proto, tc.port, got, tc.want, c.Allowed(tc.src, tc.dst))
		}
	}
	// Merged output: devs reach prod on two separate ranges, any proto.
	if got := c.Allowed("p2", "p5"); len(got) != 2 || got[0] != (PortRule{ProtoAny, 22, 22}) || got[1] != (PortRule{ProtoAny, 443, 443}) {
		t.Errorf("Allowed(alice, prod-1) = %+v", got)
	}
	if c.Allowed("p2", "p2") != nil || c.Allowed("nope", "p2") != nil {
		t.Error("self-pair or unknown peer allowed something")
	}
	if s := c.Summary(); s.Rules != 4 || s.Peers != 6 || s.VisiblePairs == 0 {
		t.Errorf("summary: %+v", s)
	}
}

func TestCompileVisibleSymmetric(t *testing.T) {
	c := compileExample(t)
	for _, pair := range [][2]string{{"p2", "p5"}, {"p1", "p4"}, {"p2", "p3"}} {
		if !c.Visible(pair[0], pair[1]) || !c.Visible(pair[1], pair[0]) {
			t.Errorf("%v not visible both ways", pair)
		}
	}
	// bob-box and alice-laptop have no rule in either direction.
	if c.Visible("p4", "p2") || c.Visible("p2", "p4") {
		t.Error("bob-box and alice-laptop visible")
	}
	// prod-1 can be reached by alice; visibility is symmetric even though
	// prod-1 may not initiate anything toward alice.
	if !c.Visible("p5", "p2") || c.Allowed("p5", "p2") != nil {
		t.Error("asymmetric visibility broken")
	}
}

func TestCompileSelf(t *testing.T) {
	p, _ := Parse([]byte("version: 1\nacls:\n  - action: accept\n    src: ['*']\n    dst: ['self:*']\n"))
	c := Compile(p, examplePeers)
	if !allows(c, "p2", "p3", "tcp", 1) || !allows(c, "p3", "p2", "udp", 9) {
		t.Error("same owner not allowed")
	}
	if allows(c, "p2", "p4", "tcp", 1) {
		t.Error("different owners allowed via self")
	}
	ownerless := append([]Peer{}, examplePeers...)
	ownerless = append(ownerless, Peer{ID: "x1", Name: "x1", IPv4: ip("100.64.0.9")}, Peer{ID: "x2", Name: "x2", IPv4: ip("100.64.0.10")})
	c = Compile(p, ownerless)
	if allows(c, "x1", "x2", "tcp", 1) {
		t.Error("ownerless peers matched self")
	}
}

func TestCompileCIDR(t *testing.T) {
	p, err := Parse([]byte("version: 1\nacls:\n  - action: accept\n    src: ['100.64.0.0/30']\n    dst: ['100.64.0.6:80']\n  - action: accept\n    src: ['100.64.0.5']\n    dst: ['100.64.0.0/24:443']\n"))
	if err != nil {
		t.Fatal(err)
	}
	c := Compile(p, examplePeers)
	if !allows(c, "p1", "p5", "tcp", 80) || !allows(c, "p2", "p5", "tcp", 80) {
		t.Error("/30 sources not allowed")
	}
	if allows(c, "p3", "p5", "tcp", 80) {
		t.Error("100.64.0.4 is outside /30 but allowed")
	}
	if !allows(c, "p4", "p1", "tcp", 443) || allows(c, "p4", "p1", "tcp", 80) {
		t.Error("single address source rule wrong")
	}
}

func TestFilterFor(t *testing.T) {
	c := compileExample(t)
	got := c.FilterFor("p5") // prod-1
	// devs (alice-laptop, alice-phone, bob-box) on 22 and 443; markus-box
	// (admin) on everything; ci-1 shares the owner markus -> self.
	want := map[string][]PortRule{
		"100.64.0.2": {{ProtoAny, 1, 65535}},
		"100.64.0.3": {{ProtoAny, 22, 22}, {ProtoAny, 443, 443}},
		"100.64.0.4": {{ProtoAny, 22, 22}, {ProtoAny, 443, 443}},
		"100.64.0.5": {{ProtoAny, 22, 22}, {ProtoAny, 443, 443}},
		"100.64.0.7": {{ProtoAny, 1, 65535}},
	}
	byAddr := map[string][]PortRule{}
	for _, r := range got {
		byAddr[r.Src.String()] = append(byAddr[r.Src.String()], PortRule{r.Proto, r.Lo, r.Hi})
	}
	if fmt.Sprint(byAddr) != fmt.Sprint(want) {
		t.Errorf("FilterFor(prod-1):\n got %v\nwant %v", byAddr, want)
	}
	if c.FilterFor("unknown") != nil {
		t.Error("unknown peer has filter rules")
	}
	// ci-1: devs on tcp any port, admins on anything.
	if got := c.FilterFor("p6"); len(got) < 4 {
		t.Errorf("FilterFor(ci-1) = %+v", got)
	}
}

func TestTagOwners(t *testing.T) {
	c := compileExample(t)
	cases := []struct {
		user, tag string
		want      bool
	}{
		{"markus", "tag:prod", true},
		{"alice", "tag:prod", false},
		{"alice", "tag:ci", true},
		{"bob", "tag:ci", false},
		{"markus", "tag:unknown", false},
	}
	for _, tc := range cases {
		if got := c.MayUseTag(tc.user, tc.tag); got != tc.want {
			t.Errorf("MayUseTag(%s, %s) = %v", tc.user, tc.tag, got)
		}
	}
}

func TestEmptyPolicyDenies(t *testing.T) {
	c := Compile(Empty(), examplePeers)
	for _, a := range examplePeers {
		for _, b := range examplePeers {
			if c.Visible(a.ID, b.ID) || c.Allowed(a.ID, b.ID) != nil {
				t.Fatalf("%s and %s visible under the empty policy", a.Name, b.Name)
			}
		}
		if c.FilterFor(a.ID) != nil {
			t.Fatalf("%s has filter rules under the empty policy", a.Name)
		}
	}
	if s := c.Summary(); s.VisiblePairs != 0 {
		t.Errorf("summary: %+v", s)
	}
}

func BenchmarkCompile(b *testing.B) {
	var doc strings.Builder
	doc.WriteString("version: 1\ngroups:\n")
	for g := range 10 {
		fmt.Fprintf(&doc, "  g%d: [", g)
		for u := range 10 {
			if u > 0 {
				doc.WriteString(", ")
			}
			fmt.Fprintf(&doc, "u%d", g*10+u)
		}
		doc.WriteString("]\n")
	}
	doc.WriteString("acls:\n")
	for r := range 50 {
		fmt.Fprintf(&doc, "  - action: accept\n    src: [group:g%d]\n    dst: [\"tag:t%d:%d-%d\"]\n", r%10, r%25, 1000+r, 2000+r)
	}
	p, err := Parse([]byte(doc.String()))
	if err != nil {
		b.Fatal(err)
	}
	peers := make([]Peer, 500)
	for i := range peers {
		peers[i] = Peer{ID: fmt.Sprintf("p%d", i), Name: fmt.Sprintf("peer-%d", i), Owner: fmt.Sprintf("u%d", i%100),
			Tags: []string{fmt.Sprintf("tag:t%d", i%25)}, IPv4: netip.AddrFrom4([4]byte{100, 64, byte(i / 256), byte(i % 256)})}
	}
	b.ResetTimer()
	for range b.N {
		c := Compile(p, peers)
		if c.Summary().VisiblePairs == 0 {
			b.Fatal("nothing visible")
		}
	}
}
