package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"tgguard/internal/config"
	"tgguard/internal/service"
	"tgguard/internal/state"
)

func authServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	r := miniredis.RunT(t)
	cache := state.New(r.Addr(), "")
	t.Cleanup(func() { cache.R.Close() })
	s := &Server{Service: &service.Service{State: cache}, Config: config.Config{AdminToken: strings.Repeat("k", 32)}}
	return s, s.Handler()
}
func TestSessionRequiresCSRFAndRejectsCrossOrigin(t *testing.T) {
	s, h := authServer(t)
	r := httptest.NewRequest("POST", "http://example.com/auth/login", strings.NewReader(`{"token":"`+s.Config.AdminToken+`"}`))
	r.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe cookie")
	}
	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["csrf"] == "" || strings.Contains(w.Body.String(), s.Config.AdminToken) {
		t.Fatal("invalid login response")
	}
	for _, tc := range []struct {
		csrf, origin string
		status       int
	}{{"", "http://example.com", 403}, {result["csrf"], "https://evil.example", 403}, {result["csrf"], "http://example.com", 200}} {
		r = httptest.NewRequest("POST", "http://example.com/api/v1/panel-ticket", strings.NewReader(`{}`))
		r.AddCookie(cookies[0])
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("X-CSRF-Token", tc.csrf)
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("got %d want %d", w.Code, tc.status)
		}
	}
}
func TestPanelTicketOneUseAndNoLongLivedCredential(t *testing.T) {
	s, h := authServer(t)
	r := httptest.NewRequest("POST", "/api/v1/panel-ticket", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer "+s.Config.AdminToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var v map[string]string
	json.Unmarshal(w.Body.Bytes(), &v)
	ticket := v["ticket"]
	if len(ticket) != 32 || ticket == s.Config.AdminToken {
		t.Fatal("invalid ticket")
	}
	for i := 0; i < 2; i++ {
		r = httptest.NewRequest("POST", "/auth/ticket", strings.NewReader(`{"ticket":"`+ticket+`"}`))
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 200
		if i == 1 {
			want = 401
		}
		if w.Code != want {
			t.Fatal(w.Code, want)
		}
	}
}
func TestPublicPanelAssetsNoCredentials(t *testing.T) {
	s, h := authServer(t)
	for _, path := range []string{"/", "/assets/app.js", "/assets/style.css"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || strings.Contains(w.Body.String(), s.Config.AdminToken) {
			t.Fatal(path, w.Code)
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'self'") {
			t.Fatal("CSP missing")
		}
	}
}
func TestSessionLogoutRevokesAccess(t *testing.T) {
	s, h := authServer(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"token":"`+s.Config.AdminToken+`"}`)))
	cookie := w.Result().Cookies()[0]
	var v map[string]string
	json.Unmarshal(w.Body.Bytes(), &v)
	r := httptest.NewRequest("POST", "/auth/logout", strings.NewReader(`{}`))
	r.AddCookie(cookie)
	r.Header.Set("X-CSRF-Token", v["csrf"])
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	r = httptest.NewRequest("GET", "/auth/session", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("session survived logout")
	}
}

func TestTicketSurvivesRedisFailureAndSessionCreatedAtomically(t *testing.T) {
	redis := miniredis.RunT(t)
	cache := state.New(redis.Addr(), "")
	defer cache.R.Close()
	server := &Server{Service: &service.Service{State: cache}, Config: config.Config{AdminToken: strings.Repeat("k", 32)}}
	h := server.Handler()
	ticket := strings.Repeat("t", 32)
	if e := cache.R.Set(context.Background(), "web:ticket:"+state.Hash(ticket), "1", time.Minute).Err(); e != nil {
		t.Fatal(e)
	}
	exchange := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/auth/ticket", strings.NewReader(`{"ticket":"`+ticket+`"}`))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	redis.SetError("temporary outage")
	if w := exchange(); w.Code != 503 {
		t.Fatal(w.Code)
	}
	redis.SetError("")
	if !redis.Exists("web:ticket:" + state.Hash(ticket)) {
		t.Fatal("ticket consumed during failure")
	}
	w := exchange()
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !redis.Exists("web:session:"+state.Hash(cookies[0].Value)) || redis.Exists("web:ticket:"+state.Hash(ticket)) {
		t.Fatal("session and ticket inconsistent")
	}
	if w = exchange(); w.Code != 401 {
		t.Fatal("ticket reused", w.Code)
	}
}
