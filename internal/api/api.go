// Package api serves the JSON REST API under /api/v1. It is authenticated by
// bearer tokens only — cookies never authenticate these routes, which is what
// makes CSRF irrelevant here (the boundary rule from the plan). It therefore
// runs on its own mux with neither session nor CSRF middleware, mounted under
// the root mux before the HTML UI.
package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dfalling/tareas/internal/ratelimit"
	"github.com/dfalling/tareas/internal/todos"
	"github.com/dfalling/tareas/internal/tokens"
	"github.com/dfalling/tareas/internal/users"
)

// App holds the dependencies of the JSON API.
type App struct {
	users   *users.Store
	todos   *todos.Service
	tokens  *tokens.Store
	limiter *ratelimit.Limiter
}

// New wires up the API. loc is the app timezone (APP_TIMEZONE), shared with
// the web UI so date logic behaves identically on both front-ends. limiter
// throttles failed logins by IP and account, as the web login does.
func New(pool *pgxpool.Pool, loc *time.Location, limiter *ratelimit.Limiter) *App {
	return &App{
		users:   users.NewStore(pool),
		todos:   todos.NewService(pool, loc),
		tokens:  tokens.NewStore(pool),
		limiter: limiter,
	}
}

// Routes returns the API handler: bearer-token authentication around the
// routes. Login is outside it — it is how a client obtains a token.
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.Handle("POST /api/v1/auth/logout", a.requireToken(http.HandlerFunc(a.logout)))
	mux.Handle("GET /api/v1/todos", a.requireToken(http.HandlerFunc(a.todoList)))
	mux.Handle("POST /api/v1/todos", a.requireToken(http.HandlerFunc(a.todoCreate)))
	mux.Handle("PATCH /api/v1/todos/{id}", a.requireToken(http.HandlerFunc(a.todoPatch)))
	mux.Handle("DELETE /api/v1/todos/{id}", a.requireToken(http.HandlerFunc(a.todoDelete)))
	return mux
}

// Context keys for values authenticate attaches to the request.
type ctxKey int

const (
	ctxKeyUserID ctxKey = iota
	ctxKeyToken
)

// requireToken resolves the Authorization bearer token to a stored token and
// user, answering 401 JSON when it is missing, unknown, or expired.
func (a *App) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plaintext := bearerToken(r)
		if plaintext == "" {
			a.unauthorized(w)
			return
		}
		tok, err := a.tokens.Authenticate(r.Context(), plaintext)
		switch {
		case errors.Is(err, tokens.ErrNotFound), errors.Is(err, tokens.ErrExpired):
			a.unauthorized(w)
			return
		case err != nil:
			a.serverError(w, err)
			return
		}
		// Metadata only; a bookkeeping failure never blocks the request.
		a.tokens.Touch(r.Context(), tok.ID)

		ctx := context.WithValue(r.Context(), ctxKeyUserID, tok.UserID)
		ctx = context.WithValue(ctx, ctxKeyToken, tok)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// bearerToken extracts the credentials from "Authorization: Bearer <token>".
// Anything else — missing header, wrong scheme, empty value — is "".
func bearerToken(r *http.Request) string {
	const scheme = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) <= len(scheme) || h[:len(scheme)] != scheme {
		return ""
	}
	return h[len(scheme):]
}

// userID is the authenticated account; requireToken guarantees it is set.
func userID(r *http.Request) int64 {
	return r.Context().Value(ctxKeyUserID).(int64)
}

// token is the presented credential; requireToken guarantees it is set.
func token(r *http.Request) tokens.Token {
	return r.Context().Value(ctxKeyToken).(tokens.Token)
}

// pathID parses the {id} path segment; anything that is not a positive
// integer is a 404 rather than a 500.
func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
