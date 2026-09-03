// Package client implements the device side of Thawr that is independent
// of the command line: the local state directory (node key, enrollment
// state), TLS fingerprint pinning towards the server, and the enrollment
// call. The running daemon (netmap sync, WireGuard configuration) lands
// with spec 003.
//
// Spec: docs/specs/002-peer-enrollment.md.
package client
