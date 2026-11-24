package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dfalling/tareas/internal/db"
	"github.com/dfalling/tareas/internal/ratelimit"
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

// TestAPI exercises every endpoint against a real Postgres when
// TEST_DATABASE_URL is set (the CI test job provides one): login and token
// issuance, bearer authentication, the full todo CRUD with PATCH partial
// semantics, the stable sort order, and logout revocation. It is skipped
// otherwise.
func TestAPI(t *testing.T) {
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

	hash, err := users.HashPassword("longenough1")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := users.NewStore(pool).Create(ctx, "owner@example.com", hash); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// A generous limiter: rate limiting has its own test below.
	app := New(pool, time.UTC, ratelimit.New(1000, time.Minute))
	ts := httptest.NewServer(app.Routes())
	defer ts.Close()

	// login exchanges credentials for a bearer token.
	login := func(email, password string) (*http.Response, map[string]any) {
		t.Helper()
		body := fmt.Sprintf(`{"email": %q, "password": %q}`, email, password)
		res, raw := call(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/auth/login", body, "")
		return res, decodeObject(t, raw)
	}

	// Without credentials every endpoint is 401 with the challenge header.
	res, _ := call(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/todos", "", "")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/todos unauthenticated: status %d, want 401", res.StatusCode)
	}
	if got := res.Header.Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want Bearer", got)
	}
	res, _ = call(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/todos", "", "not-the-real-token")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/todos bad token: status %d, want 401", res.StatusCode)
	}

	// Bad credentials are 401; malformed and unknown-field bodies are 400.
	res, _ = login("owner@example.com", "wrong-password")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login wrong password: status %d, want 401", res.StatusCode)
	}
	res, _ = call(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/auth/login", "{not json", "")
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("login malformed JSON: status %d, want 400", res.StatusCode)
	}
	res, _ = call(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/auth/login",
		`{"email": "a@b.c", "password": "x", "extra": 1}`, "")
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("login unknown field: status %d, want 400", res.StatusCode)
	}

	// Good credentials return the token.
	res, body := login("owner@example.com", "longenough1")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login: status %d, want 200", res.StatusCode)
	}
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatal("login response has no token")
	}

	// POST creates; the response echoes the stored todo.
	res, raw := call(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/todos",
		`{"title": "Write docs", "notes": "api section", "due_date": "2026-12-01"}`, token)
	body = decodeObject(t, raw)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST todos: status %d, want 201", res.StatusCode)
	}
	if body["title"] != "Write docs" || body["notes"] != "api section" || body["due_date"] != "2026-12-01" {
		t.Errorf("created todo = %v", body)
	}
	if body["completed"] != false {
		t.Errorf("created todo completed = %v, want false", body["completed"])
	}

	// Validation failures are 422 with the API's error shape.
	res, raw = call(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/todos",
		`{"title": "   "}`, token)
	body = decodeObject(t, raw)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("POST todos empty title: status %d, want 422", res.StatusCode)
	}
	if msg, _ := body["error"].(string); msg == "" {
		t.Errorf("validation error body = %v, want an error message", body)
	}
	res, _ = call(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/todos",
		`{"title": "x", "due_date": "01-12-2026"}`, token)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("POST todos bad date: status %d, want 422", res.StatusCode)
	}
	res, _ = call(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/todos",
		`{"title": "x", "surprise": true}`, token)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST todos unknown field: status %d, want 400", res.StatusCode)
	}

	// Seed a stable fixture set for list, sort, and PATCH assertions.
	for _, tc := range []string{
		`{"title": "Banana bread"}`,
		`{"title": "apple pie", "due_date": "2026-12-02"}`,
		`{"title": "Cherry cake", "due_date": "2026-12-01"}`,
	} {
		if res, _ = call(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/todos", tc, token); res.StatusCode != http.StatusCreated {
			t.Fatalf("POST todos %s: status %d, want 201", tc, res.StatusCode)
		}
	}

	// GET returns a flat list sorted by due date (unscheduled last), then
	// title, case-insensitively.
	res, raw = call(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/todos", "", token)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET todos: status %d, want 200", res.StatusCode)
	}
	list := decodeArray(t, raw)
	gotTitles := make([]string, 0, len(list))
	for _, item := range list {
		gotTitles = append(gotTitles, item["title"].(string))
	}
	wantTitles := []string{"Cherry cake", "Write docs", "apple pie", "Banana bread"}
	if !slices.Equal(gotTitles, wantTitles) {
		t.Errorf("list order = %v, want %v", gotTitles, wantTitles)
	}

	// PATCH is partial: each call below touches exactly the fields it sends.
	id := int64(list[0]["id"].(float64)) // Cherry cake
	path := fmt.Sprintf("%s/api/v1/todos/%d", ts.URL, id)

	res, raw = call(t, ts.Client(), http.MethodPatch, path, `{"title": "cherry tart"}`, token)
	body = decodeObject(t, raw)
	if res.StatusCode != http.StatusOK || body["title"] != "cherry tart" {
		t.Fatalf("PATCH title: (%d, %v)", res.StatusCode, body)
	}
	if body["due_date"] != "2026-12-01" || body["notes"] != nil {
		t.Errorf("PATCH title changed other fields: %v", body)
	}

	// notes → null clears; notes → string replaces.
	res, raw = call(t, ts.Client(), http.MethodPatch, path, `{"notes": null}`, token)
	body = decodeObject(t, raw)
	if res.StatusCode != http.StatusOK || body["notes"] != nil {
		t.Fatalf("PATCH notes null: (%d, %v)", res.StatusCode, body)
	}
	res, raw = call(t, ts.Client(), http.MethodPatch, path, `{"notes": "defrost cherries"}`, token)
	body = decodeObject(t, raw)
	if res.StatusCode != http.StatusOK || body["notes"] != "defrost cherries" {
		t.Fatalf("PATCH notes: (%d, %v)", res.StatusCode, body)
	}

	// due_date → null clears the due date.
	res, raw = call(t, ts.Client(), http.MethodPatch, path, `{"due_date": null}`, token)
	body = decodeObject(t, raw)
	if res.StatusCode != http.StatusOK || body["due_date"] != nil {
		t.Fatalf("PATCH due_date null: (%d, %v)", res.StatusCode, body)
	}

	// completed flips only when the desired state differs from the current
	// one, and carries completed_at with it.
	res, raw = call(t, ts.Client(), http.MethodPatch, path, `{"completed": true}`, token)
	body = decodeObject(t, raw)
	if res.StatusCode != http.StatusOK || body["completed"] != true {
		t.Fatalf("PATCH completed true: (%d, %v)", res.StatusCode, body)
	}
	if _, ok := body["completed_at"]; !ok {
		t.Errorf("completed todo has no completed_at: %v", body)
	}
	res, raw = call(t, ts.Client(), http.MethodPatch, path, `{"completed": false}`, token)
	body = decodeObject(t, raw)
	if res.StatusCode != http.StatusOK || body["completed"] != false {
		t.Fatalf("PATCH completed false: (%d, %v)", res.StatusCode, body)
	}

	// Combining fields in one PATCH merges onto the current todo.
	res, raw = call(t, ts.Client(), http.MethodPatch, path,
		`{"title": "Cherry cake", "due_date": "2026-12-05", "completed": true}`, token)
	body = decodeObject(t, raw)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PATCH combined: status %d, want 200", res.StatusCode)
	}
	if body["title"] != "Cherry cake" || body["due_date"] != "2026-12-05" || body["completed"] != true {
		t.Errorf("PATCH combined result = %v", body)
	}

	// PATCH and DELETE on an unknown id are 404.
	res, _ = call(t, ts.Client(), http.MethodPatch, ts.URL+"/api/v1/todos/9999", `{"title": "x"}`, token)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("PATCH unknown id: status %d, want 404", res.StatusCode)
	}
	res, _ = call(t, ts.Client(), http.MethodDelete, ts.URL+"/api/v1/todos/9999", "", token)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("DELETE unknown id: status %d, want 404", res.StatusCode)
	}

	// DELETE removes the todo; the id stays gone afterwards.
	res, _ = call(t, ts.Client(), http.MethodDelete, path, "", token)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE: status %d, want 204", res.StatusCode)
	}
	res, _ = call(t, ts.Client(), http.MethodDelete, path, "", token)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("DELETE twice: status %d, want 404", res.StatusCode)
	}

	// includeCompleted=true appends completed todos after the active ones,
	// both within the stable sort. Complete apple pie to make it
	// deterministic, then: active = Write docs (due first), Banana bread
	// (unscheduled last); completed = apple pie.
	applePieID := int64(list[2]["id"].(float64))
	res, _ = call(t, ts.Client(), http.MethodPatch, fmt.Sprintf("%s/api/v1/todos/%d", ts.URL, applePieID), `{"completed": true}`, token)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PATCH apple pie completed: status %d, want 200", res.StatusCode)
	}

	res, raw = call(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/todos?includeCompleted=true", "", token)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET todos includeCompleted: status %d, want 200", res.StatusCode)
	}
	list = decodeArray(t, raw)
	gotTitles = gotTitles[:0]
	for _, item := range list {
		gotTitles = append(gotTitles, item["title"].(string))
	}
	wantTitles = []string{"Write docs", "Banana bread", "apple pie"}
	if !slices.Equal(gotTitles, wantTitles) {
		t.Errorf("list with completed = %v, want %v", gotTitles, wantTitles)
	}
	// Without the flag only the active ones come back.
	_, raw = call(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/todos", "", token)
	list = decodeArray(t, raw)
	if len(list) != 2 {
		t.Errorf("active-only list length = %d, want 2", len(list))
	}

	// Logout revokes the presented token: the next use is 401.
	res, _ = call(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/auth/logout", "", token)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: status %d, want 204", res.StatusCode)
	}
	res, _ = call(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/todos", "", token)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("todos with revoked token: status %d, want 401", res.StatusCode)
	}
	// A fresh login issues a new, working token.
	res, body = login("owner@example.com", "longenough1")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("re-login: status %d, want 200", res.StatusCode)
	}
	token, _ = body["token"].(string)
	res, _ = call(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/todos", "", token)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("todos with fresh token: status %d, want 200", res.StatusCode)
	}
}

// TestAPILoginRateLimit answers 429 once the failed-attempt window fills.
// It runs its own server so the main test's generous limiter stays out of
// the way.
func TestAPILoginRateLimit(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	lockTestDB(t, ctx, pool)
	if err := db.Migrate(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE users, todos, api_tokens, sessions RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	hash, err := users.HashPassword("longenough1")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := users.NewStore(pool).Create(ctx, "limited@example.com", hash); err != nil {
		t.Fatalf("create user: %v", err)
	}

	app := New(pool, time.UTC, ratelimit.New(2, time.Minute))
	ts := httptest.NewServer(app.Routes())
	defer ts.Close()

	for range 2 {
		res, _ := call(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/auth/login",
			`{"email": "limited@example.com", "password": "wrong"}`, "")
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("failed login: status %d, want 401", res.StatusCode)
		}
	}
	res, raw := call(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/auth/login",
		`{"email": "limited@example.com", "password": "wrong"}`, "")
	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("third failed login: status %d, want 429", res.StatusCode)
	}
	if body := decodeObject(t, raw); len(body["error"].(string)) == 0 {
		t.Errorf("rate-limit body = %v, want an error message", body)
	}
}

// call performs one request against the test server and returns the
// response plus its raw body. bearer, when not "", is sent as the
// Authorization header.
func call(t *testing.T, client *http.Client, method, target, body, bearer string) (*http.Response, string) {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, target, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read %s %s body: %v", method, target, err)
	}
	return res, string(raw)
}

// decodeObject decodes a JSON object body.
func decodeObject(t *testing.T, raw string) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("body is not a JSON object: %v (%s)", err, raw)
	}
	return decoded
}

// decodeArray decodes a JSON array-of-objects body.
func decodeArray(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var decoded []map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("body is not a JSON array: %v (%s)", err, raw)
	}
	return decoded
}
