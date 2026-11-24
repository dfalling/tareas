package web

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dfalling/tareas/internal/todos"
	"github.com/dfalling/tareas/internal/users"
)

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
		users.VerifyDummy(password)
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

// userID returns the signed-in account; requireAuth guarantees it is set.
func (a *App) userID(r *http.Request) int64 {
	return a.scs.GetInt64(r.Context(), sessionKeyUserID)
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

// todoList renders the requested tab.
func (a *App) todoList(w http.ResponseWriter, r *http.Request) {
	a.renderList(w, r, todos.ParseView(r.URL.Query().Get("view")), http.StatusOK, "", nil)
}

// renderList fetches the user's todos once and renders the tab view. Failed
// POSTs re-render through the same path, with an error and the form values
// the user submitted, so nothing they typed is lost.
func (a *App) renderList(w http.ResponseWriter, r *http.Request, view todos.View, status int, errKey string, create *createForm) {
	all, err := a.todos.List(r.Context(), a.userID(r))
	if err != nil {
		a.serverError(w, err)
		return
	}
	today := a.todos.Today(time.Now())

	data := a.viewData(r)
	data.Title = a.t("tab_" + string(view))
	page := a.buildListPage(view, todos.SliceIntoTabs(all, today), today, data.CSRF)
	if create != nil {
		page.Create = *create
	}
	if errKey != "" {
		data.Error = a.t(errKey)
	}
	data.List = page
	a.render(w, status, "todos.html", data)
}

// buildListPage turns the sliced todos into the page payload: navigation,
// heading, and the active section with prebuilt display labels.
func (a *App) buildListPage(view todos.View, tabs todos.Tabs, today time.Time, csrf string) *listPage {
	page := &listPage{View: view, Tabs: a.tabsNav(view)}

	switch view {
	case todos.ViewUpcoming:
		page.Heading = a.t("tab_upcoming")
		for _, group := range tabs.Upcoming {
			page.Upcoming = append(page.Upcoming, dayGroupView{
				Label: a.dayLabel(group.Date, today),
				Items: itemListView{Items: a.items(group.Todos, today), View: view, CSRF: csrf},
			})
		}
	case todos.ViewUnscheduled:
		page.Heading = a.t("tab_unscheduled")
		page.Active = itemListView{Items: a.items(tabs.Unscheduled, today), View: view, CSRF: csrf}
	case todos.ViewCompleted:
		page.Heading = a.t("tab_completed")
		page.Active = itemListView{Items: a.items(tabs.Completed, today), View: view, CSRF: csrf}
	default: // ViewToday
		page.Heading = a.t("tab_today")
		page.Active = itemListView{Items: a.items(tabs.Today, today), View: view, CSRF: csrf}
	}
	return page
}

// tabsNav builds the four tab links with the active one marked.
func (a *App) tabsNav(view todos.View) []tabLink {
	views := []todos.View{todos.ViewToday, todos.ViewUpcoming, todos.ViewUnscheduled, todos.ViewCompleted}
	out := make([]tabLink, 0, len(views))
	for _, v := range views {
		out = append(out, tabLink{
			Label:  a.t("tab_" + string(v)),
			URL:    "/todos?view=" + string(v),
			Active: v == view,
		})
	}
	return out
}

// items converts stored todos into display rows. Display labels are built
// here so templates stay formatting-free.
func (a *App) items(list []todos.Todo, today time.Time) []todoItem {
	out := make([]todoItem, 0, len(list))
	for _, t := range list {
		item := todoItem{
			ID:         t.ID,
			Title:      t.Title,
			Completed:  t.Completed,
			ToggleAria: a.tf("toggle_aria", map[string]string{"Title": t.Title}),
			DeleteAria: a.tf("delete_aria", map[string]string{"Title": t.Title}),
		}
		if todos.IsOverdue(t, today) {
			item.Overdue = a.tf("todo_overdue", map[string]string{"Date": a.dateOf(t.DueDate)})
		}
		if t.Completed {
			item.Done = a.doneLabel(t.CompletedAt, today)
		}
		out = append(out, item)
	}
	return out
}

// dateOf renders a plain calendar date (stored as UTC midnight) in the
// locale's pattern. Due dates are dates, not instants: they must not be
// timezone-converted, or a UTC midnight shifts a day back behind UTC.
func (a *App) dateOf(d *time.Time) string {
	return d.Format(dateFormat(a.locale))
}

// doneLabel is the completed marker: "done today" or "done dd-mm-yy".
func (a *App) doneLabel(completedAt *time.Time, today time.Time) string {
	if completedAt == nil {
		return ""
	}
	completed := completedAt.In(a.loc)
	if y, m, d := completed.Date(); time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Equal(today) {
		return a.t("done_today")
	}
	return a.tf("done_on", map[string]string{"Date": completed.Format(dateFormat(a.locale))})
}

// dayLabel is an Upcoming group heading: "Tomorrow" for the next day, else
// the locale weekday and date ("Wed 25-11-26").
func (a *App) dayLabel(day, today time.Time) string {
	if day.Equal(today.AddDate(0, 0, 1)) {
		return a.t("day_tomorrow")
	}
	return a.t(weekdayKeys[int(day.Weekday())]) + " " + day.Format(dateFormat(a.locale))
}

// redirectURL is the PRG target for create/toggle/edit: back to the tab the
// action came from, anchored at the affected todo.
func redirectURL(view todos.View, id int64) string {
	return "/todos?view=" + string(view) + "#todo-" + strconv.FormatInt(id, 10)
}

// todoCreate handles the inline create form on the tab page.
func (a *App) todoCreate(w http.ResponseWriter, r *http.Request) {
	view := todos.ParseView(r.URL.Query().Get("view"))
	form := createForm{
		Title:   r.FormValue("title"),
		Notes:   r.FormValue("notes"),
		DueDate: strings.TrimSpace(r.FormValue("due_date")),
	}

	var due *time.Time
	if form.DueDate != "" {
		d, err := todos.ParseDate(form.DueDate)
		if err != nil {
			a.renderList(w, r, view, http.StatusUnprocessableEntity, "todos_invalid_date", &form)
			return
		}
		due = &d
	}

	created, err := a.todos.Create(r.Context(), a.userID(r), todos.CreateInput{
		Title:   form.Title,
		Notes:   form.Notes,
		DueDate: due,
	})
	if err != nil {
		if key, ok := validationKey(err); ok {
			a.renderList(w, r, view, http.StatusUnprocessableEntity, key, &form)
			return
		}
		a.serverError(w, err)
		return
	}
	http.Redirect(w, r, redirectURL(view, created.ID), http.StatusSeeOther)
}

// validationKey maps service validation errors to their message keys; ok is
// false for anything that is not a validation error (those are 500s).
func validationKey(err error) (key string, ok bool) {
	switch {
	case errors.Is(err, todos.ErrTitleRequired):
		return "todos_title_required", true
	case errors.Is(err, todos.ErrTitleTooLong):
		return "todos_title_too_long", true
	case errors.Is(err, todos.ErrNotesTooLong):
		return "todos_notes_too_long", true
	default:
		return "", false
	}
}

// todoToggle flips completion and returns to the same tab (PRG), anchored at
// the todo — which may have just moved to another tab, where the anchor
// simply resolves to nothing.
func (a *App) todoToggle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if _, err := a.todos.Toggle(r.Context(), a.userID(r), id); err != nil {
		if errors.Is(err, todos.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		a.serverError(w, err)
		return
	}
	view := todos.ParseView(r.URL.Query().Get("view"))
	http.Redirect(w, r, redirectURL(view, id), http.StatusSeeOther)
}

// todoEditForm renders the per-todo edit page.
func (a *App) todoEditForm(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	todo, err := a.todos.ByID(r.Context(), a.userID(r), id)
	if errors.Is(err, todos.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, err)
		return
	}
	a.renderEdit(w, r, editPage{
		ID:      todo.ID,
		Title:   todo.Title,
		Notes:   notesValue(todo.Notes),
		DueDate: dueValue(todo.DueDate),
		View:    todos.ParseView(r.URL.Query().Get("view")),
	}, http.StatusOK, "")
}

// todoEditSubmit saves title, notes, and due date as one edit. The
// clear-due-date button is a second submit: when pressed, the due date is
// cleared regardless of the field's contents.
func (a *App) todoEditSubmit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	view := todos.ParseView(r.URL.Query().Get("view"))

	title := r.FormValue("title")
	notes := r.FormValue("notes")
	dueRaw := strings.TrimSpace(r.FormValue("due_date"))
	clearDue := r.FormValue("clear_due") != ""

	page := editPage{ID: id, Title: title, Notes: notes, DueDate: dueRaw, View: view}

	var due *time.Time
	if !clearDue && dueRaw != "" {
		d, err := todos.ParseDate(dueRaw)
		if err != nil {
			a.renderEdit(w, r, page, http.StatusUnprocessableEntity, "todos_invalid_date")
			return
		}
		due = &d
	}

	updated, err := a.todos.Update(r.Context(), a.userID(r), id, todos.UpdateInput{
		Title:   title,
		Notes:   notes,
		DueDate: due,
	})
	if errors.Is(err, todos.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		if key, ok := validationKey(err); ok {
			a.renderEdit(w, r, page, http.StatusUnprocessableEntity, key)
			return
		}
		a.serverError(w, err)
		return
	}
	http.Redirect(w, r, redirectURL(view, updated.ID), http.StatusSeeOther)
}

// renderEdit renders the edit page with the given values — the normal path
// and the validation-failure path, which preserves what was typed.
func (a *App) renderEdit(w http.ResponseWriter, r *http.Request, page editPage, status int, errKey string) {
	data := a.viewData(r)
	data.Title = a.t("edit_title")
	data.Edit = &page
	if errKey != "" {
		data.Error = a.t(errKey)
	}
	a.render(w, status, "edit.html", data)
}

// todoDeleteConfirm renders the confirmation page for a hard delete.
func (a *App) todoDeleteConfirm(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	todo, err := a.todos.ByID(r.Context(), a.userID(r), id)
	if errors.Is(err, todos.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, err)
		return
	}
	data := a.viewData(r)
	data.Title = a.t("delete_title")
	data.Delete = &deletePage{
		ID:       todo.ID,
		Question: a.tf("delete_question", map[string]string{"Title": todo.Title}),
		View:     todos.ParseView(r.URL.Query().Get("view")),
	}
	a.render(w, http.StatusOK, "delete.html", data)
}

// todoDelete performs the hard delete and returns to the tab (PRG); the
// deleted todo gets no anchor.
func (a *App) todoDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	err := a.todos.Delete(r.Context(), a.userID(r), id)
	if errors.Is(err, todos.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, err)
		return
	}
	view := todos.ParseView(r.URL.Query().Get("view"))
	http.Redirect(w, r, "/todos?view="+string(view), http.StatusSeeOther)
}

// notesValue renders nullable notes as textarea content.
func notesValue(n *string) string {
	if n == nil {
		return ""
	}
	return *n
}

// dueValue renders a due date as the ISO value a date input expects.
func dueValue(d *time.Time) string {
	if d == nil {
		return ""
	}
	return d.Format("2006-01-02")
}

// validEmail is a deliberately minimal check: something@something. There is
// no email verification flow in a single-user app; the login itself is the
// proof of ownership.
func validEmail(email string) bool {
	email = strings.TrimSpace(email)
	at := strings.Index(email, "@")
	return at > 0 && at < len(email)-1 && !strings.Contains(email[at+1:], "@")
}
