package server

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// loginLimiter blocks a key (an IP, or an account's email) after max failed attempts
// within window.
type loginLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	now    func() time.Time
	fails  map[string][]time.Time
}

func newLoginLimiter(max int, window time.Duration) *loginLimiter {
	return &loginLimiter{max: max, window: window, now: time.Now, fails: map[string][]time.Time{}}
}

// sweepAt bounds memory: past this many keys, every expired one is dropped.
const sweepAt = 4096

func (l *loginLimiter) recent(key string) []time.Time {
	cutoff := l.now().Add(-l.window)
	kept := l.fails[key][:0]
	for _, t := range l.fails[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.fails, key)
		return nil
	}
	l.fails[key] = kept
	return kept
}

func (l *loginLimiter) sweep() {
	if len(l.fails) < sweepAt {
		return
	}
	for k := range l.fails {
		l.recent(k)
	}
}

func (l *loginLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(key)) < l.max
}

func (l *loginLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep()
	l.fails[key] = append(l.recent(key), l.now())
}

// take counts an attempt before it runs and reports whether it may go ahead. Counting up
// front stops a burst of parallel guesses from all passing the check before any of them
// fails; call reset when the attempt succeeds.
func (l *loginLimiter) take(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.recent(key)
	if len(r) >= l.max {
		return false
	}
	l.sweep()
	l.fails[key] = append(r, l.now())
	return true
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}

// limitKey is the client's IP for rate limiting. IPv6 clients usually own a whole /64, so
// they're limited by it.
func limitKey(r *http.Request) string {
	ip := ClientIP(r)
	if ip.Is6() {
		p, _ := ip.Prefix(64)
		return p.String()
	}
	return ip.String()
}

// accountKey is the per-account limiter key for a sign-in email.
func accountKey(email string) string { return strings.ToLower(strings.TrimSpace(email)) }
