-- Named queries for the todos service, compiled to typed pgx/v5 code by sqlc
-- (see sqlc.yaml at the repo root).
--
-- Ordering is alphabetical everywhere: lower(title), title, id. There is no
-- position column — the plan fixes alphabetical as the app-wide order.

-- name: CreateTodo :one
INSERT INTO todos (user_id, title, notes, due_date)
VALUES ($1, $2, $3, $4)
RETURNING id, user_id, title, notes, due_date, completed, completed_at, created_at, updated_at;

-- name: TodoByID :one
SELECT id, user_id, title, notes, due_date, completed, completed_at, created_at, updated_at
FROM todos
WHERE id = $1 AND user_id = $2;

-- One SELECT per web request: the four tab sections are sliced and sorted in
-- Go by the todos package (pure functions), never with per-tab queries.
-- name: ListTodos :many
SELECT id, user_id, title, notes, due_date, completed, completed_at, created_at, updated_at
FROM todos
WHERE user_id = $1
ORDER BY lower(title), title, id;

-- The JSON API returns flat lists with a stable server-side sort:
-- due date first (unscheduled last), then title. Completed todos are appended
-- after the active ones when a client asks for them (includeCompleted).
-- name: ListActiveTodos :many
SELECT id, user_id, title, notes, due_date, completed, completed_at, created_at, updated_at
FROM todos
WHERE user_id = $1 AND NOT completed
ORDER BY due_date ASC NULLS LAST, lower(title), title, id;

-- name: ListCompletedTodos :many
SELECT id, user_id, title, notes, due_date, completed, completed_at, created_at, updated_at
FROM todos
WHERE user_id = $1 AND completed
ORDER BY due_date ASC NULLS LAST, lower(title), title, id;

-- Updates are explicit named statements instead of COALESCE tricks, so
-- "clear this field" and "don't touch it" can never be confused: each
-- statement writes exactly the columns it owns.

-- name: UpdateTodoMeta :one
UPDATE todos
SET title = $2, notes = $3, updated_at = now()
WHERE id = $1 AND user_id = $4
RETURNING id, user_id, title, notes, due_date, completed, completed_at, created_at, updated_at;

-- Passing NULL clears the due date.
-- name: SetTodoDueDate :one
UPDATE todos
SET due_date = $2, updated_at = now()
WHERE id = $1 AND user_id = $3
RETURNING id, user_id, title, notes, due_date, completed, completed_at, created_at, updated_at;

-- Inside SET, "completed" still refers to the pre-update row: completing sets
-- completed_at, reopening clears it.
-- name: ToggleTodo :one
UPDATE todos
SET completed = NOT completed,
    completed_at = CASE WHEN NOT completed THEN now() ELSE NULL END,
    updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING id, user_id, title, notes, due_date, completed, completed_at, created_at, updated_at;

-- RowsAffected reports whether the todo existed at all (0 = not found).
-- name: DeleteTodo :execrows
DELETE FROM todos
WHERE id = $1 AND user_id = $2;

-- Maintenance purge for auto-delete: hard-deletes this user's completed
-- todos completed before the cutoff (now minus the user's window in days).
-- RowsAffected reports how many rows went away, for the job's log line.
-- completed_at is never NULL on completed rows (toggle stamps it), so the
-- comparison is exact; active rows never match.
-- name: DeleteCompletedBefore :execrows
DELETE FROM todos
WHERE user_id = $1 AND completed AND completed_at < $2;
