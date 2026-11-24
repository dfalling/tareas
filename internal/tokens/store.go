package tokens

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dfalling/tareas/internal/tokens/tokensdb"
)

// Store reads and writes API tokens in Postgres.
type Store struct {
	pool *pgxpool.Pool
	q    *tokensdb.Queries
}

// NewStore returns a token store backed by pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: tokensdb.New(pool)}
}

// Issue creates a new token for the user and returns its plaintext value —
// shown exactly once, at creation — alongside the stored record.
func (s *Store) Issue(ctx context.Context, userID int64, name string, expiresAt *time.Time) (string, Token, error) {
	name, err := validateName(name)
	if err != nil {
		return "", Token{}, err
	}
	plaintext, hash, prefix, err := generate()
	if err != nil {
		return "", Token{}, fmt.Errorf("generate token: %w", err)
	}
	row, err := s.q.CreateToken(ctx, tokensdb.CreateTokenParams{
		UserID:      userID,
		TokenHash:   hash,
		TokenPrefix: prefix,
		Name:        name,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		return "", Token{}, err
	}
	return plaintext, tokenFromRow(row), nil
}

// Authenticate resolves a presented plaintext token to its stored record.
// Unknown tokens and expired ones are both authentication failures; they are
// distinct errors so the API can log the difference without telling the
// client more than "unauthorized".
func (s *Store) Authenticate(ctx context.Context, plaintext string) (Token, error) {
	row, err := s.q.TokenByHash(ctx, Hash(plaintext))
	if errors.Is(err, pgx.ErrNoRows) {
		return Token{}, ErrNotFound
	}
	if err != nil {
		return Token{}, err
	}
	tok := tokenFromRow(row)
	if tok.ExpiresAt != nil && !tok.ExpiresAt.After(time.Now()) {
		return Token{}, ErrExpired
	}
	return tok, nil
}

// List returns the user's tokens, newest first, for the settings page.
func (s *Store) List(ctx context.Context, userID int64) ([]Token, error) {
	rows, err := s.q.ListTokensByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Token, len(rows))
	for i, row := range rows {
		out[i] = tokenFromRow(row)
	}
	return out, nil
}

// Revoke deletes one of the user's tokens. It reports ErrNotFound when there
// was nothing to revoke — including tokens owned by someone else, since the
// query is scoped by user_id.
func (s *Store) Revoke(ctx context.Context, userID, id int64) error {
	n, err := s.q.RevokeToken(ctx, tokensdb.RevokeTokenParams{ID: id, UserID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Touch records a use of the token. It is metadata only: failures are logged
// and swallowed so a bookkeeping hiccup never fails an authenticated request.
func (s *Store) Touch(ctx context.Context, id int64) {
	if err := s.q.TouchTokenLastUsed(ctx, id); err != nil {
		slog.Error("touch token last_used_at", "token_id", id, "err", err)
	}
}

// PurgeExpired deletes tokens past their expires_at. The §7 hygiene ticker
// calls it periodically; rows without an expiry are never touched.
func (s *Store) PurgeExpired(ctx context.Context) (int64, error) {
	return s.q.PurgeExpiredTokens(ctx)
}

func tokenFromRow(row tokensdb.ApiToken) Token {
	return Token{
		ID:         row.ID,
		UserID:     row.UserID,
		Prefix:     row.TokenPrefix,
		Name:       row.Name,
		LastUsedAt: row.LastUsedAt,
		ExpiresAt:  row.ExpiresAt,
		CreatedAt:  row.CreatedAt,
	}
}
