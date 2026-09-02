# ADR 0005: One binary with server, client and admin subcommands

Status: Accepted
Date: 2026-09-02

## Context

Self-hosted alternatives ship as several containers (control plane,
identity provider, relay, database, reverse proxy). Each extra process is
another thing to install, upgrade, monitor and secure. The target users
run one VPS or one box in a closet. Version skew between server and
client is a recurring support problem in mesh VPN projects.

## Decision

A single executable `thawr` containing:

- `thawr server`: control plane, STUN, relay, WireGuard hub, REST API
  and the admin UI embedded with `embed.FS` (plain HTML/JS, no build step).
- `thawr client`: the node agent and its local CLI (`up`, `down`,
  `status`).
- `thawr admin`: management CLI talking to the server via Unix socket or
  REST.

Server and client are always released together with one version number.
The server refuses clients older than `min_client_version` (default: same
major.minor).

## Consequences

- Installation is `curl` + `chmod +x` + one YAML line. Upgrades replace
  one file.
- Binary size grows with the embedded UI and `wireguard-go`; around
  20–30 MB is accepted.
- The admin UI cannot use a JavaScript build pipeline; it stays small and
  server-rendered-JSON driven. This is a deliberate constraint.
- The relay runs in the same process as the control plane. Separate relay
  nodes (for geographic spread) are a phase 2 option that would reuse the
  same binary with `thawr relay`.
- Client and server share packages; an accidental import of server-only
  code into the client path must be caught by review (`cmd/thawr` wires
  each subcommand explicitly).
