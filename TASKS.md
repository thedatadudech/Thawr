# Thawr — Backlog

Status legend: `[ ]` open, `[~]` in progress, `[x]` done. One spec per
session; mark it here and add one line per non-obvious decision below the
entry.

## Phase 0 — Documents

- [x] CLAUDE.md, docs/VISION.md, docs/ARCHITECTURE.md, ADRs 0001–0006,
      docs/THREAT_MODEL.md, specs 001–008, TASKS.md (this file)

## Phase 1 — Scaffold

- [ ] `go.mod`, `Makefile`, `.gitignore`, `.golangci.yml`, `README.md`,
      `LICENSE` (Apache-2.0, replacing the initial MIT file), `NOTICE`,
      `.github/workflows/ci.yml` (build, lint, test on Linux, macOS,
      Windows), `config/server.example.yaml`, `doc.go` in every package.
      `make build lint test` green. Commit `chore: initial scaffold`.

## Sprint 1 — control plane core

- [ ] **001 Server bootstrap** — `docs/specs/001-server-bootstrap.md`
      Config load + defaults + validation, SQLite + migrations, key and
      TLS generation, hub interface, listeners, `--check`, clean
      shutdown, netns boot test.
- [ ] **002 Peer enrollment** — `docs/specs/002-peer-enrollment.md`
      Users (argon2id), one-time tokens, `Enroll` RPC, IP allocator,
      peer registry, admin CLI + REST + UI list/create, `client up`
      with fingerprint pinning.
- [ ] **003 Key distribution** — `docs/specs/003-key-distribution.md`
      Netmap builder, `Sync` stream, endpoint table, `wg.Device`
      kernel + userspace adapters, client daemon with cached netmap,
      canonical integration test `TestEncryptedPingTwoClients`.

## Sprint 2 — connectivity, policy, UX

- [ ] **004 Direct connectivity** — `docs/specs/004-direct-connectivity.md`
      STUN server + client, candidate ordering, path state machine, hole
      punching via WireGuard handshakes, NAT netns tests.
- [ ] **005 Relay fallback** — `docs/specs/005-relay-fallback.md`
      Frame protocol, relay server with visibility check, client proxy
      sockets, relay→direct upgrade.
- [ ] **006 ACL policy** — `docs/specs/006-acl-policy.md`
      Policy parse/validate/compile, visibility, nftables filter,
      userspace filter, hub-side filter, `admin policy` commands.
- [ ] **007 CLI status** — `docs/specs/007-cli-status.md`
      Local daemon API, `client status` text/JSON/watch, `client ping`,
      `admin peer list/show`, exit codes.
- [ ] **008 Mobile QR export** — `docs/specs/008-mobile-qr-export.md`
      Static peers, hub routing and forwarding, QR rendering in CLI and
      UI, one-time private key handling.

## Phase 2 candidates (not scheduled)

- Signed netmaps and client-side key pinning (threat model T4).
- OIDC identity provider plugin (ADR 0006).
- DNS names for peers (`<name>.thawr`).
- IPv6 overlay.
- Exit nodes and subnet routers.
- Separate relay nodes (`thawr relay`).
- ACME TLS mode, Prometheus metrics, `thawr admin backup`.
- Workload / agent identity: short-lived tokens issued by CI or an
  orchestrator, using the existing `kind: agent`.

## Open decisions awaiting owner review

Answers change the docs above; none block Phase 1 except D1.

- D1 License file: repository has MIT; brief and ADR 0003 say
  Apache-2.0. Phase 1 replaces `LICENSE` unless told otherwise.
- D2 STUN: copy `tailscale.com/net/stun` into `internal/stun`
  (BSD-3, ~400 lines, no transitive deps) rather than importing the
  `tailscale.com` module (large dependency tree). Alternative:
  `github.com/pion/stun` (MIT).
- D3 CLI framework: `github.com/spf13/cobra` for nested subcommands and
  help. Alternative: stdlib `flag` with a hand-written dispatcher.
- D4 Enforcement of port-level policy on the receiver (nftables with
  kernel WireGuard, userspace filter with `wireguard-go`) instead of a
  userspace packet multiplexer in front of WireGuard. Cheaper to build,
  keeps the WireGuard device as the only packet sender.
- D5 Mobile peers are routed through the server's WireGuard hub, which
  means the server sees their plaintext (threat model T4). The
  alternative (no phones in v1) contradicts the brief.
- D6 Relay transport is TLS over the HTTPS port with an HTTP Upgrade,
  not a separate port, so users open exactly one TCP and two/three UDP
  ports.
- D7 Control-channel client auth is a bearer node secret over pinned
  TLS, not mTLS or a second signing key; simpler, and the WireGuard key
  stays the only long-lived identity on the device.
