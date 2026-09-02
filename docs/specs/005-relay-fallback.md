# Spec 005 — Relay fallback

Sprint 2. Depends on: 003, 004. Packages: `internal/relay` (server,
client, frame codec), `internal/api` (HTTP upgrade route), `cmd/thawr`.

## Goal

When no direct path exists, two peers exchange WireGuard packets through
the relay built into the server. Traffic stays end-to-end encrypted; the
relay learns only which peers talk and how much.

## User story

As a user on hotel Wi-Fi with a symmetric NAT, I can still SSH to my
home server. `thawr client status` shows `relay` for that peer, and it
switches to `direct` on its own when I move to a better network.

## Protocol

Transport: the client opens `GET /relay` on the HTTPS listener with
headers `Connection: Upgrade`, `Upgrade: thawr-relay/1`,
`Authorization: Bearer <node secret>`. The server responds `101` and the
connection becomes a bidirectional frame stream.

Frame: `type (1 byte) | key (32 bytes) | length (2 bytes, big endian) |
payload (length bytes)`. Maximum payload 1500 bytes; larger frames close
the connection.

| Type | Direction | key | Meaning |
|---|---|---|---|
| 0x01 `SEND` | client→server | destination public key | Forward payload to that peer |
| 0x02 `RECV` | server→client | source public key | Payload from that peer |
| 0x03 `PING` / 0x04 `PONG` | both | zero | Keepalive every 30 s; 3 missed → close |
| 0x05 `PEER_GONE` | server→client | peer key | Destination not connected or not visible (rate limited to 1/s per pair) |

Server:
- One session per peer; a new connection for the same peer replaces the
  old one.
- Forwards `SEND` only if source and destination are mutually visible in
  the current netmap (`control.Visibility` lookup, cached per
  generation). Otherwise replies `PEER_GONE` and counts a violation;
  10 violations per minute close the session.
- Per-session send queue of 256 frames; drops on overflow (UDP
  semantics), counts drops.
- Optional per-peer rate limit `relay.max_bytes_per_second` (config,
  default 0 = unlimited).
- Exposes counters in `/api/v1/status`: sessions, frames, bytes, drops.

Client (`relay.Client`):
- One connection, opened lazily when the first peer enters `relay`,
  closed after 5 min with no relayed peers. Reconnect with backoff.
- For each relayed peer: a UDP socket on `127.0.0.1:0`; the WireGuard
  endpoint is set to that address. Datagrams received on it become
  `SEND` frames to the peer's key; `RECV` frames from that key are
  written to the WireGuard listen port on `127.0.0.1`.
- When the path state machine moves a peer to `direct`, the endpoint is
  switched and the local socket is closed after 10 s.
- Path state machine (spec 004): `probing` exhausted → `relay`; in
  `relay`, re-probe every 60 s; direct handshake success → `direct`.

## Acceptance criteria

- [ ] Two clients behind symmetric NATs (netns) exchange `ping` via the
      relay within 10 s of both being online.
- [ ] A tcpdump on the server's "internet" side sees only TLS to 443;
      the relay never receives a frame that is not a valid WireGuard
      message type (1–4) in its first byte, and a test asserts the relay
      never logs payload bytes.
- [ ] A frame to a non-visible peer is not forwarded and yields
      `PEER_GONE`; repeated violations close the session.
- [ ] An unauthenticated or wrong-secret upgrade is refused with 401
      before any frame is read.
- [ ] Relayed peers upgrade to `direct` when a direct path becomes
      possible (test: remove the NAT rule mid-test) with no packet loss
      longer than 2 s.
- [ ] Relay connection is not opened while no peer needs it, and closes
      after 5 min unused.
- [ ] Throughput through the relay in the netns test ≥ 50 Mbit/s with
      `iperf3` (sanity, not a benchmark).
- [ ] Server restart: clients reconnect the relay within 10 s.

## Test cases

- `TestFrameCodec` (round trip, oversize, truncated).
- `TestRelayAuth` (401 without/with wrong secret).
- `TestRelayVisibility` (fake visibility: forwards allowed, drops
  others, `PEER_GONE`).
- `TestRelayReplaceSession`.
- `TestRelayQueueOverflow`.
- `TestRelayClientProxy` (local UDP ↔ frames with a fake server).
- `TestPathRelayUpgrade` (state machine).
- Integration: `TestRelaySymmetricNATs`, `TestRelayToDirectUpgrade`,
  `TestRelayThroughput`.

## Out of scope

- Multiple or external relay nodes, relay-to-relay meshing (phase 2:
  `thawr relay`).
- Relay over plain UDP (DERP does TLS/TCP; same here).
- Bandwidth accounting per user, quotas.
- Relay for static (mobile) peers: they use the hub, not the relay.
