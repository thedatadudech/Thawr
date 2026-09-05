<div align="center">

![Thawr](docs/assets/logo-dark.svg#gh-dark-mode-only)
![Thawr](docs/assets/logo-light.svg#gh-light-mode-only)

</div>

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
- A YAML policy file you keep in git. Default deny: peers without a
  rule cannot see each other, and the receiving peer filters ports
  (nftables with kernel WireGuard, a userspace filter otherwise).
  `thawr admin policy check`, `reload` and `show` manage it live.
- Phones use the official WireGuard app via a QR code.
- Nothing phones home. It starts and runs with no internet access.

## Install

Releases are built by CI from a tag, reproducibly, and ship with
checksums. Download the archive for your platform from the GitHub
Releases page, verify it, and put the binary on the path:

```
curl -LO https://github.com/thedatadudech/Thawr/releases/download/v0.1.0/thawr_v0.1.0_linux_amd64.tar.gz
curl -LO https://github.com/thedatadudech/Thawr/releases/download/v0.1.0/SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
tar xzf thawr_v0.1.0_linux_amd64.tar.gz
sudo install -m 0755 thawr_v0.1.0_linux_amd64/thawr /usr/local/bin/thawr
thawr version
```

macOS: the `darwin_arm64` (Apple silicon) or `darwin_amd64` archive,
`shasum -a 256 -c`. Windows: the `.zip`, `Get-FileHash`. Each release
also carries `thawr.rb`, a Homebrew formula for your own tap. The
`thawr` binary is the same on every machine; what differs is the
subcommand you install.

## Quick start

Server, on a host with a public address (Linux with systemd, or macOS):

```
sudo thawr server install --public-addr vpn.example.com
thawr admin user create markus --role admin
thawr admin token create --owner markus --kind human
```

`server install` writes `/etc/thawr/server.yaml` with that one line,
validates it, and registers `thawr-server` to start at boot
(`journalctl -u thawr-server -f` follows the log). Without a service
manager, `thawr server --config /etc/thawr/server.yaml` runs it in the
foreground.

Client, on any machine:

```
sudo thawr client install --server https://vpn.example.com --token thawr_... --fingerprint sha256:...
thawr client status
thawr client ping <peer>
```

`client install` enrols the device with the one-time token, then
registers `thawr-client` (systemd, launchd or a Windows service) to run
`thawr client up` at boot; the token never reaches the service file.
Run it on every machine except the server: the server host is already
a peer (the hub), and `client install` refuses to run there.
`sudo thawr client up --server ... --token ...` does the same in the
foreground. `client up` enrols the device on first run and then keeps
the WireGuard interface in sync with the server until stopped. The
client's socket is readable by root and the `thawr` group; create the
group and add yourself (`groupadd thawr && usermod -aG thawr $USER`;
macOS: `dseditgroup -o create thawr && dseditgroup -o edit -a $USER -t
user thawr`) to run `client status` without sudo. `client status` shows
in one table whether the control connection, the path to a peer or the
policy is the problem (`--json` for scripts, validated by
`docs/status.schema.json`; exit codes 0 connected, 1 server unreachable,
2 usage, 3 not running):

```
thawr 0.1.0 · alice-laptop 100.64.0.7 · server vpn.example.com:8443 connected (netmap #42, 3s ago)
WireGuard: kernel · thawr0 · listen 41820 · NAT: cone (reflexive 203.0.113.9:41820)

PEER          IP            KIND     OWNER   PATH                           HANDSHAKE   RX / TX
homelab-nas   100.64.0.3    server   -       direct 198.51.100.4:51820      12s         1.2 MB / 340 kB
build-box     100.64.0.9    agent    -       relay                          3m          0 B / 0 B
hub           100.64.0.1    server   -       direct vpn.example.com:51820   25s         4 kB / 4.2 kB

Filter: 3 rules · 0 dropped (last 5 min)
```

Phones join with the official WireGuard app: `thawr admin peer add-mobile
--owner markus --name markus-phone` prints a QR code to scan (once; the
server keeps only the public key). Phone traffic goes through the
server's hub, so the server can read it, unlike the end-to-end tunnels
between laptops and servers; see the threat model.

Peers find each other through STUN and WireGuard hole punching:
same-LAN addresses first, then the public address behind the router;
`client ping` forces the probe and shows the path in use along with the
echo replies. `thawr admin peer list` and `admin peer show <name>` give
the admin the same view across the whole network, including each
peer's candidates and compiled filter. When no direct path exists (two symmetric NATs, a strict
firewall) the packets go through the relay built into the server, still
end-to-end encrypted, and the path upgrades to `direct` on its own when
the network allows it.

Ports on the server: TCP 443 (control, UI, relay), UDP 3478–3479
(STUN), UDP 51820 (WireGuard hub for phones).

## Upgrading

Replace the binary and restart the service:

```
sudo install -m 0755 thawr /usr/local/bin/thawr
sudo systemctl restart thawr-server        # or thawr-client
sudo launchctl kickstart -k system/thawr-client   # macOS
```

The server applies pending database migrations at start; clients keep
their enrollment. A server can refuse clients older than
`min_client_version` in its config; `thawr client status` shows the
server's version and says `client update available` when the server is
ahead. Nothing checks for updates over the internet: the only version
Thawr ever compares against is your own server's.

`thawr server uninstall` and `thawr client uninstall` remove the
service and keep the data; add `--purge --yes` to delete it.

## Building

Requires Go 1.26 or newer. No CGO, no Node.

```
make build      # bin/thawr
make test
make lint       # gofmt, go vet, golangci-lint
make integration  # Linux with CAP_NET_ADMIN
make release VERSION=v0.1.0   # dist/: archives, SHA256SUMS, thawr.rb
make release-verify           # two builds, identical archives
```

Releases are published by CI: push a tag `vX.Y.Z`, or start the
`release` workflow by hand in the Actions tab with the version as input
and it creates the tag on `main` for you. Tags containing a `-`
(`v0.1.0-rc1`) become pre-releases.

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
