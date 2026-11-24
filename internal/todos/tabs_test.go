package todos

import (
	"errors"
	"slices"
	"testing"
	"time"

	// Embedded tzdata keeps the timezone-dependent tests hermetic even where
	// the host has no zoneinfo database.
	_ "time/tzdata"
)

// today is a fixed clock: 2026-11-24 as a UTC-midnight date, the exact shape
// Service.Today produces, so the boundary cases below are precise.
var today = time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)

func mustDate(t *testing.T, s string) *time.Time {
	t.Helper()
	d, err := ParseDate(s)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", s, err)
	}
	return &d
}

func todo(id int64, title string, mutate ...func(*Todo)) Todo {
	tt := Todo{ID: id, UserID: 1, Title: title}
	for _, m := range mutate {
		m(&tt)
	}
	return tt
}

func withDue(d *time.Time) func(*Todo) {
	return func(t *Todo) { t.DueDate = d }
}

func done(at time.Time) func(*Todo) {
	return func(t *Todo) {
		t.Completed = true
		t.CompletedAt = &at
	}
}

func titles(list []Todo) []string {
	out := make([]string, len(list))
	for i, t := range list {
		out[i] = t.Title
	}
	return out
}

func TestSliceIntoTabs(t *testing.T) {
	t.Parallel()

	completedEarly := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	completedLate := time.Date(2026, 11, 23, 18, 0, 0, 0, time.UTC)

	got := SliceIntoTabs([]Todo{
		todo(1, "due today", withDue(mustDate(t, "2026-11-24"))),
		todo(2, "overdue", withDue(mustDate(t, "2026-11-18"))),
		todo(3, "tomorrow", withDue(mustDate(t, "2026-11-25"))),
		todo(4, "next week", withDue(mustDate(t, "2026-11-30"))),
		todo(5, "unscheduled"),
		todo(6, "done first", done(completedEarly)),
		todo(7, "done last", done(completedLate)),
	}, today)

	wantToday := []string{"due today", "overdue"}
	if !slices.Equal(titles(got.Today), wantToday) {
		t.Errorf("Today = %v, want %v", titles(got.Today), wantToday)
	}
	if len(got.Upcoming) != 2 {
		t.Fatalf("Upcoming has %d day groups, want 2", len(got.Upcoming))
	}
	if d := got.Upcoming[0].Date; !d.Equal(*mustDate(t, "2026-11-25")) {
		t.Errorf("first Upcoming day = %v, want 2026-11-25", d)
	}
	if gotUpcoming := titles(got.Upcoming[0].Todos); !slices.Equal(gotUpcoming, []string{"tomorrow"}) {
		t.Errorf("first Upcoming group = %v, want [tomorrow]", gotUpcoming)
	}
	if d := got.Upcoming[1].Date; !d.Equal(*mustDate(t, "2026-11-30")) {
		t.Errorf("second Upcoming day = %v, want 2026-11-30", d)
	}
	if gotUnscheduled := titles(got.Unscheduled); !slices.Equal(gotUnscheduled, []string{"unscheduled"}) {
		t.Errorf("Unscheduled = %v", gotUnscheduled)
	}
	if gotCompleted := titles(got.Completed); !slices.Equal(gotCompleted, []string{"done last", "done first"}) {
		t.Errorf("Completed = %v, want most recent first", gotCompleted)
	}
}

func TestSliceIntoTabsEmpty(t *testing.T) {
	t.Parallel()

	tabs := SliceIntoTabs(nil, today)
	if tabs.Today != nil || tabs.Upcoming != nil || tabs.Unscheduled != nil || tabs.Completed != nil {
		t.Errorf("empty input produced %+v, want all-nil sections", tabs)
	}
}

func TestSliceIntoTabsAlphabetical(t *testing.T) {
	t.Parallel()

	// lower(title) decides; the raw title breaks ties between titles that
	// differ only in case ("M" sorts before "m"), and the ID is the final
	// tiebreak — mirroring ORDER BY lower(title), title, id.
	tabs := SliceIntoTabs([]Todo{
		todo(2, "banana"),
		todo(3, "Apple pie"),
		todo(4, "milk", withDue(mustDate(t, "2026-11-24"))),
		todo(5, "Milk", withDue(mustDate(t, "2026-11-24"))),
		todo(1, "apple"),
	}, today)

	wantToday := []string{"Milk", "milk"}
	if !slices.Equal(titles(tabs.Today), wantToday) {
		t.Errorf("Today = %v, want %v", titles(tabs.Today), wantToday)
	}
	wantUnscheduled := []string{"apple", "Apple pie", "banana"}
	if !slices.Equal(titles(tabs.Unscheduled), wantUnscheduled) {
		t.Errorf("Unscheduled = %v, want %v", titles(tabs.Unscheduled), wantUnscheduled)
	}
}

func TestSliceIntoTabsUpcomingGrouping(t *testing.T) {
	t.Parallel()

	// Days ascend and repeat days share one group, alphabetical within it.
	tabs := SliceIntoTabs([]Todo{
		todo(1, "late", withDue(mustDate(t, "2026-11-27"))),
		todo(2, "second same day", withDue(mustDate(t, "2026-11-25"))),
		todo(3, "first same day", withDue(mustDate(t, "2026-11-25"))),
		todo(4, "middle", withDue(mustDate(t, "2026-11-26"))),
	}, today)

	if len(tabs.Upcoming) != 3 {
		t.Fatalf("Upcoming has %d day groups, want 3", len(tabs.Upcoming))
	}
	wantDays := []string{"2026-11-25", "2026-11-26", "2026-11-27"}
	for i, want := range wantDays {
		if d := tabs.Upcoming[i].Date; !d.Equal(*mustDate(t, want)) {
			t.Errorf("group %d date = %v, want %v", i, d, want)
		}
	}
	wantFirstGroup := []string{"first same day", "second same day"}
	if got := titles(tabs.Upcoming[0].Todos); !slices.Equal(got, wantFirstGroup) {
		t.Errorf("first group = %v, want %v", got, wantFirstGroup)
	}
}

func TestIsOverdue(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		todo Todo
		want bool
	}{
		{"due yesterday", todo(1, "x", withDue(mustDate(t, "2026-11-23"))), true},
		{"due today", todo(1, "x", withDue(mustDate(t, "2026-11-24"))), false},
		{"due tomorrow", todo(1, "x", withDue(mustDate(t, "2026-11-25"))), false},
		{"unscheduled", todo(1, "x"), false},
		{"completed but overdue", todo(1, "x", withDue(mustDate(t, "2026-11-01")), done(today)), false},
	}
	for _, tc := range cases {
		if got := IsOverdue(tc.todo, today); got != tc.want {
			t.Errorf("%s: IsOverdue = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestParseView(t *testing.T) {
	t.Parallel()

	cases := map[string]View{
		"":            ViewToday,
		"today":       ViewToday,
		"upcoming":    ViewUpcoming,
		"unscheduled": ViewUnscheduled,
		"completed":   ViewCompleted,
		"bogus":       ViewToday,
		"Today":       ViewToday,
		"../etc":      ViewToday,
	}
	for raw, want := range cases {
		if got := ParseView(raw); got != want {
			t.Errorf("ParseView(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestTodayIn(t *testing.T) {
	t.Parallel()

	// The same instant lands on different calendar dates depending on the
	// app timezone; "today" must follow the zone, never the server clock's.
	cases := []struct {
		name string
		loc  string
		now  time.Time
		want string
	}{
		{"utc late evening", "UTC", time.Date(2026, 11, 24, 23, 59, 0, 0, time.UTC), "2026-11-24"},
		{"auckland day ahead", "Pacific/Auckland", time.Date(2026, 11, 24, 11, 30, 0, 0, time.UTC), "2026-11-25"},
		{"new york day behind", "America/New_York", time.Date(2026, 11, 25, 3, 30, 0, 0, time.UTC), "2026-11-24"},
	}
	for _, tc := range cases {
		loc, err := time.LoadLocation(tc.loc)
		if err != nil {
			t.Fatalf("load %s: %v", tc.loc, err)
		}
		got := todayIn(loc, tc.now)
		if want, err := ParseDate(tc.want); err != nil || !got.Equal(want) {
			t.Errorf("%s: todayIn = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestParseDate(t *testing.T) {
	t.Parallel()

	d, err := ParseDate("2026-11-24")
	if err != nil {
		t.Fatalf("ParseDate(valid): %v", err)
	}
	if want := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC); !d.Equal(want) {
		t.Errorf("ParseDate(valid) = %v, want UTC midnight %v", d, want)
	}

	for _, bad := range []string{
		"", "24-11-2026", "2026/11/24", "2026-1-2", "2026-13-01",
		"2026-02-30", "2026-11-24T00:00:00Z", " 2026-11-24", "2026-11-24 ",
	} {
		if _, err := ParseDate(bad); !errors.Is(err, ErrInvalidDueDate) {
			t.Errorf("ParseDate(%q) error = %v, want ErrInvalidDueDate", bad, err)
		}
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	title, notes, err := validate("  Buy milk  ", "  ")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if title != "Buy milk" {
		t.Errorf("title = %q, want trimmed %q", title, "Buy milk")
	}
	if notes != nil {
		t.Errorf("whitespace-only notes = %v, want nil", *notes)
	}

	_, notes, err = validate("Call mom", "  use the phone  ")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if notes == nil || *notes != "  use the phone  " {
		t.Errorf("notes = %v, want stored verbatim", notes)
	}

	longTitle := make([]rune, MaxTitleLen+1)
	for i := range longTitle {
		longTitle[i] = 'a'
	}
	if _, _, err := validate(string(longTitle), ""); err != ErrTitleTooLong {
		t.Errorf("over-long title: err = %v, want ErrTitleTooLong", err)
	}

	longNotes := make([]rune, MaxNotesLen+1)
	for i := range longNotes {
		longNotes[i] = 'a'
	}
	if _, _, err := validate("ok", string(longNotes)); err != ErrNotesTooLong {
		t.Errorf("over-long notes: err = %v, want ErrNotesTooLong", err)
	}

	for _, blank := range []string{"", "   ", "\n\t"} {
		if _, _, err := validate(blank, ""); err != ErrTitleRequired {
			t.Errorf("blank title %q: err = %v, want ErrTitleRequired", blank, err)
		}
	}
}
