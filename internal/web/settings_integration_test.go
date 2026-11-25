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
	"github.com/dfalling/tareas/internal/users"
)

// TestSettingsFlow exercises the settings page against a real Postgres when
// TEST_DATABASE_URL is set: rendering the stored value, saving a window,
// validation failures with redisplay, and empty = never. It is skipped
// otherwise.
func TestSettingsFlow(t *testing.T) {
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

	app := New(pool, NewSessionManager(pool, false), "en", time.UTC)
	app.csrf.SetIsTLSFunc(func(*http.Request) bool { return false })
	ts := httptest.NewServer(app.Routes())
	defer ts.Close()

	noRedirect := func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar, CheckRedirect: noRedirect}

	// Create the account (this also logs the browser in).
	res := mustGet(t, client, ts.URL+"/setup")
	token := extractCSRF(t, readBody(t, res))
	res = postForm(t, client, ts.URL+"/setup", url.Values{
		"csrf_token": {token},
		"email":      {"me@example.com"},
		"password":   {"longenough1"},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /setup: status %d, want 303", res.StatusCode)
	}

	userStore := users.NewStore(pool)
	user, err := userStore.ByEmail(ctx, "me@example.com")
	if err != nil {
		t.Fatalf("lookup user: %v", err)
	}

	// The fresh account has no window: the input renders empty (= never).
	res = mustGet(t, client, ts.URL+"/settings")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /settings: status %d, want 200", res.StatusCode)
	}
	body := readBody(t, res)
	if !strings.Contains(body, `name="auto_delete_days" value=""`) {
		t.Errorf("fresh settings page should render an empty auto-delete input:\n%s", body)
	}

	post := func(days string) *http.Response {
		t.Helper()
		res := mustGet(t, client, ts.URL+"/settings")
		token := extractCSRF(t, readBody(t, res))
		return postForm(t, client, ts.URL+"/settings", url.Values{
			"csrf_token":       {token},
			"auto_delete_days": {days},
		})
	}

	// Saving a window stores it and flashes confirmation.
	res = post("7")
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/settings" {
		t.Fatalf("POST /settings 7: status %d location %q", res.StatusCode, res.Header.Get("Location"))
	}
	days, err := userStore.AutoDeleteDays(ctx, user.ID)
	if err != nil || days == nil || *days != 7 {
		t.Fatalf("auto-delete window = %v (err %v), want 7", days, err)
	}
	body = readBody(t, mustGet(t, client, ts.URL+"/settings"))
	if !strings.Contains(body, `value="7"`) || !strings.Contains(body, "Settings saved.") {
		t.Errorf("settings page after save should show 7 and a flash:\n%s", body)
	}

	// Invalid values are rejected with 422 and the typed value redisplayed.
	for _, invalid := range []string{"0", "-3", "abc", "3651"} {
		res = post(invalid)
		if res.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("POST /settings %q: status %d, want 422", invalid, res.StatusCode)
			continue
		}
		body := readBody(t, res)
		if !strings.Contains(body, "Enter a number of days between 1 and 3650") {
			t.Errorf("POST /settings %q: missing validation error:\n%s", invalid, body)
		}
		if !strings.Contains(body, `value="`+invalid+`"`) {
			t.Errorf("POST /settings %q: typed value not redisplayed:\n%s", invalid, body)
		}
		if got, _ := userStore.AutoDeleteDays(ctx, user.ID); got == nil || *got != 7 {
			t.Errorf("POST /settings %q: stored window changed to %v", invalid, got)
		}
	}

	// An empty field clears the window: never delete.
	res = post("")
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /settings empty: status %d, want 303", res.StatusCode)
	}
	if days, _ := userStore.AutoDeleteDays(ctx, user.ID); days != nil {
		t.Fatalf("auto-delete window = %v, want nil (never)", days)
	}
}
