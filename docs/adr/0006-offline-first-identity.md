# ADR 0006: Identity is local users plus one-time tokens; OIDC optional

Status: Accepted
Date: 2026-09-02

## Context

Tailscale, NetBird and Firezone require an external identity provider to
enrol devices. That is a hard dependency on an internet-reachable service
and, for self-hosters, on running Keycloak, Zitadel, Dex or similar. Many
target deployments have no IdP, no internet, or a policy against
third-party login. Yet teams that do have an IdP want SSO.

## Decision

The built-in identity system is complete on its own:

- Local users (name, role, argon2id password hash) stored in SQLite.
- One-time enrollment tokens created by an admin or by a member for
  themselves, carrying owner, kind, tags and expiry.
- Agent peers authenticate to the control channel with a node secret
  issued at enrollment; the WireGuard key itself is the peer's identity
  on the data plane.
- The admin CLI on the server host authenticates through Unix socket
  permissions, so a server with no users at all is still administrable.

OIDC is an optional provider behind an interface in `internal/control`
(`IdentityProvider`) that maps an external subject to a local user. When
configured it adds a login button to the UI; it never gates enrollment,
policy or the client, and the server starts and runs without reaching
the provider.

Every peer, regardless of how it was created, is a generic identity with
`kind` in {human, server, agent}, so workload identity in phase 2 adds
new token issuers, not new peer types.

## Consequences

- A Thawr network can be built and operated with the internet cable
  unplugged.
- Password handling becomes Thawr's responsibility: argon2id with
  documented parameters, rate limiting on login, no password in logs.
- There is no self-service signup; an admin creates users. Correct for
  the target audience.
- Groups in the policy file reference local user names; when OIDC is
  used, the mapping must produce stable local names, which is the
  provider plugin's job.
- Device attestation, hardware-bound keys and SSO-driven device
  revocation are out of scope for v1.
