package web

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dfalling/tareas/internal/db"
)

// TestTodoWebFlow drives the tabbed todo UI end to end against a real
// Postgres when TEST_DATABASE_URL is set (the CI test job provides one):
// create from the inline form, the four tabs, toggle with view propagation,
// the edit page (including the clear-due-date button), validation redisplay,
// and the delete confirmation. It is skipped otherwise.
func TestTodoWebFlow(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	// Cleanup order matters: lockTestDB's cleanup (registered below) must
	// release its connection — the advisory-lock holder — before Close.
	t.Cleanup(pool.Close)
	lockTestDB(t, ctx, pool)
	if err := db.Migrate(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE users, todos, api_tokens, sessions RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	app := New(pool, NewSessionManager(pool, false), "en", time.UTC)
	app.csrf.SetIsTLSFunc(func(*http.Request) bool { return false })
	ts := httptest.NewServer(app.Routes())
	defer ts.Close()

	// A fresh browser that does not follow redirects, so every PRG response
	// is visible.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	user := &http.Client{
		Jar: jar,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Bootstrap the account (the app is single-user; this test owns it).
	res := mustGet(t, user, ts.URL+"/setup")
	token := extractCSRF(t, readBody(t, res))
	res = postForm(t, user, ts.URL+"/setup", url.Values{
		"csrf_token": {token},
		"email":      {"todos@example.com"},
		"password":   {"longenough1"},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /setup: status %d, want 303", res.StatusCode)
	}

	// A signed-in page's CSRF token, reused for this session's POSTs.
	csrf := func(target string) string {
		return extractCSRF(t, readBody(t, mustGet(t, user, ts.URL+target)))
	}
	body := func(target string) string {
		return readBody(t, mustGet(t, user, ts.URL+target))
	}

	today := time.Now().UTC().Format("2006-01-02")
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")

	// An undated todo belongs to the unscheduled tab; the PRG redirect still
	// carries the tab the form was submitted from (the plan's rule).
	res = postForm(t, user, ts.URL+"/todos?view=today", url.Values{
		"csrf_token": {csrf("/todos")},
		"title":      {"Buy milk"},
	})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/todos?view=today#todo-1" {
		t.Fatalf("POST /todos: status %d location %q", res.StatusCode, res.Header.Get("Location"))
	}
	if !strings.Contains(body("/todos?view=unscheduled"), "Buy milk") {
		t.Fatal("undated todo not on the unscheduled tab")
	}
	if strings.Contains(body("/todos"), "Buy milk") {
		t.Fatal("undated todo leaked onto the today tab")
	}

	// Due today → today tab; due tomorrow → upcoming, under a heading; due
	// yesterday → today tab with the overdue marker.
	for _, tc := range []struct {
		title, due string
	}{
		{"Water plants", today},
		{"Plan week", tomorrow},
		{"Old chore", yesterday},
	} {
		res = postForm(t, user, ts.URL+"/todos?view=today", url.Values{
			"csrf_token": {csrf("/todos")},
			"title":      {tc.title},
			"due_date":   {tc.due},
		})
		if res.StatusCode != http.StatusSeeOther {
			t.Fatalf("POST /todos %q: status %d, want 303", tc.title, res.StatusCode)
		}
	}

	todayBody := body("/todos")
	if !strings.Contains(todayBody, "Water plants") || !strings.Contains(todayBody, "Old chore") {
		t.Fatal("today tab missing its dated todos")
	}
	if !strings.Contains(todayBody, "overdue ·") {
		t.Fatal("overdue todo without the overdue marker")
	}
	upcoming := body("/todos?view=upcoming")
	if !strings.Contains(upcoming, "Plan week") || !strings.Contains(upcoming, "Tomorrow") {
		t.Fatal("tomorrow's todo not grouped under Tomorrow on the upcoming tab")
	}

	// Toggle from the today tab: the redirect carries the tab through, and
	// the completed tab shows the done marker.
	res = postForm(t, user, ts.URL+"/todos/2/toggle?view=today", url.Values{
		"csrf_token": {csrf("/todos")},
	})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/todos?view=today#todo-2" {
		t.Fatalf("POST toggle: status %d location %q", res.StatusCode, res.Header.Get("Location"))
	}
	completed := body("/todos?view=completed")
	if !strings.Contains(completed, "Water plants") || !strings.Contains(completed, "done today") {
		t.Fatal("completed todo missing or without a done marker")
	}
	if strings.Contains(body("/todos"), "Water plants") {
		t.Fatal("completed todo still on the today tab")
	}

	// Edit rewrites title, notes, and due date in one PRG round trip.
	res = postForm(t, user, ts.URL+"/todos/1/edit?view=unscheduled", url.Values{
		"csrf_token": {csrf("/todos")},
		"title":      {"Buy oat milk"},
		"notes":      {"barista oat"},
	})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/todos?view=unscheduled#todo-1" {
		t.Fatalf("POST edit: status %d location %q", res.StatusCode, res.Header.Get("Location"))
	}
	if !strings.Contains(body("/todos?view=unscheduled"), "Buy oat milk") {
		t.Fatal("edited title not rendered")
	}

	// The clear-due-date button moves a dated todo to unscheduled.
	res = postForm(t, user, ts.URL+"/todos/3/edit?view=upcoming", url.Values{
		"csrf_token": {csrf("/todos")},
		"title":      {"Plan week"},
		"due_date":   {tomorrow},
		"clear_due":  {"1"},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST edit clear due: status %d, want 303", res.StatusCode)
	}
	if !strings.Contains(body("/todos?view=unscheduled"), "Plan week") {
		t.Fatal("todo with cleared due date not on the unscheduled tab")
	}

	// Invalid creates re-render with a 422, the error, and the typed title.
	res = postForm(t, user, ts.URL+"/todos?view=today", url.Values{
		"csrf_token": {csrf("/todos")},
		"title":      {"   "},
	})
	if res.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(readBody(t, res), "Give the todo a title.") {
		t.Fatalf("POST /todos blank title: status %d, want 422 with message", res.StatusCode)
	}
	res = postForm(t, user, ts.URL+"/todos?view=today", url.Values{
		"csrf_token": {csrf("/todos")},
		"title":      {"Milk"},
		"due_date":   {"24-11-2026"},
	})
	failed := readBody(t, res)
	if res.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(failed, "Enter a valid date") || !strings.Contains(failed, `value="Milk"`) {
		t.Fatalf("POST /todos bad date: status %d, want 422 with message and preserved title", res.StatusCode)
	}

	// Delete confirm page names the todo; confirming removes it for good.
	if !strings.Contains(body("/todos/1/delete?view=unscheduled"), "Delete &#39;Buy oat milk&#39;?") {
		t.Fatal("delete confirmation does not name the todo")
	}
	res = postForm(t, user, ts.URL+"/todos/1/delete?view=unscheduled", url.Values{
		"csrf_token": {csrf("/todos")},
	})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/todos?view=unscheduled" {
		t.Fatalf("POST delete: status %d location %q", res.StatusCode, res.Header.Get("Location"))
	}
	if strings.Contains(body("/todos?view=unscheduled"), "Buy oat milk") {
		t.Fatal("deleted todo still rendered")
	}
	for _, target := range []string{"/todos/1/edit", "/todos/1/delete"} {
		if res := mustGet(t, user, ts.URL+target); res.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s after delete: status %d, want 404", target, res.StatusCode)
		}
	}
	if res := postForm(t, user, ts.URL+"/todos/1/toggle?view=today", url.Values{"csrf_token": {csrf("/todos")}}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("POST toggle after delete: status %d, want 404", res.StatusCode)
	}
}
