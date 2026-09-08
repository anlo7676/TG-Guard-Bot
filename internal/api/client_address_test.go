package api

import (
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestProxyClientIsolationAndSpoofing(t *testing.T) {
	s, h := authServer(t)
	s.Config.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("172.20.0.5/32")}
	call := func(ip, token string) int {
		r := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"token":"`+token+`"}`))
		r.RemoteAddr = "172.20.0.5:40000"
		r.Header.Set("X-Forwarded-For", ip)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	for i := 0; i < 10; i++ {
		if code := call("198.51.100.10", "bad"); code != 401 {
			t.Fatal(code)
		}
	}
	if code := call("203.0.113.20", s.Config.AdminToken); code != 200 {
		t.Fatal("different client blocked", code)
	}
	for _, tc := range []struct{ remote, header, want string }{
		{"203.0.113.8:123", "192.0.2.1", "203.0.113.8"},
		{"172.20.0.5:123", "192.0.2.1, 203.0.113.8", "203.0.113.8"},
		{"172.20.0.5:123", "invalid", "172.20.0.5"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.remote
		r.Header.Set("X-Forwarded-For", tc.header)
		if got := s.clientAddress(r); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
}
