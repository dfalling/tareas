// Package web contains the server-rendered HTML UI: handlers, templates,
// middleware, and view plumbing.
package web

import (
	"bytes"
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/justinas/nosurf"

	"github.com/dfalling/tareas/internal/users"
)

//go:embed templates
var templateFS embed.FS

// App holds the dependencies of the HTML UI.
type App struct {
	pool      *pgxpool.Pool
	users     *users.Store
	scs       *scs.SessionManager
	limiter   *Limiter
	locale    string
	templates map[string]*template.Template
}

// New wires up the HTML UI.
func New(pool *pgxpool.Pool, session *scs.SessionManager, locale string) *App {
	app := &App{
		pool:      pool,
		users:     users.NewStore(pool),
		scs:       session,
		limiter:   NewLimiter(10, 5*time.Minute),
		locale:    resolveLocale(locale),
		templates: parseTemplates(),
	}
	return app
}

// NewSessionManager returns the configured session manager backed by
// Postgres. secureCookies should be true whenever the app is served over
// HTTPS (browsers also accept Secure cookies on http://localhost).
func NewSessionManager(pool *pgxpool.Pool, secureCookies bool) *scs.SessionManager {
	session := scs.New()
	session.Store = pgxstore.New(pool)
	session.Lifetime = 7 * 24 * time.Hour
	session.Cookie.Name = "tareas_session"
	session.Cookie.HttpOnly = true
	session.Cookie.SameSite = http.SameSiteLaxMode
	session.Cookie.Secure = secureCookies
	return session
}

// Routes returns the HTTP handler for the HTML UI: session middleware
// outermost, CSRF protection inside it, then the routes.
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/todos", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /setup", a.setupForm)
	mux.HandleFunc("POST /setup", a.setupSubmit)
	mux.HandleFunc("GET /login", a.loginForm)
	mux.HandleFunc("POST /login", a.loginSubmit)
	mux.HandleFunc("POST /logout", a.logout)
	mux.Handle("GET /todos", a.requireAuth(http.HandlerFunc(a.todosPage)))

	return a.scs.LoadAndSave(nosurf.New(mux))
}

func parseTemplates() map[string]*template.Template {
	pages := []string{"setup.html", "login.html", "todos.html"}
	templates := make(map[string]*template.Template, len(pages))
	for _, page := range pages {
		t := template.Must(template.ParseFS(templateFS, "templates/base.html", "templates/"+page))
		templates[page] = t
	}
	return templates
}

// viewData carries everything the templates need.
type viewData struct {
	Locale        string
	T             func(string) string
	CSRF          string
	Title         string
	Authenticated bool
	Flash         string
	Error         string
}

func (a *App) viewData(r *http.Request) viewData {
	return viewData{
		Locale:        a.locale,
		T:             translator(a.locale),
		CSRF:          nosurf.Token(r),
		Authenticated: a.scs.GetBool(r.Context(), sessionKeyAuthenticated),
		Flash:         a.scs.PopString(r.Context(), sessionKeyFlash),
	}
}

func (a *App) render(w http.ResponseWriter, status int, page string, data viewData) {
	tmpl, ok := a.templates[page]
	if !ok {
		slog.Error("unknown template", "page", page)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base", data); err != nil {
		slog.Error("render template", "page", page, "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func (a *App) serverError(w http.ResponseWriter, err error) {
	slog.Error("server error", "err", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
