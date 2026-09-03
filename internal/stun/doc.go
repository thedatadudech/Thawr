// Package stun implements the small subset of STUN (RFC 5389) Thawr
// needs: binding requests and responses with XOR-MAPPED-ADDRESS, a
// client that asks the server's two STUN ports and detects
// endpoint-dependent (symmetric) NAT, and a rate-limited server.
//
// The codec in stun.go is copied from tailscale.com/net/stun
// (BSD-3-Clause, see the file header) as decided in TASKS.md D2, so the
// large tailscale.com module is not a dependency.
package stun
