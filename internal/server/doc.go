// Package server composes the Thawr control server from its parts:
// config, store, server WireGuard key, TLS certificate, the WireGuard hub
// interface, policy, and the HTTPS, admin-socket and STUN listeners. It
// owns startup order, readiness, policy reload and clean shutdown, and
// is testable with a fake WireGuard device.
//
// Spec: docs/specs/001-server-bootstrap.md.
package server
