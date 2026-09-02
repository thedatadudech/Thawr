# ADR 0004: WireGuard is the only data plane, never own crypto

Status: Accepted
Date: 2026-09-02

## Context

A private network needs an encrypted tunnel with authenticated peers.
WireGuard is in the Linux kernel, has a userspace reference
implementation in Go, a formally verified protocol, a fixed cipher suite
with no negotiation, and an official app on iOS and Android. Every
alternative (OpenVPN, IPsec, a custom Noise-based tunnel) would either
add configuration surface or require Thawr to own cryptographic code.

## Decision

The data plane is WireGuard and nothing else. On Linux Thawr uses the
kernel module through `wgctrl` when it is present and falls back to
`wireguard-go`; on macOS and Windows it uses `wireguard-go` (with
WireGuardNT/wintun on Windows). Thawr never implements a cryptographic
primitive, key exchange, or transport encryption. Control-channel
security is TLS from the Go standard library. Password hashing is
argon2id from `golang.org/x/crypto`. Randomness is `crypto/rand`.

Relay traffic is WireGuard ciphertext forwarded as opaque frames; the
relay does not hold session keys. The only exception to end-to-end
encryption is traffic of `static` (mobile) peers, which the server
decrypts and re-encrypts as a WireGuard hub. This exception is recorded
in `docs/THREAT_MODEL.md`.

## Consequences

- Phones work with the official WireGuard app and a QR code; no native
  Thawr app is needed in v1.
- No cipher agility and no protocol negotiation, which removes an entire
  class of bugs.
- Thawr's port-level policy cannot be expressed in WireGuard (which only
  knows AllowedIPs); a receiver-side packet filter is required
  (`docs/ARCHITECTURE.md` §4.6).
- Layer-2 networking is impossible by construction, matching the
  non-goals.
- Any pull request adding a cipher, a key-exchange, or custom
  encryption is rejected on principle.
