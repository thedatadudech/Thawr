package store

import (
	"context"
	"database/sql"
	"fmt"

	// Registers the pure-Go "sqlite" driver.
	_ "modernc.org/sqlite"
)

// Store is the SQLite-backed persistence layer. It owns one database
// connection pool limited to a single connection, matching SQLite's
// single-writer model under WAL.
type Store struct {
	db *sql.DB
}

// Open opens or creates the database at path, applies connection pragmas
// and runs pending migrations. The caller must Close it.
func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("store: empty database path")
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping %s: %w", path, err)
	}
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("store: close: %w", err)
	}
	return nil
}

// Meta returns the key/value metadata accessor.
func (s *Store) Meta() *Meta { return &Meta{db: s.db} }

// Peers returns the peer accessor.
func (s *Store) Peers() *Peers { return &Peers{db: s.db} }
