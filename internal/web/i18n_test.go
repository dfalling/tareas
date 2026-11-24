package web

import "testing"

// TestMessageParity keeps every locale in sync with the default catalog:
// no missing keys, no stray keys.
func TestMessageParity(t *testing.T) {
	t.Parallel()

	ref := messages[defaultLocale]
	for locale, cat := range messages {
		if locale == defaultLocale {
			continue
		}
		for key := range ref {
			if _, ok := cat[key]; !ok {
				t.Errorf("locale %q is missing key %q", locale, key)
			}
		}
		for key := range cat {
			if _, ok := ref[key]; !ok {
				t.Errorf("locale %q has extra key %q", locale, key)
			}
		}
	}
}

func TestTranslatorFallsBack(t *testing.T) {
	t.Parallel()

	// A known key in a known locale translates.
	if got := translator("es")("login_title"); got != "Iniciar sesión" {
		t.Errorf("translator(es)(login_title) = %q", got)
	}
	// An unknown key falls back to the key itself rather than rendering empty.
	if got := translator("es")("nonexistent_key"); got != "nonexistent_key" {
		t.Errorf("translator(es)(nonexistent_key) = %q", got)
	}
}
