package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("store: not found")

// Meta reads and writes the key/value metadata table (schema version,
// netmap generation, server key fingerprint).
type Meta struct {
	db *sql.DB
}

// Well-known meta keys.
const (
	MetaNetmapGeneration     = "netmap_generation"
	MetaServerKeyFingerprint = "server_key_fingerprint"
)

// Get returns the value for key or ErrNotFound.
func (m *Meta) Get(ctx context.Context, key string) (string, error) {
	var v string
	err := m.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("meta %q: %w", key, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("store: meta get %q: %w", key, err)
	}
	return v, nil
}

// Set inserts or replaces key.
func (m *Meta) Set(ctx context.Context, key, value string) error {
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	if err != nil {
		return fmt.Errorf("store: meta set %q: %w", key, err)
	}
	return nil
}
