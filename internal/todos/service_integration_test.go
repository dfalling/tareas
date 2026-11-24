package todos

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dfalling/tareas/internal/db"
	"github.com/dfalling/tareas/internal/users"
)

// lockTestDB holds db.TestLockID on its own pooled connection for the whole
// test: `go test ./...` runs package test binaries in parallel against one
// shared TEST_DATABASE_URL, and this test truncates tables that the web
// package's auth-flow test also truncates — the advisory lock keeps them
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

// TestTodoService exercises the service against a real Postgres when
// TEST_DATABASE_URL is set (the CI test job provides one): CRUD round-trips,
// the alphabetical and API sort orders as SQL produces them, user scoping,
// and the toggle's completed_at bookkeeping. It is skipped otherwise.
func TestTodoService(t *testing.T) {
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

	svc := NewService(pool, time.UTC)

	// Create normalizes: the title is trimmed, whitespace-only notes are
	// stored as NULL, and a due date lands on UTC midnight.
	banana, err := svc.Create(ctx, owner.ID, CreateInput{Title: "  Banana  ", Notes: "   "})
	if err != nil {
		t.Fatalf("create banana: %v", err)
	}
	if banana.ID <= 0 || banana.Title != "Banana" || banana.Notes != nil || banana.DueDate != nil || banana.Completed {
		t.Fatalf("create banana returned %+v", banana)
	}
	if banana.CreatedAt.IsZero() || banana.UpdatedAt.IsZero() {
		t.Fatalf("create banana left timestamps unset: %+v", banana)
	}

	apple, err := svc.Create(ctx, owner.ID, CreateInput{Title: "apple", DueDate: mustDate(t, "2026-07-01")})
	if err != nil {
		t.Fatalf("create apple: %v", err)
	}
	if !apple.DueDate.Equal(*mustDate(t, "2026-07-01")) {
		t.Fatalf("apple due date = %v, want UTC midnight 2026-07-01", apple.DueDate)
	}

	cherry, err := svc.Create(ctx, owner.ID, CreateInput{Title: "cherry", Notes: "stem on"})
	if err != nil {
		t.Fatalf("create cherry: %v", err)
	}

	// List is alphabetical by lower(title): byte order would put "Banana"
	// before "apple", lower() must not.
	list, err := svc.List(ctx, owner.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := ids(list); !equalIDs(got, []int64{apple.ID, banana.ID, cherry.ID}) {
		t.Fatalf("list order = %v (titles %v), want apple, Banana, cherry", got, titlesOf(list))
	}

	// Another user sees nothing, and cannot fetch, update, or delete
	// someone else's todo — user_id scoping on every statement.
	strangerList, err := svc.List(ctx, other.ID)
	if err != nil {
		t.Fatalf("list for other user: %v", err)
	}
	if len(strangerList) != 0 {
		t.Fatalf("other user's list = %v, want empty", titlesOf(strangerList))
	}
	if _, err := svc.ByID(ctx, other.ID, banana.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("ByID across users: err = %v, want ErrNotFound", err)
	}
	if _, err := svc.UpdateMeta(ctx, other.ID, banana.ID, "hijack", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateMeta across users: err = %v, want ErrNotFound", err)
	}
	if err := svc.Delete(ctx, other.ID, banana.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete across users: err = %v, want ErrNotFound", err)
	}

	// UpdateMeta rewrites title and notes only.
	if _, err := svc.UpdateMeta(ctx, owner.ID, cherry.ID, "cherries", "stem off"); err != nil {
		t.Fatalf("update cherry: %v", err)
	}
	cherry, err = svc.ByID(ctx, owner.ID, cherry.ID)
	if err != nil {
		t.Fatalf("reload cherry: %v", err)
	}
	if cherry.Title != "cherries" || cherry.Notes == nil || *cherry.Notes != "stem off" || cherry.DueDate != nil {
		t.Fatalf("updated cherry = %+v", cherry)
	}

	// SetDueDate sets, and clearing means storing NULL, not "don't touch".
	if _, err := svc.SetDueDate(ctx, owner.ID, cherry.ID, mustDate(t, "2026-12-25")); err != nil {
		t.Fatalf("set cherry due date: %v", err)
	}
	cherry, err = svc.ByID(ctx, owner.ID, cherry.ID)
	if err != nil {
		t.Fatalf("reload cherry: %v", err)
	}
	if cherry.DueDate == nil || !cherry.DueDate.Equal(*mustDate(t, "2026-12-25")) {
		t.Fatalf("cherry due date = %v, want 2026-12-25", cherry.DueDate)
	}
	if _, err := svc.SetDueDate(ctx, owner.ID, cherry.ID, nil); err != nil {
		t.Fatalf("clear cherry due date: %v", err)
	}
	if cherry, err = svc.ByID(ctx, owner.ID, cherry.ID); err != nil {
		t.Fatalf("reload cherry: %v", err)
	}
	if cherry.DueDate != nil {
		t.Fatalf("cleared cherry due date = %v, want nil", cherry.DueDate)
	}

	// Toggle stamps completed_at when completing and clears it when reopening.
	completed, err := svc.Toggle(ctx, owner.ID, banana.ID)
	if err != nil {
		t.Fatalf("toggle banana: %v", err)
	}
	if !completed.Completed || completed.CompletedAt == nil {
		t.Fatalf("toggled banana = %+v, want completed with a timestamp", completed)
	}
	reopened, err := svc.Toggle(ctx, owner.ID, banana.ID)
	if err != nil {
		t.Fatalf("re-toggle banana: %v", err)
	}
	if reopened.Completed || reopened.CompletedAt != nil {
		t.Fatalf("re-toggled banana = %+v, want open with no timestamp", reopened)
	}

	// The API lists: active todos sorted by due date with unscheduled last,
	// completed ones available as their own list.
	zebra, err := svc.Create(ctx, owner.ID, CreateInput{Title: "zebra", DueDate: mustDate(t, "2026-06-01")})
	if err != nil {
		t.Fatalf("create zebra: %v", err)
	}
	if _, err := svc.Toggle(ctx, owner.ID, banana.ID); err != nil {
		t.Fatalf("complete banana: %v", err)
	}

	active, err := svc.ListActive(ctx, owner.ID)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	// due 06-01, then 07-01, then the unscheduled pair alphabetically.
	if got := ids(active); !equalIDs(got, []int64{zebra.ID, apple.ID, cherry.ID}) {
		t.Fatalf("active order = %v (titles %v), want zebra, apple, cherry", got, titlesOf(active))
	}

	done, err := svc.ListCompleted(ctx, owner.ID)
	if err != nil {
		t.Fatalf("list completed: %v", err)
	}
	if got := ids(done); !equalIDs(got, []int64{banana.ID}) {
		t.Fatalf("completed = %v, want [banana]", got)
	}

	// Delete is a hard delete; the second attempt is a 404 in waiting.
	if err := svc.Delete(ctx, owner.ID, zebra.ID); err != nil {
		t.Fatalf("delete zebra: %v", err)
	}
	if _, err := svc.ByID(ctx, owner.ID, zebra.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("ByID after delete: err = %v, want ErrNotFound", err)
	}
	if err := svc.Delete(ctx, owner.ID, zebra.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: err = %v, want ErrNotFound", err)
	}

	// Update rewrites title, notes, and due date together — the edit form
	// submits the three as one change, so they land in one transaction.
	notebook, err := svc.Create(ctx, owner.ID, CreateInput{Title: "notebook"})
	if err != nil {
		t.Fatalf("create notebook: %v", err)
	}
	updated, err := svc.Update(ctx, owner.ID, notebook.ID, UpdateInput{
		Title:   "notebook A5",
		Notes:   "dotted",
		DueDate: mustDate(t, "2026-08-01"),
	})
	if err != nil {
		t.Fatalf("update notebook: %v", err)
	}
	if updated.Title != "notebook A5" || updated.Notes == nil || *updated.Notes != "dotted" || updated.DueDate == nil || !updated.DueDate.Equal(*mustDate(t, "2026-08-01")) {
		t.Fatalf("updated notebook = %+v", updated)
	}
	if _, err := svc.Update(ctx, other.ID, notebook.ID, UpdateInput{Title: "steal"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update across users: err = %v, want ErrNotFound", err)
	}

	// Validation runs before any query: a blank title is rejected.
	if _, err := svc.Create(ctx, owner.ID, CreateInput{Title: "   "}); !errors.Is(err, ErrTitleRequired) {
		t.Errorf("blank title: err = %v, want ErrTitleRequired", err)
	}
}

func ids(list []Todo) []int64 {
	out := make([]int64, len(list))
	for i, t := range list {
		out[i] = t.ID
	}
	return out
}

func equalIDs(got, want []int64) bool {
	return slices.Equal(got, want)
}

func titlesOf(list []Todo) []string {
	out := make([]string, len(list))
	for i, t := range list {
		out[i] = t.Title
	}
	return out
}
