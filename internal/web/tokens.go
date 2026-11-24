package web

import (
	"errors"
	"net/http"
	"time"

	"github.com/dfalling/tareas/internal/tokens"
)

// tokensPage renders the API-token settings page.
func (a *App) tokensPage(w http.ResponseWriter, r *http.Request) {
	a.renderTokens(w, r, http.StatusOK, "", nil)
}

// renderTokens lists the user's tokens; failed creates re-render through the
// same path, preserving what was typed.
func (a *App) renderTokens(w http.ResponseWriter, r *http.Request, status int, errKey string, form *tokensForm) {
	list, err := a.tokens.List(r.Context(), a.userID(r))
	if err != nil {
		a.serverError(w, err)
		return
	}

	page := &tokensPage{Tokens: make([]tokenRow, 0, len(list))}
	for _, tok := range list {
		page.Tokens = append(page.Tokens, tokenRow{
			ID:         tok.ID,
			Prefix:     tok.Prefix,
			Name:       tok.Name,
			Created:    a.tf("tokens_created", map[string]string{"Date": a.instantDate(tok.CreatedAt)}),
			RevokeAria: a.tf("tokens_revoke_aria", map[string]string{"Name": tok.Name}),
		})
	}
	if form != nil {
		page.Form = *form
	}

	data := a.viewData(r)
	data.Title = a.t("tokens_title")
	data.Tokens = page
	if errKey != "" {
		data.Error = a.t(errKey)
	}
	a.render(w, status, "tokens.html", data)
}

// tokensCreate issues a new token. The plaintext is put in the session flash
// and shown exactly once, on the page this POST redirects to.
func (a *App) tokensCreate(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")

	plaintext, _, err := a.tokens.Issue(r.Context(), a.userID(r), name, nil)
	if err != nil {
		switch {
		case errors.Is(err, tokens.ErrNameRequired):
			a.renderTokens(w, r, http.StatusUnprocessableEntity, "tokens_name_required", &tokensForm{Name: name})
		case errors.Is(err, tokens.ErrNameTooLong):
			a.renderTokens(w, r, http.StatusUnprocessableEntity, "tokens_name_too_long", &tokensForm{Name: name})
		default:
			a.serverError(w, err)
		}
		return
	}

	a.scs.Put(r.Context(), sessionKeyFlash, a.tf("flash_token_created", map[string]string{"Token": plaintext}))
	http.Redirect(w, r, "/settings/tokens", http.StatusSeeOther)
}

// tokensRevoke deletes one of the user's tokens (PRG). A token that is
// already gone — revoked in another tab, or never ours — still ends here
// with a confirmation, because the outcome the user cares about is the same.
func (a *App) tokensRevoke(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := a.tokens.Revoke(r.Context(), a.userID(r), id); err != nil && !errors.Is(err, tokens.ErrNotFound) {
		a.serverError(w, err)
		return
	}
	a.scs.Put(r.Context(), sessionKeyFlash, a.t("flash_token_revoked"))
	http.Redirect(w, r, "/settings/tokens", http.StatusSeeOther)
}

// instantDate renders an instant's calendar date in the app timezone and the
// locale's pattern. Unlike a due date, an instant must be converted: UTC
// midnight is a different calendar day in halves of the world.
func (a *App) instantDate(t time.Time) string {
	return t.In(a.loc).Format(dateFormat(a.locale))
}
