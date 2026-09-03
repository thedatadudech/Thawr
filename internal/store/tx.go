package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// querier is the subset of *sql.DB and *sql.Tx the accessors need, so
// the same code runs inside and outside a transaction.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// InTx runs fn with a Store bound to one transaction. The transaction is
// committed when fn returns nil and rolled back otherwise.
func (s *Store) InTx(ctx context.Context, fn func(tx *Store) error) error {
	if s.tx != nil {
		return errors.New("store: nested transactions are not supported")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	bound := &Store{db: s.db, tx: tx}
	if err := fn(bound); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit tx: %w", err)
	}
	return nil
}

// q returns the active querier: the transaction when bound, else the pool.
func (s *Store) q() querier {
	if s.tx != nil {
		return s.tx
	}
	return s.db
}
