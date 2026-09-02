# Spec 003 — Key distribution (netmap sync)

Sprint 1. Depends on: 001, 002. Packages: `internal/control`
(netmap builder, endpoint table), `internal/api` (gRPC `Sync`,
`ReportEndpoints`, `RotateKey`, `Leave`), `internal/wg` (kernel and
userspace adapters), `cmd/thawr` (client daemon).

## Goal

Every enrolled client holds a live view of the peers it may talk to and
keeps its WireGuard interface in sync with that view. Changes on the
server reach every affected client within 5 seconds. Key rotation and
peer removal need no manual action on any device.

## User story

As a user, when I enrol a new server, my laptop can ping it within
seconds without me touching either machine. When the admin deletes a
peer, it disappears from my WireGuard configuration immediately.

## Behaviour

Server:
- Keeps `generation` in `meta`, incremented in the same transaction as
  any peer, key, or policy change, and in memory on endpoint changes
  (endpoint changes are not persisted).
- `Sync(SyncRequest{generation})`: authenticates the node secret, marks
  the peer online, streams a full `NetMap` immediately if the client's
  generation is stale, then a full `NetMap` on every subsequent change
  affecting this peer. Full maps, not diffs; the map for one peer is
  small (hundreds of entries at most). Coalesce changes within 200 ms.
- Sends a keepalive `NetMap` with no change every 30 s; a peer whose
  stream is gone for 90 s is marked offline (generation bump).
- Visibility in this spec, before spec 006 exists: **every peer sees
  every peer with the same owner, and every peer sees the hub**. Spec
  006 replaces this rule with the policy engine through the
  `Visibility` interface, which is defined here.
- `NetMap` contents per peer: `generation`, `self` (ipv4, name),
  `peers[]` (`id`, `name`, `kind`, `public_key`, `ipv4`, `online`,
  `endpoints[]`, `hub` flag, `allowed_ips[]`), `filter[]` (spec 006; empty
  now = allow all between visible peers), `hub` (public key, endpoint,
  allowed IPs including all static peers).
- `ReportEndpoints`: stores candidates in memory with a 5-minute TTL,
  bumps in-memory generation for peers that see this peer.
- `RotateKey{new_public_key}`: updates the peer, bumps generation; the
  client swaps the local key after the RPC returns.
- `Leave`: deletes the peer as if by admin.

Client daemon (`thawr client up` stays in the foreground unless
`--daemon`; systemd/launchd units are docs, not code):
1. Load `state.json` and `node.key`; if a cached netmap exists, apply it
   to WireGuard before contacting the server.
2. Create the interface (`thawr0`; kernel if
   `wgctrl` can open it after `ip link add ... type wireguard`
   succeeds, else `wireguard-go` TUN), set private key, listen port
   (random ephemeral, persisted in `state.json`), address `ipv4/32`,
   route `overlay.cidr` via the interface.
3. Open `Sync`; on each `NetMap`: compute the desired peer set, apply
   the diff through `wg.Device.Configure` (add/update/remove peers,
   `AllowedIPs`, `PersistentKeepalive = 25` toward the hub and toward
   peers marked `keepalive`), write the netmap to the cache.
4. Reconnect with exponential backoff (1 s → 60 s, jitter) on stream
   loss; WireGuard keeps working from the last map meanwhile.
5. Start the local control socket for `status` (spec 007 fills it in).
6. Every 60 s and on interface change: report endpoints (spec 004 adds
   STUN; here only local addresses).

`wg.Device` interface (consumed by the client and the server hub):

```go
type Device interface {
    Configure(ctx context.Context, cfg Config) error   // full desired state, adapter diffs
    Stats(ctx context.Context) ([]PeerStats, error)    // last handshake, rx, tx, endpoint
    Close() error
}
```

## Acceptance criteria

- [ ] After enrolling B while A is connected, A's WireGuard shows B as a
      peer within 5 s (kernel and userspace adapters).
- [ ] Deleting B on the server removes B from A within 5 s, and B's
      `Sync` stream is closed with `PERMISSION_DENIED`.
- [ ] A client started with the server unreachable applies its cached
      netmap and can still exchange traffic with peers whose endpoints
      are unchanged; it connects when the server returns.
- [ ] Stream reconnect uses backoff; no more than one reconnect attempt
      per second at any time.
- [ ] `RotateKey` results in peers using the new key within 5 s and no
      lost connectivity longer than one handshake interval.
- [ ] The netmap sent to a peer contains only visible peers
      (`TestNetMapVisibility`), never node secrets or private keys
      (`TestNetMapNoSecrets`).
- [ ] A stale node secret (deleted peer) gets `PERMISSION_DENIED` on
      every RPC.
- [ ] Offline detection: killing a client marks it offline within 90 s
      and its peers receive a map with `online=false`.
- [ ] The whole flow works on Linux kernel WireGuard, Linux
      `wireguard-go`, macOS, Windows (macOS/Windows in CI via unit tests
      with a fake device; manual checklist in `docs/TESTING.md` for the
      real adapters until integration runners exist).

## Test cases

- `TestNetMapBuilder` (table: owners, hub, static peers → expected map).
- `TestNetMapVisibility`, `TestNetMapNoSecrets`.
- `TestGenerationBumpOnPeerChange`, `TestCoalesce`.
- `TestSyncStreamsOnChange` (in-process gRPC with bufconn).
- `TestOfflineAfter90s` (injected clock).
- `TestClientApplyDiff` (fake `wg.Device`: add, update endpoint, remove).
- `TestClientCachedNetMap`.
- `TestBackoff`.
- `TestRotateKey`.
- `TestKernelAdapter` and `TestUserspaceAdapter` (build tag
  `integration`, Linux, CAP_NET_ADMIN).
- Integration: `TestEncryptedPingTwoClients` — server and two clients in
  three netns connected by a veth "internet"; after enrolment `ping
  -c 3` between overlay addresses succeeds; a tcpdump on the veth sees
  only WireGuard UDP (no ICMP). This is the project's canonical
  integration test.

## Out of scope

- STUN, hole punching, relay (specs 004, 005). In this spec peers reach
  each other only when directly routable (as in the netns test).
- Port filtering (spec 006).
- Netmap signing, per-peer key pinning (phase 2, threat model T4).
- Diff-based netmaps for very large networks.
