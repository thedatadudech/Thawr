package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Peers accesses the peers table. Spec 002 adds create, list, and delete;
// spec 001 only needs the count for the status endpoint.
type Peers struct {
	db *sql.DB
}

// Count returns the number of registered peers.
func (p *Peers) Count(ctx context.Context) (int, error) {
	var n int
	if err := p.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM peers`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count peers: %w", err)
	}
	return n, nil
}
