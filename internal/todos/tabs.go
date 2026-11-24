package todos

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

// View is one of the tab filters of the HTML UI (the ?view= query value).
type View string

const (
	ViewToday       View = "today"
	ViewUpcoming    View = "upcoming"
	ViewUnscheduled View = "unscheduled"
	ViewCompleted   View = "completed"
)

// ParseView maps a raw ?view= value to a View; anything unrecognized,
// including the empty string, is the default Today tab.
func ParseView(raw string) View {
	switch View(raw) {
	case ViewUpcoming, ViewUnscheduled, ViewCompleted:
		return View(raw)
	default:
		return ViewToday
	}
}

// DayGroup is one "upcoming" section: a calendar day and its todos.
type DayGroup struct {
	Date  time.Time // UTC midnight
	Todos []Todo
}

// Tabs is the result of slicing one user's todos into the four tab sections.
// Empty sections stay nil.
type Tabs struct {
	Today       []Todo
	Upcoming    []DayGroup
	Unscheduled []Todo
	Completed   []Todo
}

// SliceIntoTabs partitions todos into the four tab sections. today must be a
// UTC-midnight date (Service.Today). It is a pure function: input order does
// not matter, and every section is sorted deterministically — alphabetical
// (lower(title), title, id) everywhere except Completed, which is
// most-recently-completed first.
func SliceIntoTabs(list []Todo, today time.Time) Tabs {
	var tabs Tabs
	byDay := map[time.Time][]Todo{}
	for _, t := range list {
		switch {
		case t.Completed:
			tabs.Completed = append(tabs.Completed, t)
		case t.DueDate == nil:
			tabs.Unscheduled = append(tabs.Unscheduled, t)
		case t.DueDate.After(today):
			byDay[*t.DueDate] = append(byDay[*t.DueDate], t)
		default: // due today or overdue — both live in Today
			tabs.Today = append(tabs.Today, t)
		}
	}

	sortByTitle(tabs.Today)
	sortByTitle(tabs.Unscheduled)
	sortByCompletedDesc(tabs.Completed)

	days := make([]time.Time, 0, len(byDay))
	for day := range byDay {
		days = append(days, day)
	}
	slices.SortFunc(days, func(a, b time.Time) int { return a.Compare(b) })
	for _, day := range days {
		group := DayGroup{Date: day, Todos: byDay[day]}
		sortByTitle(group.Todos)
		tabs.Upcoming = append(tabs.Upcoming, group)
	}
	return tabs
}

// IsOverdue reports whether t is unfinished with a due date before today
// (due today is not overdue yet). Both values must be UTC-midnight dates.
func IsOverdue(t Todo, today time.Time) bool {
	return !t.Completed && t.DueDate != nil && t.DueDate.Before(today)
}

// sortByTitle is the Go twin of the SQL ORDER BY lower(title), title, id, so
// re-sorting a ListTodos result never disturbs the order Postgres chose. For
// titles that differ only in case, the mixed-case text breaks the tie —
// matching the second sort key.
func sortByTitle(list []Todo) {
	slices.SortFunc(list, func(a, b Todo) int {
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)),
			cmp.Compare(a.Title, b.Title),
			cmp.Compare(a.ID, b.ID),
		)
	})
}

// sortByCompletedDesc orders most recently completed first, with the ID
// breaking exact-timestamp ties; a completed todo without a timestamp
// (impossible via Toggle, but possible via hand-edited rows) sorts last.
func sortByCompletedDesc(list []Todo) {
	slices.SortFunc(list, func(a, b Todo) int {
		if (a.CompletedAt == nil) != (b.CompletedAt == nil) {
			if a.CompletedAt == nil {
				return 1
			}
			return -1
		}
		if a.CompletedAt != nil {
			if c := b.CompletedAt.Compare(*a.CompletedAt); c != 0 {
				return c
			}
		}
		return cmp.Compare(b.ID, a.ID)
	})
}
