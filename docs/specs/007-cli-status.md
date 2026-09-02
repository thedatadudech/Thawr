# Spec 007 — CLI status

Sprint 2. Depends on: 003, 004, 005 (path states), 006 (filter counters
optional). Packages: `cmd/thawr` (`client status`, `client ping`,
`admin peer list`), client local API.

## Goal

A user can see in one command whether the client is connected, which
peers exist, how each is reached, and whether traffic flows, in human
and JSON form. An admin can see the same across the whole network.

## User story

As a user, when something does not work I run `thawr client status` and
immediately see whether the problem is the control connection, the path
to one peer, or the policy.

## Commands

```
thawr client status [--json] [--watch]
thawr client ping <peer-name> [--count 3]
thawr admin peer list [--json] [--online]
thawr admin peer show <name> [--json]
```

`client status` output:

```
thawr 0.1.0 · alice-laptop 100.64.0.7 · server vpn.example.com connected (netmap #42, 3s ago)
WireGuard: kernel · thawr0 · listen 41820 · NAT: cone (reflexive 203.0.113.9:41820)

PEER          IP           KIND    OWNER   PATH                     HANDSHAKE   RX / TX
homelab-nas   100.64.0.3   server  -       direct 198.51.100.4:51820   12s        1.2 MB / 340 kB
build-box     100.64.0.9   agent   -       relay                    3m          0 B / 0 B
alice-phone   100.64.0.21  human   alice   via hub                  -           -
bob-laptop    100.64.0.12  human   bob     idle                     never       -
hub           100.64.0.1   server  -       direct vpn.example.com:51820  25s     4 kB / 4 kB

Filter: 0 dropped (last 5 min)
```

Path column values: `direct <endpoint>`, `relay`, `probing`, `via hub`
(static peers), `idle` (visible, no traffic intent), `unreachable`,
`offline` (server reports peer offline). Control connection values:
`connected`, `reconnecting (attempt N, next in Ns)`, `cached netmap
(server unreachable since T)`.

`--json` emits one object: `version`, `self{name, ipv4, kind}`,
`server{addr, state, generation, last_message_at}`, `wireguard{backend,
interface, listen_port}`, `nat{type, reflexive[]}`, `peers[]` with all
columns as fields plus `endpoint_candidates`, `last_handshake_at`
(RFC 3339 or null), `rx_bytes`, `tx_bytes`, `filter{dropped_5m}`.
`--watch` redraws every 2 s.

`client ping` sends ICMP echo to the peer's overlay address via the
system ping (or `x/net/icmp` where permitted), forces the path state
machine to probe if `idle`, and prints path state changes it observes.

`admin peer list`: name, ip, kind, owner, tags, online, last seen,
current path summary from `ReportPath` (`direct`/`relay` counts),
version, OS. `admin peer show`: everything the server knows including
endpoint candidates and the compiled filter for that peer.

## Behaviour

The client daemon serves `GET /status` on the local socket
(`/var/run/thawr/client.sock`, 0660 root:thawr; Windows named pipe with
an ACL for Administrators). The CLI is a thin client: it never reads
WireGuard directly, so output is identical for kernel and userspace
backends. If the socket is absent: "thawr client is not running" and
exit 3.

Exit codes: 0 connected, 1 running but server unreachable, 2 usage
error, 3 daemon not running. Scripts can rely on these.

## Acceptance criteria

- [ ] Output matches the layout above (golden test with fixed data);
      columns align for names up to 20 characters, longer names are
      truncated with `…`.
- [ ] `--json` is stable and documented; a schema file
      `docs/status.schema.json` validates it in tests.
- [ ] Every path state from specs 004/005 is rendered.
- [ ] Byte counts use SI units with one decimal; durations use `12s`,
      `3m`, `2h`, `never`.
- [ ] Exit codes as specified.
- [ ] `--watch` stops cleanly on `Ctrl-C`.
- [ ] `client ping` on an `idle` peer triggers probing and shows the
      resulting path.
- [ ] `admin peer show` includes the compiled filter rules for the peer.
- [ ] No secrets in any output (`status` never prints node secret or
      private key even with `--json`).

## Test cases

- `TestStatusRender` (golden), `TestStatusRenderLongNames`.
- `TestStatusJSONSchema`.
- `TestHumanizeBytes`, `TestHumanizeDuration`.
- `TestStatusExitCodes` (fake socket states).
- `TestClientPingTriggersProbe` (fake daemon API).
- `TestAdminPeerShowFilter`.

## Out of scope

- Historical metrics, graphs, Prometheus (phase 2).
- Editing anything from `status`.
- A TUI.
