// Package todos holds the todo model and the service shared by the HTML UI
// and the JSON API, plus the pure tab and date logic both build on. Handlers
// never touch the todos tables directly; they call this package.
package todos

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Sentinel errors that handlers map to HTTP semantics: ErrNotFound to 404,
// the validation errors to 422, everything else to 500.
var (
	ErrNotFound       = errors.New("todo not found")
	ErrTitleRequired  = errors.New("title is required")
	ErrTitleTooLong   = errors.New("title is too long")
	ErrNotesTooLong   = errors.New("notes are too long")
	ErrInvalidDueDate = errors.New("invalid due date")
)

const (
	// MaxTitleLen and MaxNotesLen bound user input before it reaches
	// Postgres; the request-body size limit is a second, coarser line.
	MaxTitleLen = 500
	MaxNotesLen = 10000
)

// Todo is a stored todo. DueDate is a plain calendar date — always UTC
// midnight when set — and CompletedAt is nil until the todo is completed.
type Todo struct {
	ID          int64
	UserID      int64
	Title       string
	Notes       *string
	DueDate     *time.Time // UTC midnight; nil = unscheduled
	Completed   bool
	CompletedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CreateInput carries the fields a create accepts. Notes is stored verbatim
// (whitespace-only becomes NULL); DueDate must come from ParseDate.
type CreateInput struct {
	Title   string
	Notes   string
	DueDate *time.Time
}

// ParseDate parses an ISO YYYY-MM-DD date — the only format the app accepts,
// because <input type="date"> and the JSON API both submit ISO — and
// normalizes it to UTC midnight. Display formats such as DD-MM-YY are
// presentation-only and are never parsed back.
func ParseDate(s string) (time.Time, error) {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %q", ErrInvalidDueDate, s)
	}
	return d, nil
}

// normalizeDueDate snaps a due date to UTC midnight of its own UTC calendar
// date, so every stored due date compares exactly with Service.Today.
func normalizeDueDate(d *time.Time) *time.Time {
	if d == nil {
		return nil
	}
	y, m, day := d.UTC().Date()
	t := time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
	return &t
}

// validate trims and checks title/notes, returning the values to store: a
// non-empty trimmed title, and notes as nil when there is nothing to keep.
func validate(title, notes string) (string, *string, error) {
	title = strings.TrimSpace(title)
	switch {
	case title == "":
		return "", nil, ErrTitleRequired
	case utf8.RuneCountInString(title) > MaxTitleLen:
		return "", nil, ErrTitleTooLong
	case utf8.RuneCountInString(notes) > MaxNotesLen:
		return "", nil, ErrNotesTooLong
	}
	if strings.TrimSpace(notes) == "" {
		return title, nil, nil
	}
	return title, &notes, nil
}
