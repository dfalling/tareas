package tokens

import (
	"encoding/base64"
	"regexp"
	"strings"
	"testing"
)

// TestGenerate checks the plaintext token's shape and that every stored
// value derives from it.
func TestGenerate(t *testing.T) {
	t.Parallel()

	plaintext, hash, prefix, err := generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// 32 bytes base64url-encoded without padding: 43 characters from a
	// URL-safe alphabet.
	if len(plaintext) != 43 {
		t.Errorf("plaintext length = %d, want 43", len(plaintext))
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(plaintext) {
		t.Errorf("plaintext %q has characters outside base64url", plaintext)
	}
	if _, err := base64.RawURLEncoding.DecodeString(plaintext); err != nil {
		t.Errorf("plaintext does not decode as base64url: %v", err)
	}
	if hash != Hash(plaintext) {
		t.Errorf("hash does not match sha256(plaintext)")
	}
	if prefix != plaintext[:PrefixLen] || len(prefix) != PrefixLen {
		t.Errorf("prefix = %q, want the first %d plaintext characters", prefix, PrefixLen)
	}
}

// TestGenerateUnique makes sure two tokens never share plaintext.
func TestGenerateUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool, 64)
	for range 64 {
		plaintext, _, _, err := generate()
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if seen[plaintext] {
			t.Fatalf("duplicate plaintext after %d generations", len(seen)+1)
		}
		seen[plaintext] = true
	}
}

// TestHash pins the hash form: sha256, lowercase hex.
func TestHash(t *testing.T) {
	t.Parallel()

	if got := Hash("abc"); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("Hash(abc) = %q", got)
	}
}

// TestValidateName covers the device-label rules shared by the settings page
// and the API.
func TestValidateName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{"trims whitespace", "  laptop  ", "laptop", nil},
		{"empty means required", "   ", "", ErrNameRequired},
		{"too long", strings.Repeat("x", MaxNameLen+1), "", ErrNameTooLong},
		{"exactly at limit", strings.Repeat("x", MaxNameLen), strings.Repeat("x", MaxNameLen), nil},
	}
	for _, tc := range cases {
		got, err := validateName(tc.in)
		if err != tc.wantErr {
			t.Errorf("validateName(%q) error = %v, want %v", tc.in, err, tc.wantErr)
		}
		if err == nil && got != tc.want {
			t.Errorf("validateName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
