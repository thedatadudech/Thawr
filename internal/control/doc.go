// Package control is the decision-making core of the Thawr server:
// enrollment of peers with one-time tokens, the peer registry and overlay
// IP allocation, endpoint tracking, policy parsing and compilation, and
// building the per-peer network map that is distributed to clients.
//
// It depends only on the storage interfaces it declares itself; the
// concrete SQLite implementation lives in package store. It contains no
// networking code and no knowledge of gRPC, REST, or WireGuard devices.
//
// Specs: docs/specs/002, 003, 006. Design: docs/ARCHITECTURE.md §2–§4.
package control
