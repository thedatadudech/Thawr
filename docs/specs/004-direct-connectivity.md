# Spec 004 — Direct connectivity (STUN + hole punching)

Sprint 2. Depends on: 003. Packages: `internal/stun` (copied codec),
`internal/control` (endpoint table, candidate ordering), `internal/wg`
(handshake observation), `cmd/thawr` (client prober), server STUN
listener.

## Goal

Two peers behind ordinary NATs establish a direct WireGuard path without
any manual port forwarding. Peers on the same LAN use LAN addresses.

## User story

As a user, my laptop in a café and my home server behind my ISP router
talk directly with LAN-like latency; `thawr client status` shows
`direct` for that peer.

## Behaviour

Server:
- STUN binding server on `listen.stun` (two UDP ports). Responds with
  `XOR-MAPPED-ADDRESS`. Rate limit 20 req/s per source IP. No TURN.
- Accepts `ReportEndpoints{local[], reflexive[], symmetric bool, listen_port}`.
- Netmap includes for every visible peer its candidates in priority
  order plus `symmetric`.

Client, endpoint discovery (every 60 s, on interface change, on
reconnect):
1. Local candidates: every non-loopback, non-`thawr0` unicast address
   with the WireGuard listen port. Link-local excluded.
2. Reflexive: send STUN to both server STUN ports from the WireGuard
   socket's port. With kernel WireGuard the client cannot share the
   socket, so it sends from a separate socket bound to the same port
   with `SO_REUSEPORT`/`SO_REUSEADDR` (Linux, macOS); on Windows the
   userspace device exposes its conn and the STUN request goes through
   `wireguard-go`'s bind. If the two reflexive ports differ,
   `symmetric = true`.
3. Report only when the set changed or 60 s passed.

Client, path establishment per visible peer (state machine in
`internal/control/path`):
- States: `idle` (no traffic wanted), `probing`, `direct`, `relay`
  (spec 005), `unreachable`.
- Trigger: first time a peer appears, and whenever its candidates change,
  and every 60 s while in `relay`.
- Candidate order (identical on both sides, computed from the netmap):
  LAN candidates sharing a `/24` with one of ours, then other local
  candidates, then reflexive, then the hub-stable endpoint if the peer
  is the hub. Ties broken by sorting the address string.
- For each candidate: set the peer's endpoint, send an ICMP echo to the
  peer's overlay address to trigger a handshake, wait up to 2 s for
  `last_handshake_time` to advance. Success → `direct`, record the
  winning candidate, report path. Exhausted → `relay` (spec 005) or
  `unreachable` until spec 005 exists.
- If both peers are `symmetric`, skip probing reflexive candidates.
- In `direct`, if no handshake for 3 min while traffic is queued
  (`tx` grows, `rx` does not), go back to `probing`.
- WireGuard roaming remains enabled: if the kernel learns a new endpoint
  from an authenticated packet, the client adopts it (read from
  `Stats`).

## Acceptance criteria

- [ ] Two clients behind two distinct full-cone or restricted-cone NATs
      (simulated with nftables masquerade in netns) reach `direct`
      within 10 s and `ping` works.
- [ ] Two clients on the same LAN segment choose the LAN candidate even
      when reflexive candidates exist.
- [ ] A client behind a symmetric NAT and one behind a cone NAT reach
      `direct` (the cone side's reflexive address works).
- [ ] Two symmetric NATs result in `unreachable` (until spec 005) within
      10 s, without a probe storm (≤ 1 probe per 2 s per peer).
- [ ] STUN server answers correctly and refuses to exceed the rate limit.
- [ ] Endpoint reports are sent only on change or every 60 s.
- [ ] `ReportPath` reflects `direct` with the endpoint in use; admin UI
      peer detail shows it.
- [ ] No probing when there is no traffic intent (a peer nobody talks to
      stays `idle`; probing starts on first outbound packet or on
      explicit `thawr client ping <peer>`).

## Test cases

- `TestSTUNServerBinding`, `TestSTUNRateLimit`.
- `TestCandidateOrdering` (table with LAN, reflexive, symmetric cases;
  asserts both sides compute the same order).
- `TestPathStateMachine` (fake device with scripted handshake times).
- `TestSymmetricDetection`.
- `TestEndpointReportDedup`.
- Integration (`tests/nat_test.go`): topology with three netns
  (server, client A, client B) each behind its own NAT netns; parametrised
  over cone/cone, cone/symmetric, symmetric/symmetric.

## Out of scope

- Relay (spec 005); this spec ends at `unreachable`.
- IPv6 candidates (phase 2).
- UPnP / NAT-PMP port mapping.
- Port prediction for symmetric NATs.
- Userspace packet multiplexing (magicsock-style); ADR 0004 keeps the
  WireGuard device as the only sender.
