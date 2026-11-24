package ratelimit

import (
	"testing"
	"time"
)

func TestLimiterBlocksAfterMax(t *testing.T) {
	t.Parallel()

	l := New(3, time.Hour)
	for range 3 {
		if !l.Allow("k") {
			t.Fatal("Allow = false before limit reached, want true")
		}
		l.Record("k")
	}
	if l.Allow("k") {
		t.Error("Allow = true after limit reached, want false")
	}
	if !l.Allow("other") {
		t.Error("Allow(other) = false, want true")
	}
}

func TestLimiterWindowExpiry(t *testing.T) {
	t.Parallel()

	l := New(1, 10*time.Millisecond)
	l.Record("k")
	if l.Allow("k") {
		t.Error("Allow = true immediately after Record, want false")
	}
	time.Sleep(20 * time.Millisecond)
	if !l.Allow("k") {
		t.Error("Allow = false after window expiry, want true")
	}
}
