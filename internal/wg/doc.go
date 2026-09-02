// Package wg adapts WireGuard devices behind one interface so the rest of
// Thawr does not care whether the kernel module or wireguard-go is in use.
//
// It provides the Device interface (configure desired state, read peer
// statistics, close), a kernel implementation via wgctrl and netlink, a
// userspace implementation via wireguard-go and a TUN device, and the
// receiver-side packet filter used for port-level policy enforcement
// (nftables with the kernel module, an in-process filter with
// wireguard-go).
//
// Thawr never implements cryptography; this package only configures
// WireGuard (ADR 0004).
//
// Specs: docs/specs/003, 004, 006.
package wg
