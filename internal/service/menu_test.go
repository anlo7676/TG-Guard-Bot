package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"tgguard/internal/domain"
	"tgguard/internal/telegram"
)

func TestPrivateHomeHasInteractiveMenu(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		if e := json.NewDecoder(r.Body).Decode(&in); e != nil {
			t.Fatal(e)
		}
		b, _ := json.Marshal(in)
		for _, needle := range []string{"inline_keyboard", "menu:profile", "menu:admins", "menu:ai", "menu:panel"} {
			if !strings.Contains(string(b), needle) {
				t.Error("missing", needle)
			}
		}
		w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer srv.Close()
	s := &Service{Bot: &telegram.Client{Username: "guardbot", BaseURL: srv.URL, HTTP: srv.Client()}}
	if e := s.Home(context.Background(), domain.Message{Chat: domain.Chat{ID: 42, Type: "private"}, From: &domain.User{ID: 42, FirstName: "User"}}); e != nil {
		t.Fatal(e)
	}
}
func TestMenuDoesNotAcceptAnotherUsersPrivateChat(t *testing.T) {
	s := &Service{}
	if e := s.MenuCallback(context.Background(), domain.Callback{From: domain.User{ID: 99}, Message: &domain.Message{Chat: domain.Chat{ID: 42, Type: "private"}}, Data: "menu:admins"}); e != nil {
		t.Fatal(e)
	}
}
