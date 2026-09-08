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
		for _, needle := range []string{"inline_keyboard", "menu:profile", "menu:verify"} {
			if !strings.Contains(string(b), needle) {
				t.Error("missing", needle)
			}
		}
		for _, needle := range []string{"menu:admins", "menu:ai", "menu:panel", "menu:groups", "startgroup"} {
			if strings.Contains(string(b), needle) {
				t.Error("ordinary group administrator saw deployment menu", needle)
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

func TestPrivateHomeRowsMatchRole(t *testing.T) {
	for _, tc := range []struct{ manage, super bool }{{false, false}, {true, false}, {true, true}} {
		b, err := json.Marshal(privateHomeRows(tc.manage, tc.super, "testbot"))
		if err != nil {
			t.Fatal(err)
		}
		text := string(b)
		if strings.Contains(text, "menu:groups") != tc.manage || strings.Contains(text, "menu:panel") != tc.super {
			t.Fatal("wrong menu visibility", tc, text)
		}
		if !strings.Contains(text, "menu:verify") {
			t.Fatal("missing personal verification")
		}
	}
}

func TestRegisteredGroupMenusDoNotOfferVerificationCommand(t *testing.T) {
	scopes := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		if e := json.NewDecoder(r.Body).Decode(&in); e != nil {
			t.Fatal(e)
		}
		if r.URL.Path == "/setMyCommands" {
			scope := in["scope"].(map[string]any)["type"].(string)
			scopes[scope] = true
			for _, raw := range in["commands"].([]any) {
				if scope == "all_private_chats" {
					switch raw.(map[string]any)["command"] {
					case "groups", "settings", "rules", "stats", "keywords", "whitelist", "blacklist":
						t.Error("management command leaked into private default menu")
					}
				}
				if scope == "all_group_chats" && raw.(map[string]any)["command"] == "id" {
					t.Error("ID utility leaked into member menu")
				}
				if (scope != "all_private_chats" && raw.(map[string]any)["command"] == "verify") || raw.(map[string]any)["command"] == "help" {
					t.Error("unusable verification command registered", scope)
				}
			}
		}
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()
	s := &Service{Bot: &telegram.Client{BaseURL: srv.URL, HTTP: srv.Client()}}
	if e := s.RegisterMenus(context.Background()); e != nil {
		t.Fatal(e)
	}
	for _, scope := range []string{"all_group_chats", "all_chat_administrators", "all_private_chats"} {
		if !scopes[scope] {
			t.Error("menu scope not updated", scope)
		}
	}
}
