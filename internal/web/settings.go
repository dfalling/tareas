package web

import (
	"net/http"
	"strconv"
	"strings"
)

// autoDeleteDaysMax is the sanity ceiling for the auto-delete window (ten
// years): anything larger is a typo. The users table's CHECK constraint
// enforces the same range, so keep the two in sync.
const autoDeleteDaysMax = 3650

// settingsPage renders the general settings page with the account's current
// auto-delete window.
func (a *App) settingsPage(w http.ResponseWriter, r *http.Request) {
	days, err := a.users.AutoDeleteDays(r.Context(), a.userID(r))
	if err != nil {
		a.serverError(w, err)
		return
	}
	a.renderSettings(w, r, http.StatusOK, "", autoDeleteDaysInput(days))
}

// settingsSubmit stores the auto-delete window (PRG). An empty field means
// never; failed validation re-renders with what was typed.
func (a *App) settingsSubmit(w http.ResponseWriter, r *http.Request) {
	value := r.FormValue("auto_delete_days")
	days, ok := parseAutoDeleteDays(value)
	if !ok {
		a.renderSettings(w, r, http.StatusUnprocessableEntity, "settings_auto_delete_invalid", value)
		return
	}
	if err := a.users.SetAutoDeleteDays(r.Context(), a.userID(r), days); err != nil {
		a.serverError(w, err)
		return
	}
	a.scs.Put(r.Context(), sessionKeyFlash, a.t("flash_settings_saved"))
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// renderSettings renders the settings page; daysValue is the input's value
// ("" = never), passed verbatim on validation errors so nothing typed is lost.
func (a *App) renderSettings(w http.ResponseWriter, r *http.Request, status int, errKey, daysValue string) {
	data := a.viewData(r)
	data.Title = a.t("settings_title")
	data.Settings = &settingsPage{AutoDeleteDays: daysValue}
	if errKey != "" {
		data.Error = a.t(errKey)
	}
	a.render(w, status, "settings.html", data)
}

// parseAutoDeleteDays maps the form value to the stored setting: empty
// (or whitespace) = never (nil); otherwise a whole number of days from 1
// through autoDeleteDaysMax. ok is false for anything else.
func parseAutoDeleteDays(s string) (days *int, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > autoDeleteDaysMax {
		return nil, false
	}
	return &n, true
}

// autoDeleteDaysInput renders the stored setting as the input's value.
func autoDeleteDaysInput(days *int) string {
	if days == nil {
		return ""
	}
	return strconv.Itoa(*days)
}
