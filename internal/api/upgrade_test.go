package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpgradesRequireAuthenticationAndMaster(t *testing.T) {
	s, h := authServer(t)
	for _, path := range []string{"/api/v1/upgrades", "/api/v1/upgrades/check"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Fatal(path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/upgrades", strings.NewReader(`{"version":"v1.10.0"}`))
	r = r.WithContext(context.WithValue(r.Context(), actorKey{}, int64(42)))
	s.upgrades(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/v1/upgrades", nil)
	r.Header.Set("Authorization", "Bearer "+s.Config.AdminToken)
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
