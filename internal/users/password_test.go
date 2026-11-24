package users

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	t.Parallel()

	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !VerifyPassword("correct horse battery staple", hash) {
		t.Error("VerifyPassword(correct) = false, want true")
	}
	if VerifyPassword("wrong password", hash) {
		t.Error("VerifyPassword(wrong) = true, want false")
	}
}
