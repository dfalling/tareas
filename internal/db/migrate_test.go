package db

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestMigrateCreatesSchema runs against a real Postgres when
// TEST_DATABASE_URL is set (the CI test job provides one); it is skipped
// otherwise.
func TestMigrateCreatesSchema(t *testing.T) {
	t.Parallel()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	if err := Migrate(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// A second run must be a no-op: migrations are idempotent.
	if err := Migrate(ctx, dsn); err != nil {
		t.Fatalf("second migrate run: %v", err)
	}

	verifyDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open verification database: %v", err)
	}
	defer func() { _ = verifyDB.Close() }()

	var tables int
	err = verifyDB.QueryRowContext(ctx, `
		SELECT count(*)
		FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_name IN ('users', 'api_tokens', 'todos', 'sessions')
	`).Scan(&tables)
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	if tables != 4 {
		t.Errorf("got %d expected tables, want 4", tables)
	}

	// The plan requires a plain DATE column for due dates — no times.
	var dueDateType string
	err = verifyDB.QueryRowContext(ctx, `
		SELECT data_type
		FROM information_schema.columns
		WHERE table_name = 'todos' AND column_name = 'due_date'
	`).Scan(&dueDateType)
	if err != nil {
		t.Fatalf("query due_date column: %v", err)
	}
	if dueDateType != "date" {
		t.Errorf("todos.due_date type = %q, want date", dueDateType)
	}
}
