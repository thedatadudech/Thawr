# Thawr — Architecture

This document is normative. Implementation sessions follow it; changes go
through an ADR in `docs/adr/`.

## 1. Components

One binary, three roles:

| Role | Command | Runs where | Responsibilities |
|---|---|---|---|
| Server | `thawr server --config server.yaml` | One host with a public IP or forwarded ports | Peer registry, enrollment, key distribution, policy evaluation, STUN, relay, WireGuard hub for mobile peers, admin REST API and UI |
| Client | `thawr client up|down|status` | Every laptop, server, VM | Generates and holds the node key, configures the local WireGuard interface, discovers endpoints, probes direct paths, falls back to relay, enforces the receiver-side port filter |
| Admin | `thawr admin ...` | On the server host (Unix socket) or remotely (REST with a session) | Users, tokens, peers, policy check and reload, mobile QR export |

Network ports on the server, all configurable:

| Port | Protocol | Purpose |
|---|---|---|
| 443 | TCP, TLS | gRPC (clients), REST + admin UI (browsers), relay (HTTP Upgrade `thawr-relay/1`) |
| 3478, 3479 | UDP | STUN. Two ports so a client can detect endpoint-dependent (symmetric) NAT |
| 51820 | UDP | Server's own WireGuard interface (the hub). Used by mobile peers and by any peer as a stable direct endpoint to the server |

```mermaid
flowchart LR
  subgraph Server host
    S[thawr server]
    DB[(SQLite)]
    HUB[WireGuard hub thawr0]
    S --- DB
    S --- HUB
  end
  subgraph Laptop
    C1[thawr client] --- W1[WireGuard]
  end
  subgraph Home server behind NAT
    C2[thawr client] --- W2[WireGuard]
  end
  P[Phone with WireGuard app]
  A[Admin browser / CLI]

  C1 -- gRPC TLS 443 --> S
  C2 -- gRPC TLS 443 --> S
  C1 -- STUN UDP 3478 --> S
  C2 -- STUN UDP 3478 --> S
  W1 <-. direct UDP, when NAT allows .-> W2
  C1 -- relay TLS 443 --> S
  C2 -- relay TLS 443 --> S
  P -- WireGuard UDP 51820 --> HUB
  A -- REST TLS 443 / unix socket --> S
```

## 2. Packages

Dependencies point downward only. `cmd/thawr` parses flags and hands
the config to `internal/server`, which wires everything; nothing below
`cmd` imports a sibling that is not listed here.

```mermaid
flowchart TD
  CMD[cmd/thawr] --> SRV[internal/server]
  CMD --> CFG[internal/config]
  SRV --> API[internal/api]
  SRV --> RELAY[internal/relay]
  SRV --> WG[internal/wg]
  SRV --> WEB[web]
  SRV --> STORE[internal/store]
  SRV --> STUN[internal/stun]
  WG --> STUN
  API --> CTRL[internal/control]
  API --> RELAY
  CTRL --> STORE
  RELAY --> CTRL
```

| Package | Single responsibility | Exposes | Depends on |
|---|---|---|---|
| `internal/config` | Load one YAML file, apply defaults, validate | `Load(path) (*Config, error)`, `Config` struct, `Default()` | stdlib, yaml |
| `internal/store` | Persist peers, users, tokens, policy generation in SQLite; run migrations | `Open(dsn) (*Store, error)`, `Store.Peers()`, `Store.Users()`, `Store.Tokens()`, `Store.Meta()`, each a small interface with ctx-first methods | `modernc.org/sqlite` |
| `internal/control` | Everything that decides: enrollment, registry, key distribution (netmap), policy compilation, endpoint tracking, IP allocation | `Enroller`, `Registry`, `Policy` (parse + compile), `NetMapBuilder`, `EndpointTable`, `Allocator` | `internal/store` |
| `internal/wg` | Talk to a WireGuard device without knowing about Thawr | `Device` interface (`Configure` diffs peers in place, `SetPeer`, `RemovePeer`, `Stats`), `Open(ctx, Options)` (kernel via wgctrl on Linux, else wireguard-go configured through its in-process IPC API, no UAPI socket), `STUNCapable` (the userspace device sends STUN from its own socket), `Filter` interface (spec 006), `wgtest.Fake` | `wgctrl`, `wireguard-go`, netlink / nftables, `internal/stun` |
| `internal/stun` | STUN binding codec (copied from Tailscale), `Discover` client with symmetric-NAT detection, rate-limited `Serve` | `Request`, `ParseResponse`, `Transport`, `Discover`, `Serve` | stdlib |
| `internal/control/path` | Candidate ordering both sides compute identically and the per-peer path state machine (pure, clock and stats injected) | `Order`, `Machine.Step` | `internal/control` types |
| `internal/relay` | Forward opaque packets between authenticated peers; local UDP proxy on the client | `Server`, `Client`, frame codec | `internal/control` (for auth + visibility check via a small interface) |
| `internal/api` | gRPC service and REST handlers; translate wire types to `control` calls; no business logic | `NewGRPC(deps)`, `NewREST(deps)`, `Combine` (one listener for both), protobuf under `internal/api/proto` generated with buf via `make proto` | `internal/control`, `internal/relay` |
| `internal/client` | Device side independent of the CLI: state directory (node key, enrollment state, netmap cache), TLS fingerprint pinning, the Enroll call, the sync daemon with endpoint discovery and the path prober, and its local socket API | `Enroll(ctx, Options)`, `NewDaemon(DaemonOptions)`, `Daemon.Run`, `Daemon.Ping`, `BuildConfig`, `NewLocalClient`, `LoadState`, `Forget` | `internal/api/proto`, `internal/wg`, `internal/stun`, `internal/control/path` |
| `internal/server` | Compose the server: data dir, store, server key, TLS, hub interface, policy, listeners; startup order, readiness, reload, shutdown | `New(cfg, Deps)`, `Run(ctx, reload)`, `Check()`, `Status(ctx)` | `internal/config`, `internal/store`, `internal/wg`, `internal/control`, `internal/api`, `web` |
| `web` | Static admin UI, embedded | `embed.FS` | nothing |
| `cmd/thawr` | Flags, config, dependency wiring, signal handling | `main` | all of the above |

Interfaces are declared in the consuming package. `control` declares the
storage interfaces it needs; `store` satisfies them. `api` declares what
it needs from `control`; tests use fakes.

## 3. Identity model

Every entity that can hold a WireGuard key is a **peer**. A peer has:

| Field | Meaning |
|---|---|
| `id` | 128-bit random, hex |
| `name` | Unique, DNS-label safe, e.g. `alice-laptop`; derived from hostname at enrollment, admin can rename |
| `kind` | `human`, `server`, `agent`. Informational in v1; phase 2 attaches workload identity semantics without changing the schema |
| `mode` | `agent` (runs `thawr client`, participates in NAT traversal) or `static` (plain WireGuard config, e.g. a phone; routed via the hub) |
| `owner` | Optional reference to a user. Servers and agents may be unowned and are addressed by tags |
| `tags` | Set of `tag:name`. Policy addresses peers by owner, group, or tag |
| `public_key` | WireGuard public key. Private key never leaves the device for `agent` peers |
| `ipv4` | Overlay address, `/32`, allocated by the server from `overlay.cidr` |
| `node_secret_hash` | SHA-256 of the bearer secret an `agent` peer uses on the control channel |
| `last_seen_at`, `created_at`, `expires_at` | Lifecycle |

**Users** are local accounts with a name, role (`admin`, `member`), and
an argon2id password hash. Users log into the admin UI and own peers.
Members can list their own peers and create tokens for themselves;
admins can do everything. OIDC, when enabled, maps an external subject to
a local user record. The policy engine only ever sees users, groups and
tags, so the identity source is irrelevant to it.

**Enrollment tokens** are single-use secrets created by an admin (or a
member for their own peers). A token pins: owner, kind, tags, expiry.

## 4. Data flows

### 4.1 Server bootstrap

On first start with `public_addr: vpn.example.com` the server creates
`data_dir` (default `/var/lib/thawr`), opens `thawr.db`, runs migrations,
generates its WireGuard key (`server.key`, 0600), generates a self-signed
TLS certificate valid 10 years with SAN = public host
(`tls/cert.pem`, `tls/key.pem`), brings up `thawr0` with `100.64.0.1/10`,
starts STUN, relay, gRPC, REST, and prints the TLS fingerprint. Subsequent
starts reuse everything. Spec 001.

### 4.2 Enrollment

```mermaid
sequenceDiagram
  participant Adm as Admin
  participant S as Server
  participant C as New client
  Adm->>S: thawr admin token create --owner alice --kind human --expires 1h
  S-->>Adm: token thawr_xxx (shown once) + join command incl. server URL and TLS fingerprint
  Adm->>C: paste join command
  C->>C: generate WireGuard keypair, store node.key 0600
  C->>S: gRPC Enroll(token, public_key, hostname, os, version) over TLS (fingerprint pinned)
  S->>S: hash token, lookup, check expiry and unused, mark used
  S->>S: allocate ipv4, create peer, generate node secret
  S-->>C: EnrollResponse(peer_id, ipv4, node_secret, overlay cidr, server hub key + endpoint)
  C->>C: persist state.json 0600 (server, fingerprint, peer_id, node_secret)
  C->>S: Sync stream (authorization: Bearer node_secret)
```

Token secret: 32 bytes from `crypto/rand`, base64url, prefixed
`thawr_`. The server stores only SHA-256(secret). Default expiry 1 hour,
maximum 30 days. A used or expired token is rejected with the same error
code. Spec 002.

### 4.3 Key distribution (netmap sync)

The server holds a **netmap generation**: a persisted counter bumped by
every change to peers or policy, mirrored by an in-memory sequence in
the presence hub that also advances on endpoint and presence changes
and is what netmaps and the status endpoint carry. Each client holds
one server-streaming gRPC call `Sync`. On connect the server always
sends a full map (the client's reported generation is informational);
on every change affecting the peer, coalesced over 200 ms, and every
30 s as a keepalive, it sends the full map again. It computes that
peer's view:

- the peers it may talk to (policy says A→B or B→A allows at least one
  port), each with public key, ipv4, current endpoint candidates, online
  flag
- the hub as a peer with AllowedIPs covering every `static` peer's address
- the receiver-side filter rules for this peer (source ipv4 → allowed
  proto/ports)

The client turns every netmap into the complete WireGuard configuration
and applies it (adapters replace the peer set; WireGuard keeps sessions
for unchanged keys), caches it on disk, and installs the filter (spec
006). A peer is online while its `Sync` stream is open and for 90 s
after it drops. Removing a peer on the server propagates to every
client within 5 seconds and closes that peer's stream. The server's
hub interface carries every registered peer as a WireGuard peer, so
clients' keepalives toward the hub keep a tunnel to the server open.
Spec 003.

```mermaid
sequenceDiagram
  participant C as Client
  participant S as Server
  participant W as WireGuard
  C->>S: Sync(current_generation)
  S-->>C: NetMap(gen=42, peers, filter)
  C->>W: set peers, allowed IPs, endpoints
  Note over S: admin deletes peer X (gen=43)
  S-->>C: NetMap(gen=43)
  C->>W: remove peer X
  C->>S: ReportEndpoints(local, stun) every 60s or on change
```

### 4.4 Direct connectivity

Clients learn their endpoints: local interface addresses (outside the
overlay) with the WireGuard listen port, plus the server-reflexive
address from STUN on both server STUN ports, whose `host:port` the
netmap carries. If the two reflexive ports differ, the NAT is
endpoint-dependent and the client reports `symmetric: true`. With
`wireguard-go` the STUN request leaves from the WireGuard socket itself
(a `conn.Bind` wrapper filters the responses out of the receive path),
so the mapping is exact. The kernel module's UDP socket cannot be
shared, so there STUN runs from an ephemeral socket: it yields the
public IP and the symmetric verdict, the reflexive candidate assumes a
port-preserving NAT, and the server adds the exact mapping it observes
on the hub interface (the source address of the peer's WireGuard
packets) as a further reflexive candidate. Reports go out on connect,
when the local address set changes, and otherwise every 60 s.

Every netmap peer starts `idle`: its WireGuard endpoint points at a
loopback sink socket the daemon owns, which never transmits. As soon as
traffic for the peer is queued, WireGuard sends its handshake initiation
to the sink and the daemon knows there is traffic intent (`thawr client
ping <peer>` sets it explicitly). To reach peer B, client A orders B's
candidates (local addresses sharing a /24 with one of A's, other local,
reflexive, hub-stable; ties by address; reflexive skipped when both are
symmetric), removes and re-adds B with the first candidate as endpoint
(the re-add resets WireGuard's 5 s handshake retry timer) and sends one
UDP datagram to B's overlay address through the interface, which makes
WireGuard initiate a handshake there. B does the same toward A with the
identical ordering, so both sides punch holes simultaneously; a
handshake B initiates first is accepted too, and WireGuard's roaming
moves the endpoint to wherever authenticated packets come from. A
candidate is accepted when `last_handshake_time` advances within 2
seconds. If the list is exhausted (10 seconds at most) the path is
`relay` (§4.5), re-probed from there as described below. A
`direct` path with queued traffic but no handshake for 3 minutes is
re-probed. Path states are reported to the server (`ReportPath`) and
shown in the admin UI. No userspace packet multiplexing: the kernel or
`wireguard-go` device is always the one sending. Spec 004.

### 4.5 Relay fallback

```mermaid
sequenceDiagram
  participant WA as WireGuard A
  participant PA as relay proxy A (127.0.0.1:pA)
  participant R as Relay (server)
  participant PB as relay proxy B (127.0.0.1:pB)
  participant WB as WireGuard B
  WA->>PA: UDP WireGuard packet (endpoint = 127.0.0.1:pA)
  PA->>R: frame{dst=B pubkey, payload}
  R->>R: A and B authenticated, mutually visible?
  R->>PB: frame{src=A pubkey, payload}
  PB->>WB: UDP to WireGuard listen port
```

Each client keeps one TLS connection to `/relay` (HTTP/1.1 Upgrade
`thawr-relay/1`, authenticated with the node secret before anything
past the headers is read), opened when the first peer needs the relay
and closed after five minutes without relayed peers; it reconnects with
the same jittered backoff as the sync stream and answers a 30 s ping.
For every relayed peer it opens one local UDP socket on `127.0.0.1` and
points the WireGuard endpoint at it: datagrams WireGuard sends there
become `SEND` frames, `RECV` frames are written from that socket to the
WireGuard port. Frames are `type(1) | key(32) | length(2) | payload`,
at most 1500 payload bytes; only payloads starting with a WireGuard
message type (1–4) are forwarded, on both ends. The relay forwards only
between peers that are mutually visible (`control.KeyVisibility`,
cached per netmap generation), answers `PEER_GONE` at most once per
second per pair, closes a session after ten visibility violations per
minute, keeps a 256-frame queue per session (drop on overflow) and an
optional per-peer byte rate (`relay.max_bytes_per_second`); the
counters appear in `/api/v1/status`. A newer connection for the same
key replaces the older one, and a deleted peer's session is closed with
the next registry change. The relay never sees anything but WireGuard
ciphertext and never logs payload bytes.

The path machine ends an exhausted candidate list in `relay` instead of
`unreachable`. From the relay it retries one candidate every 60 s (the
next in turn), returning to the relay when the 2 s window passes, so a
failed retry costs at most one window of loss; a candidate change (new
netmap on both sides at once) starts a full simultaneous round, which
is what upgrades a relayed pair to `direct` after a network change. A
handshake arriving from a public address while relayed also upgrades
the path (roaming). Spec 005.

### 4.6 Policy evaluation

The policy file is YAML with `groups`, `tagOwners`, and `acls`. Rules
have `action` (only `accept`), `src` selectors, `dst` selectors with
ports, optional `proto`. Selectors: `*`, `user:name`, `group:name`,
`tag:name`, `peer:name`, CIDR. Default deny.

Compilation produces, for every ordered pair (src peer, dst peer), the
set of allowed (proto, port ranges). Two peers are **visible** to each
other when either direction is non-empty. Enforcement:

1. Key distribution: a client only receives keys of visible peers. This
   alone stops any traffic between peers with no relationship.
2. Receiver-side filter: the destination client drops packets from source
   overlay addresses not permitted for that proto/port, installed from
   `netmap.filter` on every map. On Linux with kernel WireGuard this is
   the nftables table `inet thawr` with a drop-policy chain on the input
   hook: `iifname != thawr0 accept`, `ct state established,related
   accept`, ICMP echo from the visible set, one rule per policy entry,
   a counting drop; every change rebuilds the table in one batch. With
   `wireguard-go` it is a userspace filter between the device and the
   TUN: outbound packets record flows (UDP 120 s idle, TCP an hour or
   30 s after FIN/RST, ICMP echo by identifier), inbound packets must
   answer a flow, be an ICMP diagnostic from a visible peer or match a
   rule. Drops are counted and shown in `client status`.
3. Hub-side filter: the server applies the same rules on the forward
   hook of its hub interface for `static` peers (spec 008).

Compilation resolves every rule to source and destination bitsets over
the peer list (`self` is checked per pair by owner) and answers
`Allowed`, `Visible`, `FilterFor` and `MayUseTag`; the server caches it
per (policy hash, persisted generation). Validation against the
registry treats unknown users and groups as errors and unknown tags and
peers as warnings. `tagOwners` is enforced when members create tokens.
The server reloads the policy on `SIGHUP`, on `thawr admin policy
reload` and on `POST /api/v1/policy/reload`; an invalid file never
replaces the running policy and the errors name the rule index and
field; a successful reload bumps the generation so every client gets a
new map and filter within seconds. `thawr admin policy check file.yaml`
validates against the running server without installing; `show` prints
the effective policy with its hash. Spec 006.

### 4.7 Mobile (static) peers

`thawr admin peer add-mobile --owner alice --name alice-phone` generates a
WireGuard keypair in server memory, stores the public key as a `static`
peer, renders a standard WireGuard `.conf` with `Endpoint =
public_addr:51820`, `AllowedIPs = <overlay cidr>`, and shows it once as
a QR code (terminal or admin UI). The private key is discarded after
rendering. The hub forwards between the phone and mesh peers; the
threat model records that the server sees plaintext for static peers
only. Spec 008.

## 5. Wire interfaces

### gRPC `thawr.v1.Control`

| RPC | Auth | Purpose |
|---|---|---|
| `Enroll(EnrollRequest) EnrollResponse` | enrollment token | Register a new agent peer |
| `Sync(SyncRequest) stream NetMap` | node secret | Long-lived netmap push |
| `ReportEndpoints(EndpointReport) Empty` | node secret | Candidates + NAT type |
| `ReportPath(PathReport) Empty` | node secret | direct/relay state per peer (for status and admin UI) |
| `RotateKey(RotateKeyRequest) RotateKeyResponse` | node secret | Replace the WireGuard key; old key valid until next generation is acknowledged |
| `Leave(Empty) Empty` | node secret | Peer removes itself |

Node secret travels as gRPC metadata `authorization: Bearer <secret>`.
Every message carries `client_version`; the server rejects versions older
than its `min_client_version`.

### REST `/api/v1` (admin)

`POST /login`, `POST /logout`, `GET /me`, `GET /status`, `GET|POST /users`,
`GET|POST /tokens`, `DELETE /tokens/{id}`, `GET /peers`,
`GET|PATCH|DELETE /peers/{name}`, `POST /peers/mobile`, `GET /policy`,
`POST /policy/check`, `POST /policy/reload`. JSON bodies. Browser
sessions are in memory (12 h) behind one `HttpOnly`, `Secure`,
`SameSite=Strict` cookie; the session's CSRF token is returned by
`/login` and `/me` and must be sent as `X-CSRF-Token` on mutating calls.
The Unix socket exposes the same API without login as the local admin;
access is filesystem permission (`root` and group `thawr`).

### Client local API

`thawr client status` talks to the running daemon over
`/var/run/thawr/client.sock` (a Unix socket on every platform; Windows
supports `AF_UNIX`) with a tiny JSON-over-HTTP API: `GET /status`,
`POST /down`, `POST /rotate-key`, and `POST /ping/{name}` (mark traffic
intent, probe, answer with the settled path). Spec 007 formats the
output.

## 6. Storage

SQLite, WAL mode, single writer. Tables: `users`, `peers`,
`enrollment_tokens`, `meta` (netmap generation, schema version, server
key fingerprint). Endpoints, relay sessions and path state are ephemeral
and kept in memory in `control.EndpointTable`; a restart simply waits for
clients to re-report. Migrations are `NNNN_name.sql` files embedded and
applied in a transaction with the version recorded in `meta`.

Indexes: `peers(public_key)` unique, `peers(name)` unique,
`peers(owner_id)`, `enrollment_tokens(secret_hash)` unique,
`enrollment_tokens(expires_at)`.

## 7. Client state

| Path | Content | Mode |
|---|---|---|
| `/var/lib/thawr/client/node.key` | WireGuard private key | 0600 |
| `/var/lib/thawr/client/state.json` | server URL, TLS fingerprint, peer id, node secret, listen port | 0600 |
| `/var/lib/thawr/client/netmap.json` | last netmap (public keys, addresses, endpoints) | 0600 |

macOS: `/Library/Application Support/Thawr/`. Windows: `%ProgramData%\Thawr\`.
The cached netmap lets the client restore WireGuard peers before the
server is reachable, so a mesh keeps working through a server outage as
long as endpoints have not changed.

## 8. External dependencies

| Module | Use | License |
|---|---|---|
| `golang.zx2c4.com/wireguard` (wireguard-go) | Userspace WireGuard, TUN | MIT |
| `golang.zx2c4.com/wireguard/wgctrl` | Configure kernel and userspace WireGuard | MIT |
| `golang.zx2c4.com/wireguard/windows` | Windows TUN (wintun) helpers | MIT; wintun.dll under its Prebuilt Binaries License |
| `modernc.org/sqlite` | SQLite, pure Go | BSD-3-Clause |
| `google.golang.org/grpc`, `google.golang.org/protobuf` | Control channel | Apache-2.0, BSD-3-Clause |
| `gopkg.in/yaml.v3` | Config and policy | MIT and Apache-2.0 |
| `golang.org/x/crypto` | argon2id password hashing | BSD-3-Clause |
| `golang.org/x/net`, `golang.org/x/sys` | Routing tables, syscalls | BSD-3-Clause |
| `github.com/vishvananda/netlink` | Linux addresses and routes | Apache-2.0 |
| `github.com/google/nftables` | Linux receiver-side filter | Apache-2.0 |
| `tailscale.com/net/stun` (copied into `internal/stun` with license header) | STUN client and server codec | BSD-3-Clause |
| `github.com/skip2/go-qrcode` | QR export | MIT |
| `github.com/spf13/cobra` | CLI subcommands | Apache-2.0 |
| `golang.org/x/term` | Password prompt without echo | BSD-3-Clause |
| `github.com/bufbuild/buf` (build time only, via `go run`) | Protobuf compilation in `make proto` | Apache-2.0 |

All pure Go. The binary builds with `CGO_ENABLED=0` on Linux, macOS, and
Windows with Go 1.26 or newer (the minimum required by the gRPC and
`golang.org/x` releases in use).

## 9. Non-goals restated

No own cryptography, no Layer 2, no hosted control plane, no native
mobile apps in v1, no DNS (phase 2), no exit nodes or subnet routers
(phase 2), no IPv6 overlay (phase 2; schema reserves `ipv6`).
