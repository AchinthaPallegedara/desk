package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		name, remote, xff, want string
	}{
		{"direct public client", "203.0.113.9:5000", "", "203.0.113.9"},
		{"public client cannot spoof", "203.0.113.9:5000", "1.2.3.4", "203.0.113.9"},
		{"npm -> next -> go", "172.18.0.5:4000", "198.51.100.7, 172.18.0.3", "198.51.100.7"},
		{"spoofed prefix ignored", "172.18.0.5:4000", "6.6.6.6, 198.51.100.7, 172.18.0.3", "198.51.100.7"},
		{"loopback dev proxy", "127.0.0.1:4000", "198.51.100.7", "198.51.100.7"},
		{"only private hops", "172.18.0.5:4000", "172.18.0.3", "172.18.0.3"},
		{"cloudflare -> npm -> next -> go", "172.18.0.5:4000", "198.51.100.7, 172.70.1.20, 172.18.0.3", "198.51.100.7"},
		{"cloudflare ipv6 edge", "172.18.0.5:4000", "198.51.100.7, 2606:4700::1, 172.18.0.3", "198.51.100.7"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := clientIP(r); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}
