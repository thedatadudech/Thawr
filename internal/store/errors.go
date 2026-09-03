package store

import (
	"errors"
	"strings"
)

// ErrConflict is returned when a unique constraint is violated.
var ErrConflict = errors.New("store: already exists")

// isUniqueViolation recognises SQLite's constraint error text; the pure-Go
// driver does not expose a typed error for it.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
