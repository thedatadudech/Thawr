# Thawr — Backlog

Status legend: `[ ]` open, `[~]` in progress, `[x]` done. One spec per
session; mark it here and add one line per non-obvious decision below the
entry.

## Phase 0 — Documents

- [x] CLAUDE.md, docs/VISION.md, docs/ARCHITECTURE.md, ADRs 0001–0006,
      docs/THREAT_MODEL.md, specs 001–008, TASKS.md (this file)

## Phase 1 — Scaffold

- [x] `go.mod`, `Makefile`, `.gitignore`, `.golangci.yml`, `README.md`,
      `LICENSE` (Apache-2.0, replacing the initial MIT file), `NOTICE`,
      `.github/workflows/ci.yml` (build, lint, test on Linux, macOS,
      Windows), `config/server.example.yaml`, `doc.go` in every package.
      `make build lint test` green. Commit `chore: initial scaffold`.
      - `make test` runs with `CGO_ENABLED=1` because the race runtime
        needs cgo; `make build` and CI cross-builds stay `CGO_ENABLED=0`.
      - `cmd/thawr` already wires cobra with `server`, `client`, `admin`
        and `version`; the first three return "not implemented" until
        their specs land.
- [x] `.github/workflows/ci.yml`: lint, race tests on three OSes,
      CGO-free cross-builds, govulncheck.

## Sprint 1 — control plane core

- [x] **001 Server bootstrap** — `docs/specs/001-server-bootstrap.md`
      Config load + defaults + validation, SQLite + migrations, key and
      TLS generation, hub interface, listeners, `--check`, clean
      shutdown, netns boot test.
      - New package `internal/server` holds the bootstrap; `cmd/thawr`
        only parses flags and signals.
      - Userspace WireGuard is configured through wireguard-go's
        in-process IPC (`IpcSet`), not a UAPI socket; the kernel adapter
        uses wgctrl. Windows returns `ErrPlatformUnsupported` until 003.
      - Policy loading is syntax-only (version 1, accept rules, known
        keys); spec 006 extends `policy.Load` in place.
      - STUN sockets are bound and drained; `/relay` answers 501.
      - Store tests use a temp-file DB, not the in-memory DSN.
      - `go.mod` requires Go 1.25 (pulled in by modernc sqlite and
        wireguard-go); CI uses `go-version-file`, so it follows.
      - `TestMigrateFromV1` waits for a second migration to exist.
      - The netns integration test skips without root and iproute2; it
        did not run in the development container (no `ip` binary). The
        real binary was booted manually there with userspace WireGuard
        and verified: files and modes, status over the socket, SIGTERM
        exit 0.
- [x] **002 Peer enrollment** — `docs/specs/002-peer-enrollment.md`
      Users (argon2id), one-time tokens, `Enroll` RPC, IP allocator,
      peer registry, admin CLI + REST + UI list/create, `client up`
      with fingerprint pinning.
      - New package `internal/client` (state dir, pinning, Enroll);
        `cmd/thawr` stays flags only.
      - gRPC and REST share the HTTPS listener via `api.Combine`
        (`grpc.Server.ServeHTTP` on HTTP/2 `application/grpc`).
      - Protobuf generated with buf and the Go plugins through `go run`
        at pinned versions; generated code is committed.
      - Browser sessions are in memory (12 h); the CSRF token travels in
        the login and `/me` responses, not a cookie, so the only cookie
        is HttpOnly. The admin socket acts as the local admin.
      - Login limiter: 10 failures per user per 15 min, then exponential
        backoff; Enroll: 10 attempts per minute per remote IP.
      - `client up` probes the server certificate and compares it with
        `--fingerprint` before sending anything; `--accept-fingerprint`
        is the explicit trust-on-first-use path. It exits after
        enrolling; the daemon is spec 003.
      - `min_client_version` compares MAJOR.MINOR; non-numeric versions
        (`dev`, git describe) are accepted.
      - REST addresses peers by name (`/peers/{name}`) because names are
        unique and what the CLI uses.
      - `go.mod` is on Go 1.26 (pulled in by grpc / golang.org/x);
        golangci-lint moved to v2.13.2 for the same reason.
      - The two-client netns integration test skips without root and
        iproute2 (not run in the development container); the CLI flow
        was exercised manually against the real binary instead.
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

## Decisions reviewed by the owner (2026-09-02: all accepted as written)

- D1 License: Apache-2.0 confirmed; Phase 1 replaced the initial MIT
  `LICENSE` with the canonical Apache-2.0 text.
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
