# Spec 010 — DNS names

Sprint 3. Depends on: 003 (netmap sync), 006 (policy visibility), 007
(status document), 008 (mobile peers via the hub). Packages: new
`internal/dns` (resolver, forwarder, split-DNS registrars),
`internal/client` (serve and register), `internal/server` and
`internal/api` (hub resolver, mobile config), `internal/config`
(`dns` key), `cmd/thawr` (`--dns`, status line).

## Goal

Every peer is reachable by name: `ssh nas.thawr`, `curl
http://build-box.thawr:8000`, `ping alice-laptop.thawr`, on laptops,
servers and phones alike, without editing hosts files by hand and
without any DNS server outside the network. The names come from the
netmap the client already has, so they work exactly when the tunnel
works, including with the internet cable pulled.

## User story

As a user I enrol a new machine and my other machines can reach it as
`<name>.thawr` within seconds, in the browser, in ssh and in scripts.
On my phone the same names work because the WireGuard app asks the hub.
When a peer is removed from the network its name stops resolving.

## Commands

```
thawr client up   ... [--dns on|serve|off]      # default on
thawr client install ... [--dns on|serve|off]
thawr client status                             # DNS state in the header
thawr client ping nas.thawr                     # the suffix is accepted
```

Server config (`server.yaml`):

```yaml
dns:
  enabled: true      # hub resolver on <hub ip>:53
  upstream: []       # forwarders for everything outside .thawr;
                     # default: the nameservers in /etc/resolv.conf
```

`client status` header:

```
thawr 0.1.0 · alice-laptop 100.64.0.7 · server vpn.example.com:8443 connected (netmap #42, 3s ago)
WireGuard: kernel · thawr0 · listen 41820 · NAT: cone (reflexive 203.0.113.9:41820) · DNS: .thawr via resolved
```

## Behaviour

### The zone

- The zone is `thawr.` and is not configurable in v1.
- `<name>.thawr` has one A record: the peer's overlay IPv4. `<self
  name>.thawr` and `hub.thawr` (the server's hub address) are included.
  Peer names are already unique DNS labels (specs 002 and 008), so no
  name collides.
- Reverse: `<w>.<z>.<y>.<x>.in-addr.arpa` for an overlay address answers
  PTR `<name>.thawr.`.
- A known name asked for AAAA or any other type answers NOERROR with
  no records. An unknown name under the zone answers NXDOMAIN. The zone
  apex answers NOERROR with no records. TTL is 30 s: netmaps change.
- Names outside the zone: the client resolver answers REFUSED (the OS
  only sends `.thawr` queries there); the hub resolver forwards them
  (below).
- Answers depend on who asks. The client resolver knows the netmap, so
  it knows exactly the peers this device may see. The hub resolver
  answers a querying overlay address only with peers that the policy
  makes visible to that peer (the same `Visibility` that decides key
  distribution), plus `hub.thawr`. A name therefore discloses nothing a
  key would not (threat model A7).
- Queries from a source outside the overlay prefix are dropped; the
  local host (a loopback source) is always answered. Both
  resolvers listen on overlay addresses only, which are reachable only
  through WireGuard; neither is an open resolver.

### `internal/dns`

One resolver used by both sides, with the data source injected:

```go
type Source interface {
    Lookup(ctx context.Context, from netip.Addr, name string) (netip.Addr, bool)  // name without the zone
    Reverse(ctx context.Context, from netip.Addr, addr netip.Addr) (string, bool)
}
type Options struct {
    Zone      string          // "thawr"
    Source    Source
    Upstreams []netip.AddrPort // empty: REFUSED outside the zone
    Allow     netip.Prefix     // sources outside it are dropped
    Timeout   time.Duration    // per upstream attempt, 2 s
    Logger    *slog.Logger
}
func NewServer(o Options) *Server
func (s *Server) Handle(ctx context.Context, req []byte, from netip.Addr, tcp bool) ([]byte, error)
func (s *Server) Serve(ctx context.Context, udp net.PacketConn, tcp net.Listener) error
func Listen(ctx context.Context, addr netip.AddrPort) (net.PacketConn, net.Listener, error)
```

- Wire codec: `golang.org/x/net/dns/dnsmessage` (BSD-3, pure Go,
  already in the module graph). No third-party DNS library.
- UDP and TCP (two-byte length prefix) on the same address. UDP
  answers over 512 bytes without EDNS are truncated with TC set; the
  client then retries over TCP as the protocol says.
- Forwarding (hub only): a query outside the zone is sent to each
  upstream in turn over the transport it arrived on, with `Timeout`
  per attempt; the first answer is returned unchanged, ID preserved.
  No cache, no recursion of our own, no upstream of our own choosing:
  the upstreams are the server host's resolvers or what the operator
  configured. When there are none the answer is REFUSED.
- Registrars tell the OS to send `.thawr` queries to the resolver,
  driven by an injected `Runner` and `Root` as `internal/svc` does, so
  unit tests never run `resolvectl` or write under `/etc`:

  ```go
  type Registrar interface {
      Register(ctx context.Context, iface string, server netip.Addr) (method string, err error)
      Update(ctx context.Context, entries []Entry) error   // hosts mode only
      Unregister(ctx context.Context, iface string) error
  }
  ```

  - Linux with systemd-resolved (`/run/systemd/resolve/` exists and
    `resolvectl` is on PATH): `resolvectl dns <iface> <ip>`,
    `resolvectl domain <iface> ~thawr`, `resolvectl default-route
    <iface> false`; unregister is `resolvectl revert <iface>`.
    Method `resolved`.
  - Linux otherwise: a block in `/etc/hosts` between `# thawr begin`
    and `# thawr end` with one line per peer, `<ip> <name>.thawr`,
    hub included (only the zone name, so a peer never shadows a LAN
    host of the same bare name). The file is rewritten atomically (temp
    file in the same directory, same mode, rename) on every netmap;
    all other lines are preserved byte for byte; unregister removes
    the block. Method `hosts`.
  - macOS: `/etc/resolver/thawr` containing `nameserver <ip>` and
    `port 53` (directory 0755, file 0644); removed on unregister;
    `dscacheutil -flushcache` is run best effort. Method
    `resolver-file`.
  - Windows: PowerShell `Add-DnsClientNrptRule -Namespace .thawr
    -NameServers <ip> -Comment thawr`; unregister removes rules with
    that comment. Compile-checked, manual checklist. Method `nrpt`.
  - Anything else: `ErrUnsupported`; the client logs it once and
    serves anyway (method `none`).

### Client

- `DaemonOptions.DNS{Mode, Port, Registrar}`; `--dns on` (default)
  serves and registers, `serve` only serves (people who configure
  their resolver themselves, and the integration tests, which must not
  touch the host's `/etc`), `off` does neither.
- The daemon implements `Source` over its current netmap: peers, self
  and hub. After the device is configured and before the local API
  starts, it binds `<self ip>:53` UDP and TCP. A bind failure (another
  resolver on that address) is logged and shown in status; the daemon
  keeps running.
- Registration happens once after the first netmap is applied (the
  interface has its address then), preceded by an `Unregister` to
  clear what a crashed instance may have left. `apply` calls
  `Registrar.Update` with the current entries (a no-op outside hosts
  mode). `Unregister` runs when the daemon exits, so `client down`
  leaves the resolver configuration as it found it.
- Status document: `dns{listen, state, method, names, error}` with
  `state` in `serving | error | off`; optional object, present when the
  daemon runs with `--dns` other than `off`. The header's second line
  appends `· DNS: .thawr via <method>`, or `DNS: error (<reason>)`, or
  `DNS: serving, not registered` for `serve` and unsupported
  platforms.
- `Daemon.Ping` and `client ping` strip a trailing `.thawr` (with or
  without the final dot) before the lookup.

### Server

- `dns.enabled` (default true): after the hub interface is up the
  server binds `<hub ip>:53` UDP and TCP with a `Source` over the peer
  registry, cached per hub generation as `control.KeyVisibility` does,
  filtered by the policy's visibility from the querying peer's
  address. A bind failure is a start-up error: the operator asked for
  it, and `dns.enabled: false` turns it off.
- `dns.upstream`: each entry is an IP or IP:port (port 53 default),
  validated on load. When empty, the server reads `nameserver` lines
  from `/etc/resolv.conf` at start (Linux and macOS); on Windows or
  when none is found, forwarding is off and the log says that phones
  will resolve only `.thawr`. The list is logged at start.
- Mobile config (spec 008): `[Interface]` gains `DNS = <hub ip>,
  thawr` when `dns.enabled`, so the WireGuard app sends its queries to
  the hub and `ssh nas` works with the search domain. The QR payload
  stays under the size the app accepts; `TestQRRoundTrip` proves it.
- `thawr admin status` gains `dns_listen` (`100.64.0.1:53`, or empty).

### Documentation

README (a "Names" paragraph in Quick start and the phone note),
ARCHITECTURE (§2 `internal/dns`, §4.8 "Names", §5 local API `dns`, §8
dependency row), THREAT_MODEL (A7: names follow visibility), TESTING
(integration row, manual checklist per platform), CLAUDE.md layout.

## Acceptance criteria

- [x] With two enrolled clients A and B, `getent hosts b.thawr` on A
      (or a Go resolver pointed at A's overlay address) returns B's
      overlay IPv4 within one netmap generation of B's enrollment, and
      stops resolving (NXDOMAIN) after `thawr admin peer remove b`.
- [x] `dig -x 100.64.0.1 @<self ip>` answers `hub.thawr.`; AAAA for a
      known name is NOERROR without records; `example.com` at the
      client resolver is REFUSED.
- [x] Linux with systemd-resolved: `resolvectl status <iface>` shows
      the DNS server and the `~thawr` routing domain after `client up`
      and nothing after `client down`. Linux without it: `/etc/hosts`
      carries the block with every peer, and only that block changes;
      `client down` removes it and leaves the rest byte-identical.
- [x] macOS: `scutil --dns` lists the `thawr` resolver after `client
      up`; `/etc/resolver/thawr` is gone after `client down` (manual
      checklist).
- [x] `client status` shows `DNS: .thawr via <method>` and `--json`
      validates against `docs/status.schema.json` with the `dns`
      object; `--dns off` omits it.
- [x] A phone joined with a QR from a `dns.enabled` server resolves a
      peer the policy lets it reach through `100.64.0.1`, gets NXDOMAIN
      for a peer the policy hides, and resolves an internet name
      through the server's upstream (integration test with a fake
      upstream; manual on a real phone).
- [x] A query from an address outside the overlay to either resolver
      gets no answer.
- [x] `client ping nas.thawr` behaves like `client ping nas`.
- [x] `--dns serve` binds and answers but leaves `/etc` untouched.

## Test cases

- `internal/dns`: `TestHandleA`, `TestHandlePTR`, `TestHandleNoData`,
  `TestHandleNXDomain`, `TestHandleApex`, `TestHandleRefusesOutside`,
  `TestHandleDropsForeignSource`, `TestForwardUDP`,
  `TestForwardTimeoutNextUpstream`, `TestTCPFraming`,
  `TestTruncation`; registrars: `TestResolvedCommands`,
  `TestHostsBlockInsertUpdateRemove` (golden), `TestResolverFile`,
  `TestNRPTCommands`.
- `internal/client`: `TestDaemonServesNames` (fake device, netmap with
  two peers, query self/peer/hub/PTR), `TestDNSRegisterOnce`,
  `TestPingStripsSuffix`; schema test extended with the `dns` object.
- `internal/config`: `TestValidateDNS`, `TestParseResolvConf`
  (fixture with comments, `search`, `options`, IPv6 nameserver).
- `internal/server`: `TestHubResolverHonoursVisibility`.
- `internal/api`: `TestMobileConfigDNS`, `TestQRRoundTrip` with the
  DNS line.
- `cmd/thawr`: `TestStatusDNSLine` (golden).
- Integration: `TestDNSNames` (two clients, A and PTR, removal),
  `TestDNSPhoneViaHub` (wg-quick phone, visibility, forwarding to a
  fake upstream; skips without `wg-quick`).

## Out of scope

- A configurable zone or per-owner subdomains (`nas.alice.thawr`).
- IPv6 records (no IPv6 overlay yet), DNSSEC, DoT/DoH, a resolver
  cache, serving LAN names or split-horizon for anything but `.thawr`.
- Registering the resolver on Linux without systemd-resolved other
  than through `/etc/hosts` (NetworkManager, resolvconf, openresolv).
- Exposing the hub resolver to the internet, or to phones for anything
  the server host's own resolver would not answer.
