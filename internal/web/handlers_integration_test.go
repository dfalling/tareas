package web

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/dfalling/tareas/internal/db"
)

// TestAuthFlow exercises the full cookie flow against a real Postgres when
// TEST_DATABASE_URL is set (the CI test job provides one): setup bootstrap,
// authenticated access, closed bootstrap, failed and successful logins, and
// logout. It is skipped otherwise.
func TestAuthFlow(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE users, todos, api_tokens, sessions RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	app := New(pool, NewSessionManager(pool, false), "en")
	ts := httptest.NewServer(app.Routes())
	defer ts.Close()

	noRedirect := func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}

	// A fresh browser: unauthenticated /todos redirects to /login.
	browser := func() *http.Client {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("cookiejar: %v", err)
		}
		return &http.Client{Jar: jar, CheckRedirect: noRedirect}
	}
	owner := browser()
	stranger := browser()

	res := mustGet(t, owner, ts.URL+"/todos")
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/login" {
		t.Fatalf("GET /todos unauthenticated: status %d location %q", res.StatusCode, res.Header.Get("Location"))
	}

	// GET /setup renders the bootstrap form with a CSRF token.
	res = mustGet(t, owner, ts.URL+"/setup")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /setup: status %d, want 200", res.StatusCode)
	}
	token := extractCSRF(t, readBody(t, res))

	// POST /setup without a CSRF token is rejected by the CSRF middleware.
	res = postForm(t, owner, ts.URL+"/setup", url.Values{
		"email": {"me@example.com"}, "password": {"longenough1"},
	})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST /setup without CSRF: status %d, want 400", res.StatusCode)
	}

	// POST /setup with a token creates the account and logs the user in.
	res = postForm(t, owner, ts.URL+"/setup", url.Values{
		"csrf_token": {token},
		"email":      {"me@example.com"},
		"password":   {"longenough1"},
	})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/todos" {
		t.Fatalf("POST /setup: status %d location %q", res.StatusCode, res.Header.Get("Location"))
	}

	// The authenticated /todos page renders for the session that created it.
	res = mustGet(t, owner, ts.URL+"/todos")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /todos authenticated: status %d, want 200", res.StatusCode)
	}

	// The bootstrap is closed now: /setup redirects to /login.
	res = mustGet(t, owner, ts.URL+"/setup")
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("GET /setup after bootstrap: status %d, want 303", res.StatusCode)
	}

	// A second browser shares nothing.
	res = mustGet(t, stranger, ts.URL+"/todos")
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("GET /todos stranger: status %d, want 303", res.StatusCode)
	}

	// Wrong credentials re-render the form with an error.
	res = postForm(t, stranger, ts.URL+"/login", url.Values{
		"csrf_token": {extractCSRF(t, readBody(t, mustGet(t, stranger, ts.URL+"/login")))},
		"email":      {"me@example.com"},
		"password":   {"wrong-password"},
	})
	if res.StatusCode != http.StatusOK || !strings.Contains(readBody(t, res), "Wrong email or password") {
		t.Fatalf("POST /login wrong password: status %d", res.StatusCode)
	}

	// Correct credentials log in.
	res = postForm(t, stranger, ts.URL+"/login", url.Values{
		"csrf_token": {extractCSRF(t, readBody(t, mustGet(t, stranger, ts.URL+"/login")))},
		"email":      {"me@example.com"},
		"password":   {"longenough1"},
	})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/todos" {
		t.Fatalf("POST /login: status %d location %q", res.StatusCode, res.Header.Get("Location"))
	}
	res = mustGet(t, stranger, ts.URL+"/todos")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /todos after login: status %d, want 200", res.StatusCode)
	}

	// Logout ends the session.
	res = postForm(t, stranger, ts.URL+"/logout", url.Values{
		"csrf_token": {extractCSRF(t, readBody(t, mustGet(t, stranger, ts.URL+"/todos")))},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /logout: status %d, want 303", res.StatusCode)
	}
	res = mustGet(t, stranger, ts.URL+"/todos")
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("GET /todos after logout: status %d, want 303", res.StatusCode)
	}
}

func mustGet(t *testing.T, client *http.Client, target string) *http.Response {
	t.Helper()
	res, err := client.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

func postForm(t *testing.T, client *http.Client, target string, form url.Values) *http.Response {
	t.Helper()
	res, err := client.PostForm(target, form)
	if err != nil {
		t.Fatalf("POST %s: %v", target, err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

func readBody(t *testing.T, res *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}

func extractCSRF(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no csrf_token input in response body")
	}
	return m[1]
}
