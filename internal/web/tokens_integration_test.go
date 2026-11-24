package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dfalling/tareas/internal/api"
	"github.com/dfalling/tareas/internal/db"
	"github.com/dfalling/tareas/internal/ratelimit"
)

// TestTokensSettings exercises the API-token settings page against a real
// Postgres when TEST_DATABASE_URL is set (the CI test job provides one):
// listing, creating (with the plaintext shown exactly once), revoking, and
// the cookie/bearer boundary — a valid web session must NOT authenticate
// /api/v1, and the token created through the page must. It is skipped
// otherwise.
func TestTokensSettings(t *testing.T) {
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
	// the one holding the advisory lock — before the pool closes.
	t.Cleanup(pool.Close)
	lockTestDB(t, ctx, pool)
	if err := db.Migrate(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE users, todos, api_tokens, sessions RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	// Mount both front-ends exactly like main.go does, so the test hits the
	// real routing boundary.
	webApp := New(pool, NewSessionManager(pool, false), "en", time.UTC)
	// nosurf defaults to assuming TLS (Secure CSRF cookie), which a
	// plain-http httptest server cannot carry; disable for the test.
	webApp.csrf.SetIsTLSFunc(func(*http.Request) bool { return false })
	apiApp := api.New(pool, time.UTC, ratelimit.New(1000, time.Minute))
	root := http.NewServeMux()
	root.Handle("/api/v1/", apiApp.Routes())
	root.Handle("/", webApp.Routes())
	ts := httptest.NewServer(root)
	defer ts.Close()

	noRedirect := func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	owner := &http.Client{Jar: jar, CheckRedirect: noRedirect}

	// Bootstrap the account and sign in, ending with a live session cookie.
	res := postForm(t, owner, ts.URL+"/setup", url.Values{
		"csrf_token": {extractCSRF(t, readBody(t, mustGet(t, owner, ts.URL+"/setup")))},
		"email":      {"me@example.com"},
		"password":   {"longenough1"},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /setup: status %d, want 303", res.StatusCode)
	}

	// BOUNDARY: the authenticated session cookie must not authenticate the
	// API — bearer tokens only.
	res = mustGet(t, owner, ts.URL+"/api/v1/todos")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/todos with session cookie: status %d, want 401", res.StatusCode)
	}
	res = postForm(t, owner, ts.URL+"/api/v1/todos", url.Values{"title": {"sneaky"}})
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST /api/v1/todos with session cookie: status %d, want 401", res.StatusCode)
	}

	// The settings page lists nothing yet.
	res = mustGet(t, owner, ts.URL+"/settings/tokens")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /settings/tokens: status %d, want 200", res.StatusCode)
	}
	page := readBody(t, res)
	if !strings.Contains(page, "No API tokens yet.") {
		t.Errorf("empty settings page missing empty message:\n%s", page)
	}

	// Creating a token flashes the plaintext exactly once and lists the
	// record afterwards.
	res = postForm(t, owner, ts.URL+"/settings/tokens", url.Values{
		"csrf_token": {extractCSRF(t, page)},
		"name":       {"laptop"},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /settings/tokens: status %d, want 303", res.StatusCode)
	}
	flash := readBody(t, mustGet(t, owner, ts.URL+"/settings/tokens"))
	token := extractToken(t, flash)
	if token == "" {
		t.Fatalf("no plaintext token in flash:\n%s", flash)
	}
	if !strings.Contains(flash, "it will not be shown again") {
		t.Errorf("flash does not carry the one-time warning:\n%s", flash)
	}

	// The next render no longer shows the token.
	page = readBody(t, mustGet(t, owner, ts.URL+"/settings/tokens"))
	if strings.Contains(page, token) {
		t.Error("plaintext token rendered again after the flash")
	}
	if !strings.Contains(page, "laptop") || !strings.Contains(page, token[:8]) {
		t.Errorf("token list missing name or prefix:\n%s", page)
	}

	// The token from the page authenticates the API.
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/todos", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err = owner.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/todos with page token: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/todos with page token: status %d, want 200", res.StatusCode)
	}
	_ = res.Body.Close()

	// An empty or over-long name re-renders with the validation error and
	// the typed value preserved.
	res = postForm(t, owner, ts.URL+"/settings/tokens", url.Values{
		"csrf_token": {extractCSRF(t, page)},
		"name":       {"   "},
	})
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("POST /settings/tokens empty name: status %d, want 422", res.StatusCode)
	}
	if page = readBody(t, res); !strings.Contains(page, "Give the token a name.") {
		t.Errorf("empty-name page missing error:\n%s", page)
	}
	res = postForm(t, owner, ts.URL+"/settings/tokens", url.Values{
		"csrf_token": {extractCSRF(t, page)},
		"name":       {"  padded  "},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /settings/tokens padded name: status %d, want 303", res.StatusCode)
	}
	page = readBody(t, mustGet(t, owner, ts.URL+"/settings/tokens"))
	if extractToken(t, page) == "" {
		t.Error("second create did not flash a plaintext token")
	}
	if !strings.Contains(page, "padded") {
		t.Errorf("token name not trimmed before storage:\n%s", page)
	}

	// Revoking removes the row and revokes the credential.
	id := extractTokenID(t, page, "laptop")
	res = postForm(t, owner, fmt.Sprintf("%s/settings/tokens/%d/revoke", ts.URL, id), url.Values{
		"csrf_token": {extractCSRF(t, page)},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST revoke: status %d, want 303", res.StatusCode)
	}
	page = readBody(t, mustGet(t, owner, ts.URL+"/settings/tokens"))
	if strings.Contains(page, `<span class="name">laptop</span>`) {
		t.Errorf("revoked token still listed:\n%s", page)
	}

	// The revoked token no longer authenticates the API.
	req, err = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/todos", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err = owner.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/todos with revoked token: %v", err)
	}
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/todos with revoked token: status %d, want 401", res.StatusCode)
	}
	_ = res.Body.Close()

	// The API login path issues tokens too; they appear on the page.
	req, err = http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/login",
		strings.NewReader(`{"email": "me@example.com", "password": "longenough1"}`))
	if err != nil {
		t.Fatalf("build login: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = owner.Do(req)
	if err != nil {
		t.Fatalf("POST /api/v1/auth/login: %v", err)
	}
	var loginBody struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&loginBody); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	_ = res.Body.Close()
	if loginBody.Token == "" {
		t.Fatal("api login returned no token")
	}
	page = readBody(t, mustGet(t, owner, ts.URL+"/settings/tokens"))
	if !strings.Contains(page, "api login") {
		t.Errorf("api-login token not listed on the settings page:\n%s", page)
	}
}

// flashedToken matches the one-time flash: the fixed phrase followed by the
// 43-character base64url token.
var flashedToken = regexp.MustCompile(`it will not be shown again: ([A-Za-z0-9_-]{43})`)

// extractToken scrapes the flashed plaintext token out of the flash message.
// It returns "" when no token was flashed.
func extractToken(t *testing.T, page string) string {
	t.Helper()
	m := flashedToken.FindStringSubmatch(page)
	if m == nil {
		return ""
	}
	return m[1]
}

// extractTokenID finds the id of the revoke form for the named token. Each
// token renders as one <li> holding its name and the revoke form's action
// /settings/tokens/{id}/revoke.
func extractTokenID(t *testing.T, page, name string) int64 {
	t.Helper()
	for _, chunk := range strings.Split(page, "<li") {
		if !strings.Contains(chunk, name) {
			continue
		}
		m := regexp.MustCompile(`/settings/tokens/(\d+)/revoke`).FindStringSubmatch(chunk)
		if m == nil {
			break
		}
		id, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			t.Fatalf("parse token id from %q: %v", m[1], err)
		}
		return id
	}
	t.Fatalf("no revoke form found for token %q in:\n%s", name, page)
	return 0
}
