package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/dfalling/tareas/internal/todos"
)

// todoJSON is the wire form of a todo. due_date is an ISO calendar date (or
// null) — never a timestamp, matching how it is stored; the instants are
// RFC3339 timestamps.
type todoJSON struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title"`
	Notes       *string    `json:"notes"`
	DueDate     *string    `json:"due_date"`
	Completed   bool       `json:"completed"`
	CompletedAt *time.Time `json:"completed_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func todoToJSON(t todos.Todo) todoJSON {
	j := todoJSON{
		ID:          t.ID,
		Title:       t.Title,
		Notes:       t.Notes,
		Completed:   t.Completed,
		CompletedAt: t.CompletedAt,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
	if t.DueDate != nil {
		d := t.DueDate.Format("2006-01-02")
		j.DueDate = &d
	}
	return j
}

// todosToJSON always returns a JSON array (never null): an empty list is [].
func todosToJSON(list []todos.Todo) []todoJSON {
	out := make([]todoJSON, 0, len(list))
	for _, t := range list {
		out = append(out, todoToJSON(t))
	}
	return out
}

// validationMessage maps service validation errors to their API message;
// ok is false for anything that is not a validation error (those are 500s).
func validationMessage(err error) (msg string, ok bool) {
	switch {
	case errors.Is(err, todos.ErrTitleRequired):
		return "title is required", true
	case errors.Is(err, todos.ErrTitleTooLong):
		return "title is too long", true
	case errors.Is(err, todos.ErrNotesTooLong):
		return "notes are too long", true
	default:
		return "", false
	}
}

// notFound answers 404 for an unknown id — including another user's, since
// every query is scoped by user_id even in a single-user app.
func notFound(w http.ResponseWriter) {
	errorJSON(w, http.StatusNotFound, "todo not found")
}

// todoList returns the user's todos as one flat list in the stable
// server-side sort (due date first, unscheduled last, then title).
// ?includeCompleted=true appends the completed todos after the active ones.
func (a *App) todoList(w http.ResponseWriter, r *http.Request) {
	uid := userID(r)
	active, err := a.todos.ListActive(r.Context(), uid)
	if err != nil {
		a.serverError(w, err)
		return
	}

	var completed []todos.Todo
	if include, _ := strconv.ParseBool(r.URL.Query().Get("includeCompleted")); include {
		if completed, err = a.todos.ListCompleted(r.Context(), uid); err != nil {
			a.serverError(w, err)
			return
		}
	}

	writeJSON(w, http.StatusOK, todosToJSON(append(active, completed...)))
}

// createRequest is the body of POST /api/v1/todos. A nil Notes or DueDate is
// "not provided".
type createRequest struct {
	Title   string  `json:"title"`
	Notes   *string `json:"notes"`
	DueDate *string `json:"due_date"`
}

// todoCreate adds a todo and returns it with a 201.
func (a *App) todoCreate(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := readJSON(w, r, &req); err != nil {
		errorJSON(w, http.StatusBadRequest, badRequest(err))
		return
	}

	due, err := parseDueDate(req.DueDate)
	if err != nil {
		errorJSON(w, http.StatusUnprocessableEntity, "due_date must be an ISO YYYY-MM-DD date")
		return
	}

	created, err := a.todos.Create(r.Context(), userID(r), todos.CreateInput{
		Title:   req.Title,
		Notes:   derefString(req.Notes),
		DueDate: due,
	})
	if err != nil {
		if msg, ok := validationMessage(err); ok {
			errorJSON(w, http.StatusUnprocessableEntity, msg)
			return
		}
		a.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, todoToJSON(created))
}

// patchRequest holds the fields present in a PATCH body. Each hasX flag
// distinguishes "field absent" (leave alone) from "field null" (clear it) —
// the distinction the todos service's explicit named queries exist for.
type patchRequest struct {
	hasTitle    bool
	title       string
	hasNotes    bool
	notes       *string
	hasDueDate  bool
	dueDate     *time.Time
	hasComplete bool
	completed   bool
}

// parsePatchBody decodes the PATCH body as a field map so presence and null
// can be told apart. Unknown fields are rejected: a typo should fail loudly,
// not silently no-op.
func parsePatchBody(w http.ResponseWriter, r *http.Request) (patchRequest, bool) {
	var body map[string]json.RawMessage
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, badRequest(err))
		return patchRequest{}, false
	}

	var p patchRequest
	for key, raw := range body {
		switch key {
		case "title":
			if isNull(raw) {
				errorJSON(w, http.StatusBadRequest, "title cannot be null")
				return patchRequest{}, false
			}
			if err := json.Unmarshal(raw, &p.title); err != nil {
				errorJSON(w, http.StatusBadRequest, "title must be a string")
				return patchRequest{}, false
			}
			p.hasTitle = true
		case "notes":
			// null clears the notes; a string replaces them.
			if err := json.Unmarshal(raw, &p.notes); err != nil {
				errorJSON(w, http.StatusBadRequest, "notes must be a string or null")
				return patchRequest{}, false
			}
			p.hasNotes = true
		case "due_date":
			var rawDate *string
			if err := json.Unmarshal(raw, &rawDate); err != nil {
				errorJSON(w, http.StatusBadRequest, "due_date must be an ISO YYYY-MM-DD date or null")
				return patchRequest{}, false
			}
			due, err := parseDueDate(rawDate)
			if err != nil {
				errorJSON(w, http.StatusUnprocessableEntity, "due_date must be an ISO YYYY-MM-DD date")
				return patchRequest{}, false
			}
			p.hasDueDate = true
			p.dueDate = due
		case "completed":
			if isNull(raw) {
				errorJSON(w, http.StatusBadRequest, "completed cannot be null")
				return patchRequest{}, false
			}
			if err := json.Unmarshal(raw, &p.completed); err != nil {
				errorJSON(w, http.StatusBadRequest, "completed must be a boolean")
				return patchRequest{}, false
			}
			p.hasComplete = true
		default:
			errorJSON(w, http.StatusBadRequest, "unknown field "+strconv.Quote(key))
			return patchRequest{}, false
		}
	}
	return p, true
}

// isNull reports whether a raw JSON value is the null literal.
func isNull(raw json.RawMessage) bool {
	return string(raw) == "null"
}

// todoPatch applies a partial update: only the fields the body contains
// change. Completion is a flip, so a requested state that differs from the
// current one toggles the todo; title/notes/due_date merge onto the current
// values and save as one edit.
func (a *App) todoPatch(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		notFound(w)
		return
	}
	p, ok := parsePatchBody(w, r)
	if !ok {
		return
	}

	uid := userID(r)
	ctx := r.Context()

	current, err := a.todos.ByID(ctx, uid, id)
	if errors.Is(err, todos.ErrNotFound) {
		notFound(w)
		return
	}
	if err != nil {
		a.serverError(w, err)
		return
	}

	updated := current
	if p.hasComplete && p.completed != current.Completed {
		updated, err = a.todos.Toggle(ctx, uid, id)
		if err != nil {
			a.serverError(w, err)
			return
		}
	}

	if p.hasTitle || p.hasNotes || p.hasDueDate {
		in := todos.UpdateInput{
			Title:   updated.Title,
			Notes:   derefString(updated.Notes),
			DueDate: updated.DueDate,
		}
		if p.hasTitle {
			in.Title = p.title
		}
		if p.hasNotes {
			in.Notes = derefString(p.notes)
		}
		if p.hasDueDate {
			in.DueDate = p.dueDate
		}
		updated, err = a.todos.Update(ctx, uid, id, in)
		if errors.Is(err, todos.ErrNotFound) {
			notFound(w)
			return
		}
		if err != nil {
			if msg, ok := validationMessage(err); ok {
				errorJSON(w, http.StatusUnprocessableEntity, msg)
				return
			}
			a.serverError(w, err)
			return
		}
	}

	writeJSON(w, http.StatusOK, todoToJSON(updated))
}

// todoDelete hard-deletes the todo.
func (a *App) todoDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		notFound(w)
		return
	}
	err := a.todos.Delete(r.Context(), userID(r), id)
	if errors.Is(err, todos.ErrNotFound) {
		notFound(w)
		return
	}
	if err != nil {
		a.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseDueDate converts an optional ISO date string. nil is "not provided";
// a non-empty string that does not parse is the package's ErrInvalidDueDate.
func parseDueDate(s *string) (*time.Time, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	d, err := todos.ParseDate(*s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// derefString turns an optional string into the plain string the todos
// service takes; nil means "no notes".
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
