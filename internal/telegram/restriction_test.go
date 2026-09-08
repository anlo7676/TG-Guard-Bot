package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestShortRestrictionUsesSafetyWindow(t *testing.T) {
	var remaining int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		if e := json.NewDecoder(r.Body).Decode(&b); e != nil {
			t.Error(e)
		}
		remaining = int64(b["until_date"].(float64)) - time.Now().Unix()
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()
	c := New("fake", nil)
	c.BaseURL = srv.URL
	if err := c.Restrict(context.Background(), -100, 77, 30); err != nil {
		t.Fatal(err)
	}
	if remaining < 90 {
		t.Fatal("short request could become permanent", remaining)
	}
}
