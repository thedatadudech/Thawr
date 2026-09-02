// Package store persists Thawr server state in a single SQLite database
// using modernc.org/sqlite (pure Go, no CGO, ADR 0002).
//
// It owns the schema and its numbered SQL migrations (embedded), and
// exposes small per-entity interfaces (peers, users, enrollment tokens,
// metadata such as the netmap generation) consumed by package control.
// Secrets are stored only as SHA-256 or argon2id hashes.
//
// Spec: docs/specs/001-server-bootstrap.md and docs/ARCHITECTURE.md §6.
package store
