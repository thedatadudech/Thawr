# Spec 008 — Mobile QR export (static peers via the hub)

Sprint 2. Depends on: 001 (hub), 003, 006 (hub-side filter). Packages:
`internal/control` (static peer creation), `internal/wg` (hub peer
management on the server), `internal/api` (`POST /peers/mobile`),
`cmd/thawr` (`admin peer add-mobile`), `web/` (QR page).

## Goal

A phone joins the network with the official WireGuard app by scanning a
QR code, without any Thawr software on the phone. The phone is a
first-class peer for policy and status; its traffic is routed through the
server's WireGuard hub.

## User story

As an admin, I run `thawr admin peer add-mobile --owner alice --name
alice-phone`, Alice scans the QR from my screen, and her phone can reach
whatever the policy allows for Alice's peers. When she loses the phone,
I delete the peer and it is cut off.

## Commands

```
thawr admin peer add-mobile --owner <user> --name <name> [--kind human] [--tags ...] [--out file.conf] [--no-qr]
```

Prints a QR code (UTF-8 half-block rendering, fits an 80x40 terminal)
and, with `--out`, writes the `.conf` with mode 0600. Without `--out`
the config is never written to disk on the server. The admin UI has
"Add mobile peer" under Peers that shows the QR once in a modal with a
warning; navigating away discards it; the config cannot be retrieved
again.

## Behaviour

Server, `POST /api/v1/peers/mobile`:
1. Validate owner exists, name is a free DNS label, tags pass
   `tagOwners`.
2. Generate a WireGuard keypair in memory (`wgtypes.GeneratePrivateKey`).
3. Allocate ipv4, insert peer with `mode = static`, `public_key`,
   `kind` (default `human`), tags, owner. No node secret.
4. Add the peer to the hub interface: `AllowedIPs = ipv4/32`.
5. Install the hub-side filter for it (spec 006) and enable forwarding
   between `thawr0` and itself for the overlay CIDR (Linux:
   `net.ipv4.conf.thawr0.forwarding=1`, nftables `forward` chain with
   the same filter rules; userspace hub: the filter sits in the TUN
   path).
6. Bump generation: agent peers now receive the hub with an additional
   `AllowedIPs` entry for the phone's address.
7. Render and return:

```
[Interface]
PrivateKey = <generated>
Address = 100.64.0.21/32

[Peer]
PublicKey = <hub public key>
Endpoint = vpn.example.com:51820
AllowedIPs = 100.64.0.0/10
PersistentKeepalive = 25
```

8. Zero the private key in memory after the response is written. Log
   `peer created` with name, owner, ipv4, and public-key fingerprint.

Hub data path: the phone sends to `100.64.0.0/10` through the hub; the
hub forwards to the agent peer over its own WireGuard peering (agent
peers keep a keepalive to the hub, spec 003, so the hub can reach them
behind NAT). Return traffic follows the same path. Agent peers see the
phone's overlay address as source, so their receiver-side filter applies
as for any peer.

Deleting a static peer removes it from the hub, its filter rules, and
bumps the generation (agent peers drop its address from the hub's
`AllowedIPs`).

`thawr admin peer list` shows `mode` and, for static peers, last
handshake from the hub as "last seen".

## Acceptance criteria

- [ ] `add-mobile` produces a config that the reference `wg-quick`
      accepts (integration test uses a fourth netns with `wg-quick` as
      the "phone").
- [ ] The phone netns can `ping` an agent peer owned by the same user
      and cannot reach a peer denied by policy; the agent peer's filter
      counts the drop for a denied port, and the hub-side filter drops
      phone → denied peer before forwarding.
- [ ] The private key appears in the API response body and the CLI
      output exactly once, in no log line, and not in the DB
      (`TestMobileKeyNotPersisted`).
- [ ] QR decodes (test with a QR decoder library in tests) to the exact
      config text.
- [ ] Deleting the peer removes the hub entry within 1 s and the phone
      loses connectivity; re-adding with the same name yields a new key
      and new address.
- [ ] Status on agent peers shows the phone as `via hub`.
- [ ] Admin UI modal shows the QR once and refuses to re-open it.
- [ ] Threat model section T4 (server sees static-peer plaintext) is
      linked from the CLI output and the UI modal as a one-line warning.

## Test cases

- `TestMobileConfigRender` (golden).
- `TestMobileKeyNotPersisted`, `TestMobileKeyNotLogged`.
- `TestMobileTagOwners`.
- `TestHubAllowedIPsIncludeStatic` (netmap builder).
- `TestQRRoundTrip`.
- `TestHubPeerAddRemove` (fake device).
- Integration: `TestMobilePeerViaHub`, `TestMobilePolicyEnforced`.

## Out of scope

- A native mobile app, NAT traversal from the phone, end-to-end
  encryption for phones (non-goals v1; threat model T4).
- DNS for phones.
- Exporting a config for an existing agent peer (agent peers own their
  key; the server cannot export it).
- Per-peer bandwidth limits on the hub.
