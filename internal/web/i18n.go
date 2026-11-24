package web

// catalog holds the UI strings for one locale.
type catalog map[string]string

// messages maps locale codes to their UI strings. Adding a language means
// adding a catalog here; the parity test enforces that all locales stay in
// sync. User content (todo titles, notes) is never translated.
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
		"todos_title":           "Today",
		"todos_empty":           "Nothing here yet.",
		"error_internal":        "Something went wrong. Please try again.",
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
		"todos_title":           "Hoy",
		"todos_empty":           "Nada por aquí todavía.",
		"error_internal":        "Algo salió mal. Inténtalo de nuevo.",
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
