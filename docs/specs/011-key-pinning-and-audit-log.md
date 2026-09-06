# Spec 011 — Key pinning and audit log

Sprint 3. Depends on: 002 (enrollment), 003 (netmap sync), 007 (status
document, local API), 008 (static peers). Packages: `internal/client`
(pins, `trust`), `internal/store` (`audit_log`), `internal/control`
(auditor on every mutation), `internal/api` (REST `audit`),
`internal/config` (`audit` key), `internal/server` (retention),
`cmd/thawr` (`client trust`, `admin audit`, status), `web/`.

## Goal

Two of the three phase-2 mitigations for threat T4 (compromised server)
and the persistence T5 relies on. A client that has once seen a peer's
WireGuard key, or the hub's, refuses to silently accept a different one:
the peer is held out of the tunnel until the person at the keyboard
accepts the change. Every control-plane mutation on the server leaves a
row in an audit table an admin can list, with who did it and to what.
Signed peer records (an admin-held offline key) are spec 012; this spec
makes a key swap visible and a server compromise reconstructible, it
does not yet make the swap impossible.

## User story

As a user I rotate the key on my laptop. My desktop shows the laptop as
`key changed` and does not talk to it until I run `thawr client trust
laptop` there; after that the path comes back. If my server were
compromised and handed my desktop a stranger's key under my laptop's
name, the same hold would stop the traffic instead of encrypting it to
the stranger.

As an admin I run `thawr admin audit --since 24h` after an incident and
see every token, enrolment, rename, deletion, key rotation, policy
reload and login of the last day, with the user or peer behind each.

## Commands

```
thawr client trust <name>...        # accept the offered key of held peers (hub is "hub")
thawr client trust --all            # accept every held key
thawr client status                 # held peers show PATH "key changed"; the header says how to trust

thawr admin audit [--since 24h] [--limit 100] [--action peer.rename] [--actor alice] [--json]
```

Server config (`server.yaml`):

```yaml
audit:
  retention_days: 90     # rows older than this are pruned daily; 0 keeps everything
```

`client status` header with a held peer:

```
thawr 0.1.0 · desktop 100.64.0.3 · server vpn.example.com:8443 connected (netmap #42, 3s ago)
WireGuard: kernel · thawr0 · listen 41820 · NAT: cone (reflexive 203.0.113.9:41820) · DNS: .thawr via resolved · 1 key changed: thawr client trust laptop
```

## Behaviour

### Pins (client)

- The client pins the hub's public key and, for every agent (non
  `via_hub`) peer in the netmap, the pair `(peer id, public key)`
  under the peer's **name**. Static peers have no WireGuard peer on the
  client and are not pinned. Pinning is always on.
- Pins live in `<state dir>/pins.json` (mode 0600, written through a
  temporary file and renamed):
  `{"hub": "<key>", "peers": {"<name>": {"id": "...", "key": "..."}}}`.
  A file that cannot be read or parsed is a start-up error; it is never
  reset silently. `client down --forget` removes it with the rest of
  the state.
- On every netmap, before anything is applied, each entry is checked:
  - name unknown → pin `(id, key)`. First contact is trusted, as the
    enrolment already trusts the server it talks to.
  - name known, same id, same key → pass.
  - id known under a different name (admin rename) → the pin moves to
    the new name, pass.
  - name known, same id, different key → **held** (a rotation, or a
    swapped key).
  - name known, different id → **held** (a re-enrolled or substituted
    peer took the name).
  - hub key different from the pinned one → the **hub is held**.
- A held peer is removed from the netmap the client applies: no
  WireGuard peer, no filter rule, no `<name>.thawr`, no path probing.
  A held hub means no hub WireGuard peer and no route through it;
  direct mesh paths keep working and the TLS-pinned control connection
  is unaffected. Pins are never pruned by absence: a peer that vanishes
  from one netmap and returns with a new key is still held.
- `thawr client trust <name>` (local API `POST /trust/{name}`, `all`
  for every held entry) replaces the pin with the offered `(id, key)`
  and re-applies the last netmap. A name that is not held answers
  404 ("nothing to trust"). Accepting is logged with both fingerprints.
- Status: `held[]{name, ipv4, pinned_key, offered_key, since}` (the
  hub as `hub`), and the held peer's row in `peers` (or `hub`) carries
  `path: "key_changed"`. The header appends ` · N key(s) changed: thawr
  client trust <names>`. Exit codes are unchanged.
- After `thawr client rotate-key` on a device, every other device holds
  it until it runs `trust`; the rotating client prints that reminder.
  Spec 012 (admin-signed peer records) removes this step for signed
  rotations.

### Audit log (server)

- Table `audit_log(id, at, actor, actor_role, action, target,
  details)` in migration `0003_audit_log.sql`; `details` is a JSON
  object of strings. Rows are appended in the same transaction as the
  mutation they record where one exists; a failed append fails the
  mutation.
- Actors: a UI user by name and role; the admin socket as `local` /
  `admin`; a peer acting for itself as `peer:<name>` / `peer`; a failed
  login as the attempted name / `anonymous`.
- Actions and targets:

  | action | target | details |
  |---|---|---|
  | `token.create` | token id | owner, kind, tags, expires_at |
  | `token.revoke` | token id | owner |
  | `peer.enrol` | peer id | name, kind, token, key (fingerprint) |
  | `peer.create_static` | peer id | name, owner, key (fingerprint) |
  | `peer.rename` | peer id | from, to |
  | `peer.delete` | peer id | name |
  | `peer.leave` | peer id | name |
  | `peer.rotate_key` | peer id | name, key (fingerprint), generation |
  | `user.create` | user id | name, role |
  | `login.ok` / `login.failed` | user name | – |
  | `policy.reload` | policy path | hash, rules |

  Details never contain a secret, a full key or a password hash.
- `audit.retention_days` (default 90, 0 = forever) prunes rows older
  than that at start and every 24 hours.
- Reading: `GET /api/v1/audit?since=&before_id=&action=&actor=&limit=`
  (admin only; `since` is RFC 3339 or a duration; `limit` 1–1000,
  default 100; newest first; `before_id` pages backwards), `thawr admin
  audit` over the socket with the same filters, and an "Audit log"
  section in the admin UI (admins only, newest 100, "Load older").

## Acceptance criteria

1. Two clients see each other. `rotate-key` on B: within one netmap A
   shows B `key_changed` in `status --json`, `held[0].name == "b"`, B
   is absent from A's WireGuard peers and `b.thawr` does not resolve;
   `ping b` reports the peer unknown. `client trust b` on A restores
   the WireGuard peer and the path.
2. A netmap with a new peer id under a known name holds the peer; a
   netmap with a known id under a new name passes and `pins.json`
   carries the new name.
3. A netmap with a different hub key leaves the device without a hub
   peer; `status` shows the hub `key_changed`; `trust hub` restores it.
4. A corrupt `pins.json` makes `client up` fail with the file name in
   the error.
5. Every action in the table above produces exactly one row with the
   expected actor and target; a failed audit append rolls back the
   mutation (tested by closing the store inside the transaction).
6. `GET /api/v1/audit` answers 403 to a member, 401 without a session,
   and to an admin the newest rows first honouring `limit`, `since`,
   `action`, `actor` and `before_id`.
7. `thawr admin audit` prints `TIME ACTOR ACTION TARGET DETAILS` and
   with `--json` the array the REST endpoint returns.
8. With `retention_days: 1` rows older than a day are gone after the
   pruner ran (injected clock); with `0` nothing is pruned.

## Test cases

- `internal/client`: `pins_test.go` (each rule above, file round
  trip, corrupt file), `daemon_test.go` `TestDaemonHoldsChangedKey`
  and `TestDaemonHoldsChangedHubKey` against the fake device,
  `status_test.go` schema check with `held`.
- `internal/store`: `audit_test.go` (append, list order, filters,
  cursor, prune, details round trip).
- `internal/control`: existing mutation tests assert their audit row.
- `internal/api`: `rest_test.go` audit endpoint authorisation and
  filters.
- `cmd/thawr`: `client trust` output, status header and table with a
  held peer, `admin audit` table and JSON.
- `tests/pinning_test.go` (integration, Linux): rotate on one netns
  client, hold and trust on the other, audit row on the server.

## Out of scope

- Signed peer records / network lock (spec 012).
- Tamper-evident chaining of the audit log or forwarding to syslog,
  webhooks or a SIEM.
- Audit visibility for members (their own actions).
- Pinning for static peers (the hub terminates their tunnels).
- Automatic acceptance of rotations announced by the server: the
  server is exactly the party this spec stops trusting alone.
