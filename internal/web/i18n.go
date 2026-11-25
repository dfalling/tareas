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
		"nav_tokens":            "API tokens",
		"nav_settings":          "Settings",
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
		"edit_delete":    "Delete todo",

		"tokens_title":  "API tokens",
		"tokens_intro":  "Tokens let API clients access your todos without your password. Copy a new token when you create it; it is shown only once.",
		"tokens_empty":  "No API tokens yet.",
		"tokens_create": "Create token",

		"tokens_name_label":    "Name",
		"tokens_name_hint":     "e.g. laptop, phone",
		"tokens_name_required": "Give the token a name.",
		"tokens_name_too_long": "Keep the name under 100 characters.",

		"tokens_revoke":       "Revoke",
		"tokens_revoke_aria":  "Revoke {{.Name}}",
		"tokens_created":      "Created {{.Date}}",
		"flash_token_created": "Token created — copy it now, it will not be shown again: {{.Token}}",
		"flash_token_revoked": "Token revoked.",

		"settings_title":               "Settings",
		"settings_save":                "Save",
		"settings_auto_delete_label":   "Auto-delete completed todos",
		"settings_auto_delete_hint":    "Completed todos are permanently deleted this many days after completion. Leave the field empty to keep them forever.",
		"settings_auto_delete_invalid": "Enter a number of days between 1 and 3650, or leave the field empty.",
		"flash_settings_saved":         "Settings saved.",

		"delete_title":    "Delete todo",
		"delete_question": "Delete '{{.Title}}'? This cannot be undone.",
		"delete_confirm":  "Yes, delete",
		"delete_cancel":   "Cancel",

		"toggle_aria": "Toggle {{.Title}}",

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
		"nav_tokens":            "Tokens de API",
		"nav_settings":          "Ajustes",
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
		"edit_delete":    "Eliminar tarea",

		"tokens_title":  "Tokens de API",
		"tokens_intro":  "Los tokens permiten que clientes de la API accedan a tus tareas sin tu contraseña. Copia un token nuevo al crearlo; solo se muestra una vez.",
		"tokens_empty":  "Todavía no hay tokens de API.",
		"tokens_create": "Crear token",

		"tokens_name_label":    "Nombre",
		"tokens_name_hint":     "p. ej. portátil, móvil",
		"tokens_name_required": "Ponle un nombre al token.",
		"tokens_name_too_long": "El nombre no puede superar los 100 caracteres.",

		"tokens_revoke":       "Revocar",
		"tokens_revoke_aria":  "Revocar {{.Name}}",
		"tokens_created":      "Creado el {{.Date}}",
		"flash_token_created": "Token creado: cópialo ahora, no se volverá a mostrar: {{.Token}}",
		"flash_token_revoked": "Token revocado.",

		"settings_title":               "Ajustes",
		"settings_save":                "Guardar",
		"settings_auto_delete_label":   "Autoeliminar tareas completadas",
		"settings_auto_delete_hint":    "Las tareas completadas se eliminan de forma permanente al pasar estos días desde que se completaron. Deja el campo vacío para conservarlas siempre.",
		"settings_auto_delete_invalid": "Introduce un número de días entre 1 y 3650, o deja el campo vacío.",
		"flash_settings_saved":         "Ajustes guardados.",

		"delete_title":    "Eliminar tarea",
		"delete_question": "¿Eliminar '{{.Title}}'? No se puede deshacer.",
		"delete_confirm":  "Sí, eliminar",
		"delete_cancel":   "Cancelar",

		"toggle_aria": "Cambiar {{.Title}}",

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
