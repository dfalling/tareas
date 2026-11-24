package api

import (
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/dfalling/tareas/internal/tokens"
	"github.com/dfalling/tareas/internal/users"
)

// loginRequest is the body of POST /api/v1/auth/login.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// login verifies credentials and issues a fresh bearer token. Like the web
// login it is rate-limited per IP and per account, and a failed lookup is
// answered with the same Argon2id work as a real one.
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := readJSON(w, r, &req); err != nil {
		errorJSON(w, http.StatusBadRequest, badRequest(err))
		return
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	ipKey := "ip:" + ip
	emailKey := "email:" + strings.ToLower(strings.TrimSpace(req.Email))

	// Behind a reverse proxy every attempt shares the proxy IP; for a
	// single-user instance the per-account key carries the protection.
	if !a.limiter.Allow(ipKey) || !a.limiter.Allow(emailKey) {
		errorJSON(w, http.StatusTooManyRequests, "too many attempts; try again later")
		return
	}

	u, err := a.users.ByEmail(r.Context(), req.Email)
	if errors.Is(err, users.ErrNotFound) {
		// Equalize timing with a real verification before failing.
		users.VerifyDummy(req.Password)
	}
	if err != nil && !errors.Is(err, users.ErrNotFound) {
		a.serverError(w, err)
		return
	}
	if err != nil || !users.VerifyPassword(req.Password, u.PasswordHash) {
		// Record failures by both IP and account to slow online guessing.
		a.limiter.Record(ipKey)
		a.limiter.Record(emailKey)
		errorJSON(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// One token per login; logout (or the settings page) revokes it.
	plaintext, _, err := a.tokens.Issue(r.Context(), u.ID, "api login", nil)
	if err != nil {
		a.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Token string `json:"token"`
	}{plaintext})
}

// logout revokes the presented token. Already-revoked tokens still log out
// cleanly: the outcome the client cares about is "this token no longer
// works".
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	tok := token(r)
	if err := a.tokens.Revoke(r.Context(), tok.UserID, tok.ID); err != nil {
		if !errors.Is(err, tokens.ErrNotFound) {
			a.serverError(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
