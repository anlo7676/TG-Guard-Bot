package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tgguard/internal/config"
	"tgguard/internal/service"
)

func TestUnauthorizedNeverTouchesStorage(t *testing.T) {
	s := &Server{Service: &service.Service{}, Config: config.Config{AdminToken: strings.Repeat("x", 32), Mode: "webhook", WebhookSecret: strings.Repeat("y", 32)}}
	for _, path := range []string{"/api/v1/groups", "/api/v1/groups/-123/settings", "/api/v1/queue/dead"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/telegram/webhook", strings.NewReader(`{}`)))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}
func TestCrossOriginWriteRejected(t *testing.T) {
	token := strings.Repeat("x", 32)
	s := &Server{Service: &service.Service{}, Config: config.Config{AdminToken: token}}
	r := httptest.NewRequest("PUT", "/api/v1/groups/-1/settings", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
func TestBodyValidation(t *testing.T) {
	for _, body := range []string{`{"name":"x"}{}`, `{"unknown":1}`, strings.Repeat("x", 500)} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		w := httptest.NewRecorder()
		var out struct {
			Name string `json:"name"`
		}
		if decode(w, r, &out, 100, true) {
			t.Fatalf("accepted %q", body)
		}
	}
}
func TestSecretEqual(t *testing.T) {
	if SecretEqual("", "") || SecretEqual("secret", "wrong") || !SecretEqual("secret", "secret") {
		t.Fatal("invalid secret comparison")
	}
}
func TestWebhookBodyRejectedBeforeStorage(t *testing.T) {
	secret := strings.Repeat("y", 32)
	s := &Server{Service: &service.Service{}, Config: config.Config{Mode: "webhook", WebhookSecret: secret}}
	for _, body := range []string{`{"update_id":0}`, `{"update_id":1}{}`, `broken`} {
		r := httptest.NewRequest("POST", "/telegram/webhook", strings.NewReader(body))
		r.Header.Set("X-Telegram-Bot-Api-Secret-Token", secret)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("%s: %d", body, w.Code)
		}
	}
}
