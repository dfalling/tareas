// Package tokens manages API bearer tokens: generation, hashed storage,
// authentication, and revocation. Only sha256(token) is stored, so a database
// leak never leaks usable credentials; the settings page identifies tokens by
// a short plaintext prefix.
package tokens

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// Sentinel errors that handlers map to HTTP semantics: ErrNotFound to 404
// (or 401 for authentication), the validation errors to 422.
var (
	ErrNotFound     = errors.New("token not found")
	ErrExpired      = errors.New("token expired")
	ErrNameRequired = errors.New("token name is required")
	ErrNameTooLong  = errors.New("token name is too long")
)

const (
	// entropyBytes is 256 bits of randomness — the token itself, before
	// base64 encoding.
	entropyBytes = 32
	// PrefixLen is how many plaintext characters are stored for UI
	// identification; the rest of the token is unrecoverable.
	PrefixLen = 8
	// MaxNameLen bounds the device label.
	MaxNameLen = 100
)

// Token is a stored API token. The plaintext credential is never kept: only
// its sha256 hash (never exposed here), a short prefix, and metadata.
type Token struct {
	ID         int64
	UserID     int64
	Prefix     string
	Name       string
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
	CreatedAt  time.Time
}

// generate returns a new plaintext token and the values to store for it:
// 256 bits of CSPRNG entropy, base64url-encoded (~43 chars), its sha256 hash
// as hex, and the identification prefix.
func generate() (plaintext, hash, prefix string, err error) {
	buf := make([]byte, entropyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", "", err
	}
	plaintext = base64.RawURLEncoding.EncodeToString(buf)
	hash = Hash(plaintext)
	prefix = plaintext[:PrefixLen]
	return plaintext, hash, prefix, nil
}

// Hash is the stored form of a plaintext token: sha256 as lowercase hex.
func Hash(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// validateName trims and checks the device label.
func validateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", ErrNameRequired
	case utf8.RuneCountInString(name) > MaxNameLen:
		return "", ErrNameTooLong
	}
	return name, nil
}
