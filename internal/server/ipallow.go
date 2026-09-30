package server

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type ctxKey int

const clientIPKey ctxKey = iota

// ClientIP returns the client address resolved by IPAllow.
func ClientIP(r *http.Request) netip.Addr {
	a, _ := r.Context().Value(clientIPKey).(netip.Addr)
	return a
}

func contains(ps []netip.Prefix, a netip.Addr) bool {
	for _, p := range ps {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// resolveClientIP uses the TCP peer, unless the peer is a trusted proxy, in which
// case it walks X-Forwarded-For right-to-left and takes the first untrusted hop.
func resolveClientIP(r *http.Request, proxies []netip.Prefix) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	peer = peer.Unmap()
	if !contains(proxies, peer) {
		return peer, true
	}
	var hops []string
	for _, h := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(h, ",")...)
	}
	client := peer
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return netip.Addr{}, false
		}
		client = a.Unmap()
		if !contains(proxies, client) {
			break
		}
	}
	return client, true
}

// IPAllow rejects clients outside allowed and stores the client IP in the context.
func IPAllow(allowed, proxies []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, ok := resolveClientIP(r, proxies)
			if !ok || !contains(allowed, ip) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey, ip)))
		})
	}
}
