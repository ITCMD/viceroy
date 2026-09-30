package server

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func prefixes(ss ...string) []netip.Prefix {
	var out []netip.Prefix
	for _, s := range ss {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

func TestIPAllow(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(ClientIP(r).String()))
	})
	cases := []struct {
		name    string
		allowed []netip.Prefix
		proxies []netip.Prefix
		remote  string
		xff     string
		want    int
		wantIP  string
	}{
		{"localhost allowed", prefixes("127.0.0.1/32"), nil, "127.0.0.1:5000", "", 200, "127.0.0.1"},
		{"lan denied by default", prefixes("127.0.0.1/32"), nil, "192.168.0.20:5000", "", 403, ""},
		{"lan subnet allowed", prefixes("192.168.0.0/24"), nil, "192.168.0.20:5000", "", 200, "192.168.0.20"},
		{"xff ignored from untrusted peer", prefixes("127.0.0.1/32"), nil, "10.0.0.5:5000", "127.0.0.1", 403, ""},
		{"xff honored from trusted proxy", prefixes("192.168.0.0/24"), prefixes("127.0.0.1/32"), "127.0.0.1:5000", "192.168.0.20", 200, "192.168.0.20"},
		{"proxy passes outside client", prefixes("192.168.0.0/24"), prefixes("127.0.0.1/32"), "127.0.0.1:5000", "8.8.8.8", 403, ""},
		{"spoofed leftmost xff ignored", prefixes("192.168.0.0/24"), prefixes("127.0.0.1/32"), "127.0.0.1:5000", "192.168.0.9, 8.8.8.8", 403, ""},
		{"chained trusted proxies", prefixes("192.168.0.0/24"), prefixes("127.0.0.1/32", "10.0.0.0/8"), "127.0.0.1:5000", "192.168.0.20, 10.0.0.2", 200, "192.168.0.20"},
		{"ipv4-mapped ipv6 peer", prefixes("127.0.0.1/32"), nil, "[::ffff:127.0.0.1]:5000", "", 200, "127.0.0.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = c.remote
			if c.xff != "" {
				req.Header.Set("X-Forwarded-For", c.xff)
			}
			rec := httptest.NewRecorder()
			IPAllow(c.allowed, c.proxies)(ok).ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("code = %d, want %d", rec.Code, c.want)
			}
			if c.wantIP != "" && rec.Body.String() != c.wantIP {
				t.Fatalf("client ip = %q, want %q", rec.Body.String(), c.wantIP)
			}
		})
	}
}
