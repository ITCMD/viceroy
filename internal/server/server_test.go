package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"viceroy/internal/config"
	"viceroy/internal/db"
)

type client struct {
	t    *testing.T
	base string
	http *http.Client
}

func newTestServer(t *testing.T) *client {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	cfg := config.Default()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	web := fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}}
	srv := httptest.NewServer(New(cfg, conn, web, slog.New(slog.DiscardHandler)).Handler())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: srv.URL, http: &http.Client{Jar: jar}}
}

func (c *client) do(method, path, body string, csrf bool) (int, map[string]any) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.base+path, strings.NewReader(body))
	if csrf {
		req.Header.Set("X-Viceroy-CSRF", "1")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func TestSetupLoginFlow(t *testing.T) {
	c := newTestServer(t)

	code, body := c.do("GET", "/api/session", "", false)
	if code != 200 || body["needs_setup"] != true || body["user"] != nil {
		t.Fatalf("fresh session = %d %v", code, body)
	}

	setup := `{"name":"Lucas","email":"lucas@example.com","password":"correct horse battery","household_name":"Home"}`
	if code, _ := c.do("POST", "/api/setup", setup, false); code != 403 {
		t.Fatalf("setup without CSRF header = %d, want 403", code)
	}
	if code, body := c.do("POST", "/api/setup", setup, true); code != 200 {
		t.Fatalf("setup = %d %v", code, body)
	}
	code, body = c.do("GET", "/api/session", "", false)
	if body["needs_setup"] != false || body["user"] == nil || body["household"].(map[string]any)["name"] != "Home" {
		t.Fatalf("after setup session = %v", body)
	}
	if code, _ := c.do("POST", "/api/setup", setup, true); code != 409 {
		t.Fatalf("second setup = %d, want 409", code)
	}

	if code, _ := c.do("POST", "/api/auth/logout", "", true); code != 204 {
		t.Fatalf("logout = %d", code)
	}
	if _, body := c.do("GET", "/api/session", "", false); body["user"] != nil {
		t.Fatalf("still signed in after logout: %v", body)
	}

	if code, _ := c.do("POST", "/api/auth/login", `{"email":"lucas@example.com","password":"nope nope nope"}`, true); code != 401 {
		t.Fatalf("bad login = %d", code)
	}
	if code, _ := c.do("POST", "/api/auth/login", `{"email":"lucas@example.com","password":"correct horse battery"}`, true); code != 200 {
		t.Fatalf("login = %d", code)
	}
	if _, body := c.do("GET", "/api/session", "", false); body["user"] == nil {
		t.Fatal("not signed in after login")
	}
}

func TestLoginRateLimit(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"L","email":"l@example.com","password":"correct horse battery"}`, true)
	var code int
	for i := 0; i < 11; i++ {
		code, _ = c.do("POST", "/api/auth/login", `{"email":"l@example.com","password":"wrong wrong wrong"}`, true)
	}
	if code != 429 {
		t.Fatalf("11th failed login = %d, want 429", code)
	}
}

func TestSPAFallback(t *testing.T) {
	c := newTestServer(t)
	resp, err := c.http.Get(c.base + "/budget/2026-09")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(b), "app") {
		t.Fatalf("client route = %d %q", resp.StatusCode, b)
	}
	resp, _ = c.http.Get(c.base + "/assets/missing.js")
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("missing asset = %d, want 404", resp.StatusCode)
	}
	if code, _ := c.do("GET", "/api/nope", "", false); code != 404 {
		t.Fatalf("unknown api route = %d", code)
	}
}

func TestManifestContentType(t *testing.T) {
	web := fstest.MapFS{"index.html": {Data: []byte("x")}, "manifest.webmanifest": {Data: []byte("{}")}}
	rec := httptest.NewRecorder()
	spaHandler(web).ServeHTTP(rec, httptest.NewRequest("GET", "/manifest.webmanifest", nil))
	if ct := rec.Header().Get("Content-Type"); ct != "application/manifest+json" {
		t.Fatalf("content type = %q", ct)
	}
}
