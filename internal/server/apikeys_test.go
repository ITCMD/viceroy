package server

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// Every /api route must be documented, and every documented route must exist.
func TestAPIDocsCoverRoutes(t *testing.T) {
	c := newTestServer(t)
	routes := map[string]bool{}
	chi.Walk(c.server.Handler().(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/api/") && route != "/api/*" {
			routes[method+" "+strings.TrimPrefix(route, "/api")] = true
		}
		return nil
	})
	documented := map[string]bool{}
	for _, d := range apiDocs {
		documented[d.Method+" "+d.Path] = true
	}
	var missing, extra []string
	for r := range routes {
		if !documented[r] {
			missing = append(missing, r)
		}
	}
	for d := range documented {
		if !routes[d] {
			extra = append(extra, d)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing)+len(extra) > 0 {
		t.Fatalf("undocumented routes: %v\ndocumented but missing: %v", missing, extra)
	}
}

// bearer sends a request with an API key instead of the session cookie.
func (c *client) bearer(key, method, path, body string) (int, string) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.base+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req) // no cookie jar
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func TestAPIKeys(t *testing.T) {
	c := newTestServer(t)
	c.do("POST", "/api/setup", `{"name":"A","email":"a@example.com","password":"correct horse battery"}`, true)

	_, st := c.do("GET", "/api/settings/api", "", false)
	if st["enabled"] != false || st["can_edit"] != true {
		t.Fatalf("initial = %v", st)
	}
	if code, _ := c.do("POST", "/api/settings/api/keys", `{"name":"","scope":"read"}`, true); code != 400 {
		t.Fatalf("unnamed key = %d", code)
	}
	_, w := c.do("POST", "/api/settings/api/keys", `{"name":"Home Assistant","scope":"write"}`, true)
	_, r := c.do("POST", "/api/settings/api/keys", `{"name":"Grafana","scope":"read"}`, true)
	writeKey, readKey := w["key"].(string), r["key"].(string)
	if !strings.HasPrefix(writeKey, "vk_") || len(writeKey) != 43 {
		t.Fatalf("key = %q", writeKey)
	}

	// Off by default: keys are refused.
	if code, body := c.bearer(readKey, "GET", "/api/accounts", ""); code != 403 || !strings.Contains(body, "turned off") {
		t.Fatalf("api off = %d %s", code, body)
	}
	c.do("PATCH", "/api/settings/api", `{"enabled":true}`, true)

	if code, body := c.bearer(readKey, "GET", "/api/accounts", ""); code != 200 || !strings.Contains(body, "Paper Cash") {
		t.Fatalf("read key GET = %d %s", code, body)
	}
	if code, body := c.bearer(readKey, "POST", "/api/accounts", `{"name":"X","type":"cash"}`); code != 403 || !strings.Contains(body, "read-only") {
		t.Fatalf("read key POST = %d %s", code, body)
	}
	// Write keys need no CSRF header.
	if code, body := c.bearer(writeKey, "POST", "/api/accounts", `{"name":"From API","type":"cash","balance":"5"}`); code != 201 && code != 200 {
		t.Fatalf("write key POST = %d %s", code, body)
	}
	// Keys can't manage keys.
	if code, _ := c.bearer(writeKey, "POST", "/api/settings/api/keys", `{"name":"sneaky","scope":"write"}`); code != 403 {
		t.Fatalf("key creating keys = %d", code)
	}
	if code, _ := c.bearer("vk_nope", "GET", "/api/accounts", ""); code != 401 {
		t.Fatalf("bad key = %d", code)
	}
	if code, body := c.bearer(readKey, "GET", "/api/openapi.json", ""); code != 200 || !strings.Contains(body, `"/api/transactions/{id}"`) {
		t.Fatalf("openapi = %d", code)
	}

	_, st = c.do("GET", "/api/settings/api", "", false)
	keys := st["keys"].([]any)
	if len(keys) != 2 || keys[0].(map[string]any)["last_used_at"] == nil || keys[0].(map[string]any)["prefix"] != writeKey[:9] {
		t.Fatalf("keys = %v", keys)
	}
	if strings.Contains(fmt.Sprint(st), writeKey) {
		t.Fatal("full key listed")
	}
	c.do("DELETE", fmt.Sprint("/api/settings/api/keys/", keys[0].(map[string]any)["id"]), "", true)
	if code, _ := c.bearer(writeKey, "GET", "/api/accounts", ""); code != 401 {
		t.Fatalf("revoked key = %d", code)
	}
}
