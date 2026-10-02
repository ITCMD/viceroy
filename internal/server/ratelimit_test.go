package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

// Parallel guesses are counted as they start, so a burst can't slip past the limit.
func TestLoginRateLimitParallel(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"L","email":"l@example.com","password":"correct horse battery"}`, true)
	var mu sync.Mutex
	codes := map[int]int{}
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _ := c.newSession().do("POST", "/api/auth/login", `{"email":"l@example.com","password":"wrong wrong wrong"}`, true)
			mu.Lock()
			codes[code]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if codes[401] != 10 || codes[429] != 20 {
		t.Fatalf("codes = %v, want 10×401 and 20×429", codes)
	}
}

// Guesses at one account from many IPs are limited by the account.
func TestLoginAccountLimit(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"L","email":"l@example.com","password":"correct horse battery"}`, true)
	c.server.limiter = newLoginLimiter(1000, 15*time.Minute) // as if every guess came from a new IP
	for i := 0; i < 20; i++ {
		if code, _ := c.newSession().do("POST", "/api/auth/login", `{"email":"L@example.com ","password":"wrong wrong wrong"}`, true); code != 401 {
			t.Fatalf("guess %d = %d", i, code)
		}
	}
	code, body := c.newSession().do("POST", "/api/auth/login", `{"email":"l@example.com","password":"correct horse battery"}`, true)
	if code != 429 || !strings.Contains(body["error"].(string), "this account") {
		t.Fatalf("after 20 guesses = %d %v", code, body)
	}
	if code, _ := c.newSession().do("POST", "/api/auth/login", `{"email":"other@example.com","password":"wrong wrong wrong"}`, true); code != 401 {
		t.Fatalf("other account = %d", code)
	}
}

func TestLimitKey(t *testing.T) {
	key := func(ip string) string {
		r := httptest.NewRequest("GET", "/", nil)
		return limitKey(r.WithContext(context.WithValue(r.Context(), clientIPKey, netip.MustParseAddr(ip))))
	}
	if a, b := key("2001:db8:1:2:aaaa::1"), key("2001:db8:1:2:bbbb::9"); a != b || a != "2001:db8:1:2::/64" {
		t.Fatalf("same /64 = %q, %q", a, b)
	}
	if key("2001:db8:1:3::1") == key("2001:db8:1:2::1") {
		t.Fatal("different /64s share a key")
	}
	if k := key("192.0.2.7"); k != "192.0.2.7" {
		t.Fatalf("ipv4 = %q", k)
	}
}

func TestLimiterSweep(t *testing.T) {
	l := newLoginLimiter(3, time.Minute)
	now := time.Unix(1000, 0)
	l.now = func() time.Time { return now }
	for i := 0; i < sweepAt; i++ {
		l.fail(netip.AddrFrom4([4]byte{10, 0, byte(i >> 8), byte(i)}).String())
	}
	now = now.Add(2 * time.Minute)
	l.fail("192.0.2.1")
	if len(l.fails) != 1 {
		t.Fatalf("keys after sweep = %d", len(l.fails))
	}
}

func TestSecurityHeaders(t *testing.T) {
	script := "\n      try { x() } catch {}\n    "
	s := &Server{web: fstest.MapFS{"index.html": {Data: []byte("<head><script>" + script + "</script></head>")}}}
	s.cfg.PublicURL = "https://budget.example.com"
	w := httptest.NewRecorder()
	s.securityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	sum := sha256.Sum256([]byte(script))
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self' 'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"';") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("csp = %q", csp)
	}
	if w.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("no HSTS on an https install")
	}
}
