package service

import (
	"context"
	"encoding/json"
	"github.com/alicebob/miniredis/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"tgguard/internal/domain"
	"tgguard/internal/state"
	"tgguard/internal/telegram"
)

func TestDCCommandTargetsAndUnavailable(t *testing.T) {
	var mu sync.Mutex
	var targets []int64
	var messages []string
	tg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/getUserProfilePhotos" {
			targets = append(targets, int64(in["user_id"].(float64)))
			w.Write([]byte(`{"ok":true,"result":{"photos":[]}}`))
		} else {
			messages = append(messages, in["text"].(string))
			w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
		}
	}))
	defer tg.Close()
	redis := miniredis.RunT(t)
	cache := state.New(redis.Addr(), "")
	defer cache.R.Close()
	s := Service{State: cache, Bot: &telegram.Client{HTTP: tg.Client(), BaseURL: tg.URL}}
	m := domain.Message{Chat: domain.Chat{ID: 42, Type: "private"}, From: &domain.User{ID: 42}}
	for _, arg := range []string{"", "123456", "@unknown"} {
		if e := s.Command(context.Background(), 1, m, "dc", arg); e != nil {
			t.Fatal(e)
		}
	}
	m.Reply = &domain.Message{From: &domain.User{ID: 77}}
	if e := s.Command(context.Background(), 2, m, "dc", ""); e != nil {
		t.Fatal(e)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(targets) != 3 || targets[0] != 42 || targets[1] != 123456 || targets[2] != 77 {
		t.Fatal(targets)
	}
	if !strings.Contains(messages[0], "暂时无法查询") || !strings.Contains(messages[2], "用法") {
		t.Fatal(messages)
	}
}
