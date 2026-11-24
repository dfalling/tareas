package web

import "strings"

// catalog holds the UI strings for one locale.
type catalog map[string]string

// messages maps locale codes to their UI strings. Adding a language means
// adding a catalog here; the parity test enforces that all locales stay in
// sync. User content (todo titles, notes) is never translated.
//
// Parameterized keys use text/template-style placeholders ({{.Title}}) and
// are filled with translatef; dates use the per-locale date_format pattern,
// so a different display format per language needs no code change.
var messages = map[string]catalog{
	"en": {
		"nav_logout":            "Log out",
		"login_title":           "Log in",
		"login_email_label":     "Email",
		"login_password_label":  "Password",
		"login_submit":          "Log in",
		"login_invalid":         "Wrong email or password.",
		"login_rate_limited":    "Too many attempts. Try again in a few minutes.",
		"setup_title":           "Create your account",
		"setup_email_label":     "Email",
		"setup_password_label":  "Password",
		"setup_password_hint":   "At least 8 characters.",
		"setup_submit":          "Create account",
		"setup_invalid_email":   "Enter a valid email address.",
		"setup_short_password":  "Password must be at least 8 characters.",
		"flash_account_created": "Account created. Welcome!",
		"todos_empty":           "Nothing here yet.",
		"error_internal":        "Something went wrong. Please try again.",

		"tab_today":       "Today",
		"tab_upcoming":    "Upcoming",
		"tab_unscheduled": "Unscheduled",
		"tab_completed":   "Completed",

		"label_title":   "Title",
		"label_notes":   "Notes",
		"label_due":     "Due date",
		"create_submit": "Add",

		"todos_title_required": "Give the todo a title.",
		"todos_title_too_long": "Keep the title under 500 characters.",
		"todos_notes_too_long": "Keep the notes under 10000 characters.",
		"todos_invalid_date":   "Enter a valid date, or leave the field empty.",

		"edit_title":     "Edit todo",
		"edit_save":      "Save",
		"edit_cancel":    "Cancel",
		"edit_clear_due": "Clear due date",

		"delete_title":    "Delete todo",
		"delete_question": "Delete '{{.Title}}'? This cannot be undone.",
		"delete_confirm":  "Yes, delete",
		"delete_cancel":   "Cancel",

		"toggle_aria": "Toggle {{.Title}}",
		"delete_aria": "Delete {{.Title}}",

		"todo_overdue": "overdue · {{.Date}}",
		"done_today":   "done today",
		"done_on":      "done {{.Date}}",

		"day_tomorrow": "Tomorrow",
		"weekday_sun":  "Sun",
		"weekday_mon":  "Mon",
		"weekday_tue":  "Tue",
		"weekday_wed":  "Wed",
		"weekday_thu":  "Thu",
		"weekday_fri":  "Fri",
		"weekday_sat":  "Sat",

		// Presentation-only Go reference-time layout: DD-MM-YY.
		"date_format": "02-01-06",
	},
	"es": {
		"nav_logout":            "Salir",
		"login_title":           "Iniciar sesión",
		"login_email_label":     "Correo electrónico",
		"login_password_label":  "Contraseña",
		"login_submit":          "Iniciar sesión",
		"login_invalid":         "Correo o contraseña incorrectos.",
		"login_rate_limited":    "Demasiados intentos. Vuelve a intentarlo en unos minutos.",
		"setup_title":           "Crea tu cuenta",
		"setup_email_label":     "Correo electrónico",
		"setup_password_label":  "Contraseña",
		"setup_password_hint":   "Mínimo 8 caracteres.",
		"setup_submit":          "Crear cuenta",
		"setup_invalid_email":   "Introduce un correo válido.",
		"setup_short_password":  "La contraseña debe tener al menos 8 caracteres.",
		"flash_account_created": "Cuenta creada. ¡Bienvenido!",
		"todos_empty":           "Nada por aquí todavía.",
		"error_internal":        "Algo salió mal. Inténtalo de nuevo.",

		"tab_today":       "Hoy",
		"tab_upcoming":    "Próximas",
		"tab_unscheduled": "Sin fecha",
		"tab_completed":   "Hechas",

		"label_title":   "Título",
		"label_notes":   "Notas",
		"label_due":     "Fecha límite",
		"create_submit": "Añadir",

		"todos_title_required": "Ponle un título a la tarea.",
		"todos_title_too_long": "El título no puede superar los 500 caracteres.",
		"todos_notes_too_long": "Las notas no pueden superar los 10000 caracteres.",
		"todos_invalid_date":   "Introduce una fecha válida, o deja el campo vacío.",

		"edit_title":     "Editar tarea",
		"edit_save":      "Guardar",
		"edit_cancel":    "Cancelar",
		"edit_clear_due": "Quitar fecha",

		"delete_title":    "Eliminar tarea",
		"delete_question": "¿Eliminar '{{.Title}}'? No se puede deshacer.",
		"delete_confirm":  "Sí, eliminar",
		"delete_cancel":   "Cancelar",

		"toggle_aria": "Cambiar {{.Title}}",
		"delete_aria": "Eliminar {{.Title}}",

		"todo_overdue": "atrasada · {{.Date}}",
		"done_today":   "hecha hoy",
		"done_on":      "hecha el {{.Date}}",

		"day_tomorrow": "Mañana",
		"weekday_sun":  "dom",
		"weekday_mon":  "lun",
		"weekday_tue":  "mar",
		"weekday_wed":  "mié",
		"weekday_thu":  "jue",
		"weekday_fri":  "vie",
		"weekday_sat":  "sáb",

		// Presentation-only Go reference-time layout: DD-MM-YY.
		"date_format": "02-01-06",
	},
}

// defaultLocale is the fallback when a catalog is missing or a key is
// untranslated.
const defaultLocale = "en"

// resolveLocale maps a configured locale to a supported one.
func resolveLocale(locale string) string {
	if _, ok := messages[locale]; ok {
		return locale
	}
	return defaultLocale
}

// translator returns a per-request translate function for the locale. It
// falls back to the default locale, then to the key itself, so a missing
// translation is visible rather than fatal.
func translator(locale string) func(string) string {
	cat := messages[locale]
	return func(key string) string {
		if s, ok := cat[key]; ok {
			return s
		}
		if s, ok := messages[defaultLocale][key]; ok {
			return s
		}
		return key
	}
}

// translatef translates key and fills its {{.Name}} placeholders with data,
// for whole-sentence keys like "Delete '{{.Title}}'?". Values may be user
// content; html/template escapes the result once, at render time.
func translatef(locale, key string, data map[string]string) string {
	msg := translator(locale)(key)
	pairs := make([]string, 0, 2*len(data))
	for name, value := range data {
		pairs = append(pairs, "{{."+name+"}}", value)
	}
	return strings.NewReplacer(pairs...).Replace(msg)
}

// dateFormat returns the locale's display pattern for plain dates.
func dateFormat(locale string) string {
	return translator(locale)("date_format")
}

// weekdayKeys maps time.Weekday (Sunday = 0) to its message key.
var weekdayKeys = [...]string{
	"weekday_sun", "weekday_mon", "weekday_tue",
	"weekday_wed", "weekday_thu", "weekday_fri", "weekday_sat",
}
