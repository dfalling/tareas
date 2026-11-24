package web

import "net/http"

// Session keys used by the HTML UI.
const (
	sessionKeyAuthenticated = "authenticated"
	sessionKeyUserID        = "userID"
	sessionKeyFlash         = "flash"
)

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
