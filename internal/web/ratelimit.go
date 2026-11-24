package web

import (
	"sync"
	"time"
)

// Limiter is a fixed-window rate limiter for failed login attempts, keyed by
// arbitrary strings (client IP, email). It is in-memory only, which is fine
// for a single-instance deployment.
type Limiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	entries map[string]*limiterEntry
}

type limiterEntry struct {
	count int
	start time.Time
}

// NewLimiter returns a limiter allowing max events per window per key.
func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{
		max:     max,
		window:  window,
		entries: make(map[string]*limiterEntry),
	}
}

// Allow reports whether another event for key is permitted in the current
// window.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	e, ok := l.entries[key]
	if !ok {
		return true
	}
	if time.Since(e.start) >= l.window {
		delete(l.entries, key)
		return true
	}
	return e.count < l.max
}

// Record notes an event for key, starting a new window if needed.
func (l *Limiter) Record(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	e, ok := l.entries[key]
	if !ok || time.Since(e.start) >= l.window {
		if len(l.entries) >= 1024 {
			l.prune()
		}
		l.entries[key] = &limiterEntry{count: 1, start: time.Now()}
		return
	}
	e.count++
}

// prune drops expired entries so the map cannot grow without bound.
func (l *Limiter) prune() {
	now := time.Now()
	for k, e := range l.entries {
		if now.Sub(e.start) >= l.window {
			delete(l.entries, k)
		}
	}
}
