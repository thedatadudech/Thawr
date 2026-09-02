# Spec 006 — ACL policy

Sprint 2. Depends on: 003. Packages: `internal/control/policy` (parse,
validate, compile), `internal/control` (visibility, netmap filter
rules), `internal/wg` (nftables filter, userspace filter), `cmd/thawr`
(`admin policy check|reload|show`).

## Goal

Who may reach what is declared in one YAML file the user keeps in git,
evaluated to default deny, and enforced both by key distribution and by
a packet filter on the receiving peer.

## User story

As an admin, I write that developers may reach port 22 and 443 on
production servers and nothing else, commit the file, run
`thawr admin policy reload`, and from that moment a developer's laptop
cannot open port 5432 on a production box, and cannot see peers it has
no rule for at all.

## Policy file

```yaml
version: 1

groups:
  admins: [markus]
  devs: [alice, bob]

tagOwners:                 # who may create tokens carrying a tag
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
    dst: ["self:*"]        # every peer may reach its owner's other peers
```

Selectors (`src` and the host part of `dst`): `*`, `user:name` or bare
`name`, `group:name`, `tag:name`, `peer:name`, `self` (dst only: peers
with the same owner as src), IPv4 address or CIDR within the overlay.
Ports: `*`, `22`, `22,443`, `8000-8100`, combinable. `proto`: `tcp`,
`udp`, `icmp`, `any` (default). `action` must be `accept`; there is no
`deny` because default deny plus ordering-independent accept rules is
simpler to reason about.

Validation errors name the rule index and field. Unknown users, groups,
tags and peers are errors, not warnings, except that `tag:` and `peer:`
names not yet existing are allowed with a warning (so policy can be
written before peers enrol).

## Compilation

`policy.Compile(p Policy, peers []Peer, users []User) (*Compiled, error)`
produces:

- `Allowed(src, dst PeerID) []PortRule` — union of all matching rules,
  each `{proto, lo, hi}`.
- `Visible(a, b PeerID) bool` — `Allowed(a,b)` or `Allowed(b,a)` is
  non-empty. Symmetric by construction.
- `FilterFor(dst PeerID) []FilterRule` — for every src that may reach
  dst: `{src_ipv4, proto, lo, hi}`. This is what the netmap carries.
- ICMP echo is implicitly allowed between visible peers (diagnostics,
  and spec 004's probing needs it).

Compilation is pure and cached per (policy hash, netmap generation).

`tagOwners` is enforced at token creation: a token may carry `tag:x`
only if the creator matches a `tagOwners` entry for it (admins always
may).

## Enforcement

1. Key distribution (spec 003) uses `Visible`. This replaces the
   temporary same-owner rule.
2. Receiver-side filter, installed by the client from `netmap.filter`:
   - Linux, kernel WireGuard: nftables table `inet thawr`, chain
     `input` hooked on `iif thawr0` with policy `drop`; rules
     `ct state established,related accept`, `icmp type echo-request
     ip saddr {visible} accept`, one `ip saddr X <proto> dport {ports}
     accept` per filter rule. Atomic replace on every netmap.
   - `wireguard-go` (Linux fallback, macOS, Windows): a userspace filter
     between the device and the TUN: parses IPv4 header and TCP/UDP
     ports, keeps a flow table (5-tuple, 120 s idle timeout for UDP,
     TCP tracked by SYN/FIN/RST with 1 h idle) so replies to accepted
     outbound flows pass. Anything not matched is dropped and counted.
3. Hub-side: the server installs `FilterFor(static peer)` on its own
   forwarding path for every static peer, using the same two mechanisms.

`thawr admin policy check <file>` validates and prints a matrix summary
(`N rules, M peers, K visible pairs`). `thawr admin policy reload`
re-reads `policy_file`, rejects invalid files while keeping the running
policy, bumps the generation on success. `thawr admin policy show` prints
the effective policy and its hash. The admin UI has a read-only policy
page with the same output and a "check" textarea.

## Acceptance criteria

- [ ] The example above compiles; `alice → prod-1:22` allowed,
      `alice → prod-1:5432` denied, `alice → ci-1:53/udp` denied (proto
      tcp), `markus → anything` allowed, `bob → alice-laptop` denied
      (different owner, no rule), `alice-laptop → alice-phone` allowed
      (`self`).
- [ ] An empty or missing policy makes every peer invisible to every
      other peer except the hub (default deny), verified in the netmap.
- [ ] A policy change is reflected in all clients' netmaps and filters
      within 5 s of `reload`.
- [ ] An invalid file on `reload` leaves the previous policy active and
      returns the validation errors; on startup it is fatal.
- [ ] nftables filter: with kernel WireGuard in netns, `nc` to a denied
      port times out and to an allowed port connects; replies to an
      allowed outbound TCP connection pass; the ruleset is replaced
      atomically (no window with `accept all` or with rules missing).
- [ ] Userspace filter: same behaviour with `wireguard-go`; UDP reply
      within 120 s passes, after that it is dropped.
- [ ] A member cannot create a token with a tag they do not own.
- [ ] Filter drop counters appear in `thawr client status --json`.
- [ ] Policy evaluation for 500 peers and 50 rules compiles in under
      50 ms (benchmark in CI, not a hard gate).

## Test cases

- `TestPolicyParse` (valid file, every selector form, every port form).
- `TestPolicyValidateErrors` (table: unknown group, bad port range,
  `deny` action, missing version, duplicate group).
- `TestCompileAllowed`, `TestCompileVisibleSymmetric`, `TestCompileSelf`,
  `TestCompileCIDR`, `TestFilterFor`.
- `TestTagOwners`.
- `TestReloadKeepsOldOnError`.
- `TestUserspaceFilter` (packet fixtures: allowed TCP SYN, denied SYN,
  reply to accepted flow, UDP timeout with injected clock, non-IPv4).
- `TestNftablesRuleset` (Linux, integration tag: install, list, compare
  with golden ruleset).
- `BenchmarkCompile`.
- Integration: `TestPolicyEnforcedEndToEnd` for both adapters.

## Out of scope

- `deny` rules, rule ordering semantics.
- Time-based or posture-based rules.
- SSH-level (user@host) rules, application-layer policy.
- Policy stored in the DB or edited in the UI (file is the source of
  truth by design).
- IPv6 rules (phase 2 with the IPv6 overlay).
- Egress filtering on the sender (receiver-side only; the sender's
  `AllowedIPs` already limits destinations to visible peers).
