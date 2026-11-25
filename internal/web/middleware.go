package web

import "net/http"

// Session keys used by the HTML UI.
const (
	sessionKeyAuthenticated = "authenticated"
	sessionKeyUserID        = "userID"
	sessionKeyFlash         = "flash"
)

// maxBodyBytes caps request bodies — the same ceiling the API applies to
// its JSON bodies: well above any legit form payload, far below anything
// that could pressure the server.
const maxBodyBytes = 1 << 20 // 1 MiB

// limitBody caps request bodies at maxBodyBytes. GETs have no body and are
// unaffected; oversized POSTs are rejected up front when the client declares
// Content-Length, and by the read error otherwise (for the web UI that means
// the CSRF check fails first, with a 400).
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxBodyBytes {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// requireAuth redirects unauthenticated requests to the login page.
func (a *App) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.scs.GetBool(r.Context(), sessionKeyAuthenticated) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
