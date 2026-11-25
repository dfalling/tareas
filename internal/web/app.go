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

	"github.com/dfalling/tareas/internal/ratelimit"
	"github.com/dfalling/tareas/internal/todos"
	"github.com/dfalling/tareas/internal/tokens"
	"github.com/dfalling/tareas/internal/users"
)

//go:embed templates static
var assetFS embed.FS

// App holds the dependencies of the HTML UI.
type App struct {
	pool      *pgxpool.Pool
	users     *users.Store
	todos     *todos.Service
	tokens    *tokens.Store
	scs       *scs.SessionManager
	limiter   *ratelimit.Limiter
	locale    string
	loc       *time.Location
	csrf      *nosurf.CSRFHandler
	templates map[string]*template.Template
}

// New wires up the HTML UI. loc is the app timezone (APP_TIMEZONE): "today",
// due-date display, and the completed marker are all computed in it.
func New(pool *pgxpool.Pool, session *scs.SessionManager, locale string, loc *time.Location) *App {
	app := &App{
		pool:      pool,
		users:     users.NewStore(pool),
		todos:     todos.NewService(pool, loc),
		tokens:    tokens.NewStore(pool),
		scs:       session,
		limiter:   ratelimit.New(10, 5*time.Minute),
		locale:    resolveLocale(locale),
		loc:       loc,
		templates: parseTemplates(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/todos", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /setup", app.setupForm)
	mux.HandleFunc("POST /setup", app.setupSubmit)
	mux.HandleFunc("GET /login", app.loginForm)
	mux.HandleFunc("POST /login", app.loginSubmit)
	mux.HandleFunc("POST /logout", app.logout)
	mux.Handle("GET /todos", app.requireAuth(http.HandlerFunc(app.todoList)))
	mux.Handle("POST /todos", app.requireAuth(http.HandlerFunc(app.todoCreate)))
	mux.Handle("GET /todos/{id}/edit", app.requireAuth(http.HandlerFunc(app.todoEditForm)))
	mux.Handle("POST /todos/{id}/edit", app.requireAuth(http.HandlerFunc(app.todoEditSubmit)))
	mux.Handle("POST /todos/{id}/toggle", app.requireAuth(http.HandlerFunc(app.todoToggle)))
	mux.Handle("GET /todos/{id}/delete", app.requireAuth(http.HandlerFunc(app.todoDeleteConfirm)))
	mux.Handle("POST /todos/{id}/delete", app.requireAuth(http.HandlerFunc(app.todoDelete)))
	mux.Handle("GET /settings", app.requireAuth(http.HandlerFunc(app.settingsPage)))
	mux.Handle("POST /settings", app.requireAuth(http.HandlerFunc(app.settingsSubmit)))
	mux.Handle("GET /settings/tokens", app.requireAuth(http.HandlerFunc(app.tokensPage)))
	mux.Handle("POST /settings/tokens", app.requireAuth(http.HandlerFunc(app.tokensCreate)))
	mux.Handle("POST /settings/tokens/{id}/revoke", app.requireAuth(http.HandlerFunc(app.tokensRevoke)))
	// Only /static/* is served; templates and everything else in assetFS stay
	// unreachable because the mux pattern never routes those paths here.
	mux.Handle("GET /static/", http.FileServerFS(assetFS))

	// limitBody sits inside CSRF but outside the handlers: the CSRF check
	// parses the form body, so the cap must already apply there.
	app.csrf = nosurf.New(limitBody(mux))
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
	return a.scs.LoadAndSave(a.csrf)
}

func parseTemplates() map[string]*template.Template {
	pages := []string{"setup.html", "login.html", "todos.html", "edit.html", "delete.html", "tokens.html", "settings.html"}
	templates := make(map[string]*template.Template, len(pages))
	for _, page := range pages {
		t := template.Must(template.ParseFS(assetFS, "templates/base.html", "templates/"+page))
		templates[page] = t
	}
	return templates
}

// viewData carries everything every page needs, plus exactly one page
// payload (List, Edit, or Delete) that the page's content block consumes.
type viewData struct {
	Locale        string
	CSRF          string
	Title         string
	Authenticated bool
	Flash         string
	Error         string

	List     *listPage
	Edit     *editPage
	Delete   *deletePage
	Tokens   *tokensPage
	Settings *settingsPage
}

// tabLink is one entry of the tab navigation.
type tabLink struct {
	Label  string
	URL    string
	Active bool
}

// todoItem is one rendered todo row, with its labels prebuilt in Go so the
// templates hold no formatting logic. Titles are user data and are never
// translated; they are only inserted as-is (html/template escapes them).
type todoItem struct {
	ID         int64
	Title      string
	Completed  bool
	ToggleAria string
	Overdue    string // localized "overdue · dd-mm-yy"; empty when not overdue
	Done       string // localized "done today"/"done dd-mm-yy"; completed only
}

// itemListView is a flat list of todo rows plus what their forms need.
type itemListView struct {
	Items []todoItem
	View  todos.View
	CSRF  string
}

// dayGroupView is one Upcoming section: a day heading and its rows.
type dayGroupView struct {
	Label string
	Items itemListView
}

// createForm holds the inline create form's redisplay values after a
// validation error, so nothing the user typed is lost.
type createForm struct {
	Title   string
	Notes   string
	DueDate string
}

// listPage is the payload of the tabbed todo page: navigation, the active
// section, and the inline create form.
type listPage struct {
	View     todos.View
	Tabs     []tabLink
	Heading  string
	Active   itemListView   // today, unscheduled, and completed tabs
	Upcoming []dayGroupView // upcoming tab: one group per day
	Create   createForm
}

// editPage is the payload of the per-todo edit page.
type editPage struct {
	ID      int64
	Title   string
	Notes   string
	DueDate string // ISO yyyy-mm-dd for <input type="date">; empty = none
	View    todos.View
}

// deletePage is the payload of the delete confirmation page.
type deletePage struct {
	ID       int64
	Question string // prebuilt from a whole-sentence key + the todo's title
	View     todos.View
}

// tokenRow is one rendered API-token row on the settings page. Display
// labels are prebuilt in Go so the templates hold no formatting logic. The
// plaintext token is never sent anywhere but the one-time flash after
// creation.
type tokenRow struct {
	ID         int64
	Prefix     string
	Name       string
	Created    string // localized "created <date>"
	RevokeAria string // localized "revoke <name>"
}

// tokensPage is the payload of the API-token settings page.
type tokensPage struct {
	Tokens []tokenRow
	Form   tokensForm
}

// tokensForm holds the create form's redisplay value after a validation
// error, so nothing the user typed is lost.
type tokensForm struct {
	Name string
}

// settingsPage is the payload of the general settings page. AutoDeleteDays
// is the input's value: "" = never delete, otherwise the configured number
// of days (also used to redisplay what was typed after a validation error).
type settingsPage struct {
	AutoDeleteDays string
}

// T translates a UI key for the page's locale. A method (not a func field)
// because text/template can only invoke methods with arguments.
func (v viewData) T(key string) string {
	return translator(v.Locale)(key)
}

func (a *App) viewData(r *http.Request) viewData {
	return viewData{
		Locale:        a.locale,
		CSRF:          nosurf.Token(r),
		Authenticated: a.scs.GetBool(r.Context(), sessionKeyAuthenticated),
		Flash:         a.scs.PopString(r.Context(), sessionKeyFlash),
	}
}

// t and tf are the handlers' shortcuts for translating plain keys and
// parameterized whole-sentence keys.
func (a *App) t(key string) string {
	return translator(a.locale)(key)
}

func (a *App) tf(key string, data map[string]string) string {
	return translatef(a.locale, key, data)
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
