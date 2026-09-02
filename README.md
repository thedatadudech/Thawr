# Thawr

**One binary. No cloud. Works offline.**

Thawr is a self-hosted, WireGuard-based private network (mesh VPN / ZTNA)
that connects laptops, servers, phones and home machines across the
internet as if they were on one LAN. The name comes from the cave near
Mecca where the Prophet Muhammad ﷺ and Abu Bakr found shelter during the
Hijra: hidden, protected, reachable only by those who belong.

Status: pre-alpha. The design is complete (see `docs/`); the
implementation follows the specs in `docs/specs/` one at a time and is
tracked in `TASKS.md`.

## What you get

- A single `thawr` binary that runs as the control server, as the client
  on every device, and as the admin CLI.
- WireGuard for the data plane, kernel module where available,
  `wireguard-go` otherwise. Thawr never implements cryptography.
- Direct peer-to-peer connections through NAT, with a relay built into
  the server as fallback. Traffic between peers is end-to-end encrypted;
  the relay forwards opaque packets.
- SQLite embedded; the whole server state is one directory.
- Local users and one-time enrollment tokens. No identity provider
  required. OIDC optional.
- A YAML policy file you keep in git. Default deny.
- Phones use the official WireGuard app via a QR code.
- Nothing phones home. It starts and runs with no internet access.

## Quick start (target UX; not functional until Sprint 1 is done)

Server, on a host with a public address:

```yaml
# /etc/thawr/server.yaml
public_addr: vpn.example.com
```

```
thawr server --config /etc/thawr/server.yaml
thawr admin user create markus --role admin
thawr admin token create --owner markus --kind human
```

Client, on any machine:

```
thawr client up --server https://vpn.example.com --token thawr_... --fingerprint sha256:...
thawr client status
```

Ports on the server: TCP 443 (control, UI, relay), UDP 3478–3479
(STUN), UDP 51820 (WireGuard hub for phones).

## Building

Requires Go 1.25 or newer. No CGO, no Node.

```
make build      # bin/thawr
make test
make lint       # gofmt, go vet, golangci-lint
make integration  # Linux with CAP_NET_ADMIN
```

## Documentation

| Document | Content |
|---|---|
| `docs/VISION.md` | Problem, users, non-goals, comparison with Tailscale, NetBird, Headscale |
| `docs/ARCHITECTURE.md` | Components, packages, data flows, wire interfaces, dependencies |
| `docs/adr/` | Architecture decision records |
| `docs/THREAT_MODEL.md` | Assets, attackers, mitigations, out of scope |
| `docs/specs/` | One spec per feature with acceptance criteria and tests |
| `TASKS.md` | Backlog and status |
| `CLAUDE.md` | Working agreement for AI-assisted sessions |

## Contributing

Semantic commits (`feat:`, `fix:`, `refactor:`, `docs:`, `chore:`,
`test:`), one logical change per commit, `make test lint` clean.
Contributions are accepted under the Developer Certificate of Origin.
Pull requests that add cryptographic code are declined on principle (ADR
0004).

## License

Apache License 2.0. See `LICENSE` and `NOTICE`.
