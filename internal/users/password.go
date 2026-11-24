package users

import (
	"fmt"

	"github.com/alexedwards/argon2id"
)

// params follows the OWASP recommended Argon2id parameters: 19 MiB of
// memory, 2 iterations, 1 lane (~19 MiB peak during hashing).
var params = &argon2id.Params{
	Memory:      19456,
	Iterations:  2,
	Parallelism: 1,
	SaltLength:  16,
	KeyLength:   32,
}

// HashPassword hashes a plain-text password with Argon2id.
func HashPassword(password string) (string, error) {
	hash, err := argon2id.CreateHash(password, params)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return hash, nil
}

// VerifyPassword reports whether the plain-text password matches the stored
// Argon2id hash.
func VerifyPassword(password, hash string) bool {
	match, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		// Malformed hash: treat as a failed verification.
		return false
	}
	return match
}

// dummyHash equalizes verification timing when the account does not exist,
// so login latency does not reveal whether an email is registered.
var dummyHash = func() string {
	h, _ := HashPassword("timing-equalizer-not-a-real-password")
	return h
}()

// VerifyDummy runs a verification against a fixed hash. Login handlers call
// it after a failed user lookup so both paths do the same Argon2id work.
func VerifyDummy(password string) bool {
	return VerifyPassword(password, dummyHash)
}
