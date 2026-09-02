# Spec 001 — Server bootstrap

Sprint 1. Depends on: nothing. Packages: `internal/config`,
`internal/store`, `internal/wg`, `internal/api` (skeleton), `cmd/thawr`.

## Goal

`thawr server --config server.yaml` starts a fully initialised control
plane from a config file that contains only `public_addr`, creates all
state under `data_dir` on first run, and reuses it on every later run.

## User story

As a homelab owner, I copy one binary to my VPS, write a two-line YAML
file, run `thawr server`, and have a server that is ready to enrol peers,
with no other software installed and no internet access required.

## Config

```yaml
public_addr: vpn.example.com   # required; host or host:port reachable by peers
data_dir: /var/lib/thawr       # default shown
```

Full schema with defaults (all optional except `public_addr`):

```yaml
public_addr: vpn.example.com
data_dir: /var/lib/thawr
listen:
  https: ":443"           # gRPC + REST + UI + relay
  stun: [":3478", ":3479"]
  wireguard: ":51820"
overlay:
  cidr: 100.64.0.0/10
  interface: thawr0
tls:
  mode: self-signed       # self-signed | file
  cert_file: ""           # with mode=file
  key_file: ""
policy_file: /etc/thawr/policy.yaml
admin_socket: /var/lib/thawr/admin.sock
log:
  level: info             # debug | info | warn | error
  format: text            # text | json
min_client_version: ""    # default: server's major.minor
```

Env overrides: `THAWR_CONFIG` (path), `THAWR_LOG_LEVEL`. No other env
vars in v1.

## Behaviour

1. Parse flags, load YAML, apply defaults, validate. On any validation
   error print every error (not just the first) and exit 2.
2. Create `data_dir` with mode 0700 if missing. Refuse to start if it
   exists with group- or world-writable permissions.
3. Open `data_dir/thawr.db` (WAL), apply pending migrations in one
   transaction, record schema version in `meta`.
4. Load or generate `server.key` (WireGuard private key, 0600). Store
   the public key fingerprint in `meta`; refuse to start if the key on
   disk does not match the fingerprint in the DB (prevents mixing a DB
   and a key from different servers).
5. TLS: with `self-signed`, load `tls/cert.pem` and `tls/key.pem` or
   generate an ECDSA P-256 certificate valid 10 years with SAN = host
   part of `public_addr`. With `file`, load the given files and fail if
   unreadable.
6. Bring up the WireGuard hub interface with the first address of
   `overlay.cidr` (`100.64.0.1/10`), listen port from
   `listen.wireguard`. Kernel module if available, else `wireguard-go`.
7. Load `policy_file` if it exists; an absent file means empty policy
   (default deny) and a warning. An invalid file is a fatal error at
   startup.
8. Start STUN listeners, relay, gRPC, REST, admin socket (0660,
   group `thawr` if the group exists, else owner only).
9. Log one `info` line per component with its address, then one line
   `server ready` with the TLS fingerprint `sha256:<hex>` and the hub
   public key.
10. On `SIGINT`/`SIGTERM`: stop listeners, close the WireGuard interface,
    close the DB, exit 0 within 5 seconds. On `SIGHUP`: reload policy
    only.

`thawr server --check --config server.yaml` validates config, TLS files
and policy and exits without starting anything.

## Acceptance criteria

- [ ] A config containing only `public_addr` starts a server; every
      default in the table above is applied.
- [ ] First start creates `thawr.db`, `server.key` (0600), `tls/cert.pem`,
      `tls/key.pem` (0600), `admin.sock`; second start reuses all of
      them and logs no "generated" messages.
- [ ] Missing `public_addr`, invalid CIDR, unparsable listen address, and
      non-existent TLS files each produce a clear error naming the key.
- [ ] `--check` exits 0 on a valid config and 2 on an invalid one without
      touching `data_dir`.
- [ ] `GET /api/v1/status` over the admin socket returns JSON with
      `version`, `uptime_seconds`, `peer_count`, `netmap_generation`,
      `tls_fingerprint`, `hub_public_key`.
- [ ] `SIGTERM` shuts down cleanly within 5 s; the WireGuard interface is
      gone afterwards.
- [ ] Startup with the internet unreachable succeeds (no outbound
      connection is attempted; verified by the integration test running
      in an isolated netns).
- [ ] No secret material in logs at any level, including `debug`.

## Test cases

Unit (`internal/config`):
- `TestDefaults`: minimal YAML → all defaults.
- `TestValidate`: table of invalid inputs → expected error strings.
- `TestEnvOverride`: `THAWR_LOG_LEVEL=debug` wins over file.

Unit (`internal/store`):
- `TestMigrateFresh`, `TestMigrateIdempotent`, `TestMigrateFromV1`
  (fixture DB at each released schema).
- `TestMetaRoundTrip`.

Unit (`cmd/thawr` or `internal/server`):
- `TestBootstrapCreatesFiles` with a temp `data_dir` and a fake
  `wg.Device`; asserts files and modes.
- `TestKeyMismatchRefused`.
- `TestSelfSignedCert`: SAN, validity, ECDSA.
- `TestNoSecretsInLogs`.

Integration (`tests/`):
- `TestServerBootsInNetns`: start the server in a netns without a default
  route, wait for `server ready`, query status over the socket, send
  `SIGTERM`, assert exit 0 and interface removal.

## Out of scope

- Users and login (spec 002 creates the first admin user).
- ACME / Let's Encrypt (phase 2; `tls.mode: acme`).
- Metrics endpoint, systemd unit generation (phase 2).
- Clustering, multiple servers.
