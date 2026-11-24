package todos

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dfalling/tareas/internal/todos/todosdb"
)

// Service is the todo logic shared by the HTML UI and the JSON API: both
// front-ends call these methods, so behavior exists exactly once. It is also
// the seam handler tests fake (each consumer defines the interface it needs).
type Service struct {
	q   *todosdb.Queries
	loc *time.Location
}

// NewService returns a service backed by pool. loc is the app timezone
// (APP_TIMEZONE); "today" is always computed in it, never by SQL.
func NewService(pool *pgxpool.Pool, loc *time.Location) *Service {
	return &Service{q: todosdb.New(pool), loc: loc}
}

// Today returns the calendar date of now in the app timezone, normalized to
// UTC midnight so it compares exactly with stored due dates.
func (s *Service) Today(now time.Time) time.Time {
	return todayIn(s.loc, now)
}

// todayIn is the pure heart of Today, tested against fixed clocks.
func todayIn(loc *time.Location, now time.Time) time.Time {
	y, m, d := now.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Create inserts a new todo.
func (s *Service) Create(ctx context.Context, userID int64, in CreateInput) (Todo, error) {
	title, notes, err := validate(in.Title, in.Notes)
	if err != nil {
		return Todo{}, err
	}
	row, err := s.q.CreateTodo(ctx, todosdb.CreateTodoParams{
		UserID:  userID,
		Title:   title,
		Notes:   notes,
		DueDate: normalizeDueDate(in.DueDate),
	})
	if err != nil {
		return Todo{}, err
	}
	return todoFromRow(row), nil
}

// ByID returns one todo of this user. A missing todo — or another user's,
// since every query is scoped by user_id even in a single-user app — is
// ErrNotFound.
func (s *Service) ByID(ctx context.Context, userID, id int64) (Todo, error) {
	row, err := s.q.TodoByID(ctx, todosdb.TodoByIDParams{ID: id, UserID: userID})
	return todoOrNotFound(row, err)
}

// List returns every todo of the user, alphabetically. The web UI calls this
// once per request and slices the result into tab sections in Go.
func (s *Service) List(ctx context.Context, userID int64) ([]Todo, error) {
	rows, err := s.q.ListTodos(ctx, userID)
	if err != nil {
		return nil, err
	}
	return todosFromRows(rows), nil
}

// ListActive and ListCompleted return the API's flat lists with the stable
// due-date-then-title sort; the API handler appends the completed list when
// the client asks for it (?includeCompleted=true).
func (s *Service) ListActive(ctx context.Context, userID int64) ([]Todo, error) {
	rows, err := s.q.ListActiveTodos(ctx, userID)
	if err != nil {
		return nil, err
	}
	return todosFromRows(rows), nil
}

func (s *Service) ListCompleted(ctx context.Context, userID int64) ([]Todo, error) {
	rows, err := s.q.ListCompletedTodos(ctx, userID)
	if err != nil {
		return nil, err
	}
	return todosFromRows(rows), nil
}

// UpdateMeta rewrites title and notes. A partial update (the API PATCH)
// merges the fields it received onto the current todo before calling this.
func (s *Service) UpdateMeta(ctx context.Context, userID, id int64, title, notes string) (Todo, error) {
	t, n, err := validate(title, notes)
	if err != nil {
		return Todo{}, err
	}
	row, err := s.q.UpdateTodoMeta(ctx, todosdb.UpdateTodoMetaParams{
		ID:     id,
		Title:  t,
		Notes:  n,
		UserID: userID,
	})
	return todoOrNotFound(row, err)
}

// SetDueDate sets the due date; nil clears it.
func (s *Service) SetDueDate(ctx context.Context, userID, id int64, due *time.Time) (Todo, error) {
	row, err := s.q.SetTodoDueDate(ctx, todosdb.SetTodoDueDateParams{
		ID:      id,
		DueDate: normalizeDueDate(due),
		UserID:  userID,
	})
	return todoOrNotFound(row, err)
}

// Toggle flips completed. Completing stamps completed_at; reopening clears it.
func (s *Service) Toggle(ctx context.Context, userID, id int64) (Todo, error) {
	row, err := s.q.ToggleTodo(ctx, todosdb.ToggleTodoParams{ID: id, UserID: userID})
	return todoOrNotFound(row, err)
}

// Delete hard-deletes the todo. It reports ErrNotFound when there was nothing
// to delete, so the API can answer 404 instead of pretending success.
func (s *Service) Delete(ctx context.Context, userID, id int64) error {
	n, err := s.q.DeleteTodo(ctx, todosdb.DeleteTodoParams{ID: id, UserID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func todoOrNotFound(row todosdb.Todo, err error) (Todo, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return Todo{}, ErrNotFound
	}
	if err != nil {
		return Todo{}, err
	}
	return todoFromRow(row), nil
}

func todoFromRow(row todosdb.Todo) Todo {
	return Todo{
		ID:          row.ID,
		UserID:      row.UserID,
		Title:       row.Title,
		Notes:       row.Notes,
		DueDate:     row.DueDate,
		Completed:   row.Completed,
		CompletedAt: row.CompletedAt,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func todosFromRows(rows []todosdb.Todo) []Todo {
	if rows == nil {
		return nil
	}
	out := make([]Todo, len(rows))
	for i, row := range rows {
		out[i] = todoFromRow(row)
	}
	return out
}
