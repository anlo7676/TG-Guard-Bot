package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRestoreUsesGroupDefaultPermissions(t *testing.T) {
	var restored bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/getChat" {
			w.Write([]byte(`{"ok":true,"result":{"id":-1,"permissions":{"can_send_messages":true,"can_send_photos":false}}}`))
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		p := body["permissions"].(map[string]any)
		if p["can_send_photos"] != false || p["can_send_messages"] != true {
			t.Error("permissions escalated")
		}
		restored = true
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if e := c.Restore(context.Background(), -1, 42); e != nil || !restored {
		t.Fatalf("%v %v", e, restored)
	}
}
func TestKickIsBanThenUnban(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.URL.Path)
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if e := c.Kick(context.Background(), -1, 42); e != nil {
		t.Fatal(e)
	}
	if strings.Join(methods, ",") != "/banChatMember,/unbanChatMember" {
		t.Fatal(methods)
	}
}
func TestDeleteOnlyIgnoresMissingMessage(t *testing.T) {
	for _, desc := range []string{"Bad Request: message to delete not found", "Bad Request: not enough rights to delete"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 400, "description": desc})
		}))
		c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
		e := c.Delete(context.Background(), -1, 1)
		if (e == nil) != strings.Contains(desc, "not found") {
			t.Fatalf("%s %v", desc, e)
		}
		srv.Close()
	}
}
func TestFloodWaitCancellable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":false,"error_code":429,"parameters":{"retry_after":60}}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if e := c.Call(ctx, "sendMessage", map[string]any{}, nil); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
}
func TestNetworkErrorDoesNotLeakToken(t *testing.T) {
	c := New("sensitive-bot-token", nil)
	c.BaseURL = "http://127.0.0.1:1/botsensitive-bot-token"
	if e := c.Call(context.Background(), "getMe", map[string]any{}, nil); e == nil || strings.Contains(e.Error(), "sensitive-bot-token") {
		t.Fatal(e)
	}
}
