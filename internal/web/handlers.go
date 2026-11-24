package web

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/dfalling/tareas/internal/users"
)

// dummyHash equalizes verification timing when the account does not exist,
// so login latency does not reveal whether an email is registered.
var dummyHash = func() string {
	h, _ := users.HashPassword("timing-equalizer-not-a-real-password")
	return h
}()

// setupForm renders the one-time bootstrap page; once an account exists it
// redirects to the login page.
func (a *App) setupForm(w http.ResponseWriter, r *http.Request) {
	n, err := a.users.Count(r.Context())
	if err != nil {
		a.serverError(w, err)
		return
	}
	if n > 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	data := a.viewData(r)
	data.Title = data.T("setup_title")
	a.render(w, http.StatusOK, "setup.html", data)
}

// setupSubmit creates the first account, then logs the user in. It is only
// reachable while the users table is empty.
func (a *App) setupSubmit(w http.ResponseWriter, r *http.Request) {
	n, err := a.users.Count(r.Context())
	if err != nil {
		a.serverError(w, err)
		return
	}
	if n > 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	email := r.FormValue("email")
	password := r.FormValue("password")

	data := a.viewData(r)
	data.Title = data.T("setup_title")

	switch {
	case !validEmail(email):
		data.Error = data.T("setup_invalid_email")
		a.render(w, http.StatusUnprocessableEntity, "setup.html", data)
		return
	case utf8.RuneCountInString(password) < 8:
		data.Error = data.T("setup_short_password")
		a.render(w, http.StatusUnprocessableEntity, "setup.html", data)
		return
	}

	hash, err := users.HashPassword(password)
	if err != nil {
		a.serverError(w, err)
		return
	}
	u, err := a.users.Create(r.Context(), email, hash)
	if err != nil {
		a.serverError(w, err)
		return
	}

	if err := a.scs.RenewToken(r.Context()); err != nil {
		a.serverError(w, err)
		return
	}
	a.scs.Put(r.Context(), sessionKeyUserID, u.ID)
	a.scs.Put(r.Context(), sessionKeyAuthenticated, true)
	a.scs.Put(r.Context(), sessionKeyFlash, data.T("flash_account_created"))
	http.Redirect(w, r, "/todos", http.StatusSeeOther)
}

// loginForm renders the login page.
func (a *App) loginForm(w http.ResponseWriter, r *http.Request) {
	data := a.viewData(r)
	data.Title = data.T("login_title")
	a.render(w, http.StatusOK, "login.html", data)
}

// loginSubmit authenticates against the users table and starts a session.
func (a *App) loginSubmit(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	ipKey := "ip:" + ip
	emailKey := "email:" + strings.ToLower(strings.TrimSpace(email))

	// Behind a reverse proxy every attempt shares the proxy IP; for a
	// single-user instance the per-account key carries the protection.
	if !a.limiter.Allow(ipKey) || !a.limiter.Allow(emailKey) {
		data := a.viewData(r)
		data.Title = data.T("login_title")
		data.Error = data.T("login_rate_limited")
		a.render(w, http.StatusTooManyRequests, "login.html", data)
		return
	}

	u, err := a.users.ByEmail(r.Context(), email)
	if errors.Is(err, users.ErrNotFound) {
		// Equalize timing with a real verification before failing.
		_ = users.VerifyPassword(password, dummyHash)
	}
	if err != nil && !errors.Is(err, users.ErrNotFound) {
		a.serverError(w, err)
		return
	}
	if err != nil || !users.VerifyPassword(password, u.PasswordHash) {
		// Record failures by both IP and account to slow online guessing.
		a.limiter.Record(ipKey)
		a.limiter.Record(emailKey)
		data := a.viewData(r)
		data.Title = data.T("login_title")
		data.Error = data.T("login_invalid")
		a.render(w, http.StatusOK, "login.html", data)
		return
	}

	if err := a.scs.RenewToken(r.Context()); err != nil {
		a.serverError(w, err)
		return
	}
	a.scs.Put(r.Context(), sessionKeyUserID, u.ID)
	a.scs.Put(r.Context(), sessionKeyAuthenticated, true)
	http.Redirect(w, r, "/todos", http.StatusSeeOther)
}

// logout destroys the session.
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if err := a.scs.Destroy(r.Context()); err != nil {
		a.serverError(w, err)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// todosPage is a placeholder until the tabbed todo UI is built.
func (a *App) todosPage(w http.ResponseWriter, r *http.Request) {
	data := a.viewData(r)
	data.Title = data.T("todos_title")
	a.render(w, http.StatusOK, "todos.html", data)
}

// validEmail is a deliberately minimal check: something@something. There is
// no email verification flow in a single-user app; the login itself is the
// proof of ownership.
func validEmail(email string) bool {
	email = strings.TrimSpace(email)
	at := strings.Index(email, "@")
	return at > 0 && at < len(email)-1 && !strings.Contains(email[at+1:], "@")
}
