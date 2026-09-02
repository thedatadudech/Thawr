# Spec 002 — Peer enrollment

Sprint 1. Depends on: 001. Packages: `internal/control` (enroller,
allocator, registry), `internal/store`, `internal/api` (gRPC `Enroll`,
REST users/tokens/peers), `cmd/thawr` (`admin user`, `admin token`,
`admin peer`, `client up`).

## Goal

An admin creates a one-time token; a new device runs one command with
that token and becomes a registered peer with an overlay IP and a node
secret. Every peer is a generic identity (`kind`, `tags`, optional
owner).

## User story

As an admin, I run `thawr admin token create --owner alice --kind human`
and send Alice the printed join command. Alice runs it on her laptop and
sees "enrolled as alice-laptop, 100.64.0.7". As an admin, I can list,
rename, and delete peers.

## Commands

```
thawr admin user create <name> --role admin|member   # prompts for password, or THAWR_PASSWORD_FILE
thawr admin user list
thawr admin token create --owner <user> --kind human|server|agent [--tags tag:a,tag:b] [--expires 1h] [--name <peer-name>]
thawr admin token list
thawr admin token revoke <id>
thawr admin peer list [--json]
thawr admin peer rename <name> <new-name>
thawr admin peer delete <name>
thawr client up --server https://vpn.example.com --token thawr_... --fingerprint sha256:... [--name <peer-name>]
thawr client down
```

`token create` prints:

```
Token id:   tk_7f3a…            (expires 2026-09-02 17:04 UTC, single use)
Join with:  thawr client up --server https://vpn.example.com --token thawr_Qm3… --fingerprint sha256:ab12…
```

The token secret is printed once and never again.

## Behaviour

Token: id `tk_` + 8 random hex; secret `thawr_` + base64url(32 random
bytes). Stored: id, SHA-256(secret), owner, kind, tags, optional name,
expiry, `created_by`, `used_at`, `used_by_peer`. Default expiry 1 h,
max 30 d. Members may only create tokens with `--owner` = themselves.

Client `up`:
1. If `state.json` exists and is valid, refuse with "already enrolled as
   X; run `thawr client down --forget` first".
2. Generate WireGuard keypair via `wgtypes.GeneratePrivateKey`
   (Curve25519 from `x/crypto`), write `node.key` 0600.
3. Connect to the server with TLS, verify fingerprint. Without
   `--fingerprint`: print the server's fingerprint and abort unless
   `--accept-fingerprint` is given.
4. Call `Enroll{token, public_key, hostname, os, arch, version, name?}`.
5. Persist `state.json` 0600. Continue as in spec 003 (start the daemon).

Server `Enroll`:
1. Validate: token format, public key 32 bytes base64, hostname ≤ 63
   chars, version ≥ `min_client_version`.
2. Hash token, look up, check `used_at IS NULL` and `expires_at > now`.
   Any failure → `PERMISSION_DENIED "invalid token"` (identical message),
   logged at `warn` with token id prefix if parsable and remote address.
3. In one transaction: mark token used, allocate the lowest free IPv4 in
   `overlay.cidr` (excluding network, hub, broadcast), derive a unique
   name (`--name`, else token name, else hostname sanitised to a DNS
   label, suffixed `-2`, `-3` on conflict), insert peer with `kind`,
   `tags`, `owner` from the token, `mode = agent`, generate node secret
   (32 random bytes), store its SHA-256, bump netmap generation.
4. Return `peer_id, name, ipv4, overlay_cidr, node_secret, hub_public_key,
   hub_endpoint, server_version`.
5. Rate limit `Enroll` to 10 per minute per remote IP.

Deleting a peer: remove row, bump generation, close its `Sync` stream
and relay session, remove from hub if static.

REST equivalents under `/api/v1/users`, `/tokens`, `/peers` with the
same validation, used by the admin UI (peer list, token creation form).

## Acceptance criteria

- [ ] `user create` stores an argon2id hash (time=3, memory=64 MiB,
      threads=4, salt 16 bytes); the password never appears in logs or
      DB.
- [ ] A token can be used exactly once; second use, expired, revoked and
      unknown tokens all return the same error.
- [ ] A member cannot create a token for another owner (REST 403, CLI
      error).
- [ ] Enrollment allocates addresses sequentially and never reuses an
      address held by an existing peer; the allocator survives restart.
- [ ] Two devices with the same hostname get `host` and `host-2`.
- [ ] `kind` and `tags` on the created peer equal those on the token,
      regardless of what the client sends.
- [ ] `peer delete` removes the peer and increments the generation;
      the peer's node secret is rejected afterwards.
- [ ] `client up` with a wrong fingerprint aborts before sending the
      token.
- [ ] `client up` on an already-enrolled machine refuses.
- [ ] All files written by the client are 0600.
- [ ] Admin UI can list peers and create tokens (form → join command
      shown once).

## Test cases

- `TestTokenCreateAndHash`, `TestTokenExpiry` (injected clock),
  `TestTokenSingleUse` (concurrent double use → exactly one succeeds).
- `TestEnrollTokenErrors` (table: used, expired, unknown, malformed →
  same message).
- `TestAllocatorSequential`, `TestAllocatorSkipsReserved`,
  `TestAllocatorExhausted` (tiny `/29` CIDR).
- `TestNameDerivation` (hostname sanitising, conflicts, `--name`).
- `TestEnrollKindTagsFromToken`.
- `TestMemberTokenOwnerRestriction`.
- `TestPeerDeleteBumpsGeneration`.
- `TestClientUpFingerprintMismatch` (httptest TLS server with a
  different cert).
- `TestClientStateFileModes`.
- `TestArgon2Params`.
- Integration: `TestEnrollTwoClients` in netns — server + two clients
  enrol, `peer list` shows both with distinct IPs.

## Out of scope

- Netmap delivery to the client (spec 003).
- Reusable / multi-use tokens, token-bound IP reservation.
- OIDC login (phase 2, ADR 0006).
- Password reset flow (admin sets a new password via CLI is enough).
- Peer expiry / auto-cleanup of stale peers.
