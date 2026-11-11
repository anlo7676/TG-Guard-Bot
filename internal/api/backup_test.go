package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"tgguard/internal/config"
	"tgguard/internal/service"
)

func TestDatabaseEndpointsRequireAuthenticationAndConfirmation(t *testing.T) {
	s := &Server{Config: config.Config{AdminToken: strings.Repeat("a", 32)}, Service: &service.Service{Maintenance: &service.Maintenance{}}}
	h := s.Handler()
	for _, path := range []string{"/api/v1/database/backup", "/api/v1/database/restore"} {
		method := "GET"
		if strings.HasSuffix(path, "restore") {
			method = "POST"
		}
		r := httptest.NewRequest(method, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/api/v1/database/restore", nil)
	r.Header.Set("Authorization", "Bearer "+s.Config.AdminToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	r.Header.Set("X-Confirm-Restore", "RESTORE")
	r.Header.Set("Origin", "https://other.test")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	if err := s.Service.Maintenance.Pause(context.Background()); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("GET", "/api/v1/database/backup", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal("maintenance admitted backup", w.Code)
	}
}
