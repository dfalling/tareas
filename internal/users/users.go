// Package users provides the user store and password hashing.
package users

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when no user matches the lookup.
var ErrNotFound = errors.New("user not found")

// User is a stored account.
type User struct {
	ID           int64
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

// Store reads and writes users in Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a user store backed by pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Count returns the number of accounts. The one-time /setup bootstrap is
// only available while this is zero.
func (s *Store) Count(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

// Create inserts a new user. Email is normalized (trimmed, lowercased)
// before storage; the unique index on lower(email) is the final guard.
func (s *Store) Create(ctx context.Context, email, passwordHash string) (User, error) {
	email = normalizeEmail(email)
	u := User{Email: email}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash)
		VALUES ($1, $2)
		RETURNING id, created_at
	`, email, passwordHash).Scan(&u.ID, &u.CreatedAt)
	if err != nil {
		return User{}, err
	}
	return u, nil
}

// ByEmail returns the user with the given email (normalized before lookup).
func (s *Store) ByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, created_at
		FROM users
		WHERE email = $1
	`, normalizeEmail(email)).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	return u, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
