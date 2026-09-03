package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// User roles.
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// User is a local account.
type User struct {
	ID           string
	Name         string
	Role         string
	PasswordHash string
	Disabled     bool
	CreatedAt    time.Time
}

// Users accesses the users table.
type Users struct {
	q querier
}

const userColumns = `id, name, role, password_hash, disabled, created_at`

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var (
		u        User
		disabled int
		created  string
	)
	if err := row.Scan(&u.ID, &u.Name, &u.Role, &u.PasswordHash, &disabled, &created); err != nil {
		return User{}, err
	}
	u.Disabled = disabled != 0
	u.CreatedAt = parseTime(created)
	return u, nil
}

// Create inserts u. ErrConflict when the name exists.
func (s *Users) Create(ctx context.Context, u User) error {
	_, err := s.q.ExecContext(ctx,
		`INSERT INTO users (`+userColumns+`) VALUES (?, ?, ?, ?, ?, ?)`,
		u.ID, u.Name, u.Role, u.PasswordHash, boolInt(u.Disabled), formatTime(u.CreatedAt))
	if isUniqueViolation(err) {
		return fmt.Errorf("user %q: %w", u.Name, ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("store: create user %q: %w", u.Name, err)
	}
	return nil
}

// GetByName returns the user or ErrNotFound.
func (s *Users) GetByName(ctx context.Context, name string) (User, error) {
	u, err := scanUser(s.q.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("user %q: %w", name, ErrNotFound)
	}
	if err != nil {
		return User{}, fmt.Errorf("store: get user %q: %w", name, err)
	}
	return u, nil
}

// GetByID returns the user or ErrNotFound.
func (s *Users) GetByID(ctx context.Context, id string) (User, error) {
	u, err := scanUser(s.q.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("user %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return User{}, fmt.Errorf("store: get user %s: %w", id, err)
	}
	return u, nil
}

// List returns all users ordered by name.
func (s *Users) List(ctx context.Context) ([]User, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("store: list users: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan user: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list users: %w", err)
	}
	return out, nil
}

// Count returns the number of users.
func (s *Users) Count(ctx context.Context) (int, error) {
	var n int
	if err := s.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count users: %w", err)
	}
	return n, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// formatTime stores RFC 3339 UTC with millisecond precision.
func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func formatTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func parseTimePtr(s sql.NullString) *time.Time {
	if !s.Valid {
		return nil
	}
	t := parseTime(s.String)
	return &t
}
