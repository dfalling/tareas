package tokens

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dfalling/tareas/internal/db"
	"github.com/dfalling/tareas/internal/users"
)

// lockTestDB holds db.TestLockID on its own pooled connection for the whole
// test: `go test ./...` runs package test binaries in parallel against one
// shared TEST_DATABASE_URL, and this test truncates tables that the other
// packages' integration tests also truncate — the advisory lock keeps them
// from running at the same time.
func lockTestDB(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection for test lock: %v", err)
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", db.TestLockID); err != nil {
		conn.Release()
		t.Fatalf("acquire test lock: %v", err)
	}
	t.Cleanup(func() {
		// Uncancellable: a canceled test must still release the lock.
		_, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", db.TestLockID)
		conn.Release()
	})
}

// TestTokenStore exercises the store against a real Postgres when
// TEST_DATABASE_URL is set (the CI test job provides one): issue and
// authenticate round-trips, expiry, user scoping, revocation, and the purge.
// It is skipped otherwise.
func TestTokenStore(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	// Cleanup order matters: t.Cleanup runs last-registered-first, so
	// lockTestDB's cleanup (registered below) releases its connection —
	// the one holding the advisory lock — before the pool closes. A plain
	// `defer pool.Close()` would deadlock: Close waits for that connection,
	// which is only released in cleanup.
	t.Cleanup(pool.Close)
	lockTestDB(t, ctx, pool)
	if err := db.Migrate(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE users, todos, api_tokens, sessions RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	userStore := users.NewStore(pool)
	owner, err := userStore.Create(ctx, "owner@example.com", "not-a-real-hash")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := userStore.Create(ctx, "other@example.com", "not-a-real-hash")
	if err != nil {
		t.Fatalf("create other: %v", err)
	}

	store := NewStore(pool)

	// Issue stores the hash and prefix, never the plaintext.
	plaintext, tok, err := store.Issue(ctx, owner.ID, "laptop", nil)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if len(plaintext) != 43 || tok.Prefix != plaintext[:PrefixLen] {
		t.Fatalf("issued token shape: plaintext %q prefix %q", plaintext, tok.Prefix)
	}
	if tok.Name != "laptop" || tok.UserID != owner.ID || tok.ExpiresAt != nil {
		t.Errorf("issued token = %+v", tok)
	}

	// Authenticate round-trips the plaintext to the same record.
	got, err := store.Authenticate(ctx, plaintext)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if got.ID != tok.ID || got.UserID != owner.ID {
		t.Errorf("authenticate got %+v, want token %d", got, tok.ID)
	}

	// A different token does not authenticate, and neither does garbage.
	if _, err := store.Authenticate(ctx, "not-a-real-token"); !errors.Is(err, ErrNotFound) {
		t.Errorf("authenticate(garbage) = %v, want ErrNotFound", err)
	}

	// The same plaintext re-authenticates (hash lookup is deterministic).
	if got, err := store.Authenticate(ctx, plaintext); err != nil || got.ID != tok.ID {
		t.Errorf("re-authenticate: (%+v, %v)", got, err)
	}

	// Tokens are scoped: the other user cannot revoke the owner's token.
	if err := store.Revoke(ctx, other.ID, tok.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoke foreign token = %v, want ErrNotFound", err)
	}
	if _, err := store.Authenticate(ctx, plaintext); err != nil {
		t.Errorf("token still valid after foreign revoke attempt: %v", err)
	}

	// List is scoped by user and newest first.
	if _, _, err := store.Issue(ctx, owner.ID, "phone", nil); err != nil {
		t.Fatalf("issue second: %v", err)
	}
	list, err := store.List(ctx, owner.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].Name != "phone" || list[1].Name != "laptop" {
		t.Errorf("list = %+v, want newest (phone) first", list)
	}
	if otherList, err := store.List(ctx, other.ID); err != nil || len(otherList) != 0 {
		t.Errorf("other user's list = %+v, %v; want empty", otherList, err)
	}

	// Touch stamps last_used_at.
	if _, err := pool.Exec(ctx, "UPDATE api_tokens SET last_used_at = NULL"); err != nil {
		t.Fatalf("clear last_used_at: %v", err)
	}
	store.Touch(ctx, tok.ID)
	touched, err := store.Authenticate(ctx, plaintext)
	if err != nil {
		t.Fatalf("authenticate after touch: %v", err)
	}
	if touched.LastUsedAt == nil {
		t.Error("last_used_at still NULL after Touch")
	}

	// An expired token fails authentication with ErrExpired. A plaintext
	// cannot be re-dated, so issue a live token and backdate its row.
	plaintext2, liveTok, err := store.Issue(ctx, owner.ID, "backdated", nil)
	if err != nil {
		t.Fatalf("issue backdated: %v", err)
	}
	past := time.Now().Add(-time.Hour).UTC()
	if _, err := pool.Exec(ctx, "UPDATE api_tokens SET expires_at = $1 WHERE id = $2", past, liveTok.ID); err != nil {
		t.Fatalf("backdate expiry: %v", err)
	}
	if _, err := store.Authenticate(ctx, plaintext2); !errors.Is(err, ErrExpired) {
		t.Errorf("authenticate(expired) = %v, want ErrExpired", err)
	}

	// Revoke removes the token; authenticating it afterwards fails.
	if err := store.Revoke(ctx, owner.ID, tok.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := store.Revoke(ctx, owner.ID, tok.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoke twice = %v, want ErrNotFound", err)
	}
	if _, err := store.Authenticate(ctx, plaintext); !errors.Is(err, ErrNotFound) {
		t.Errorf("authenticate(revoked) = %v, want ErrNotFound", err)
	}

	// Purge removes only expired rows: the backdated one goes, the live
	// phone token stays.
	n, err := store.PurgeExpired(ctx)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n != 1 {
		t.Errorf("purge removed %d rows, want 1", n)
	}
	if remaining, err := store.List(ctx, owner.ID); err != nil || len(remaining) != 1 || remaining[0].Name != "phone" {
		t.Errorf("after purge list = %+v, %v; want only the live token", remaining, err)
	}
}
