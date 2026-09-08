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
		} else if r.URL.Path == "/getChat" {
			w.Write([]byte(`{"ok":true,"result":{"id":123456,"first_name":"查询目标","last_name":"甲"}}`))
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
	m := domain.Message{Chat: domain.Chat{ID: 42, Type: "private"}, From: &domain.User{ID: 42, FirstName: "本人"}}
	for _, arg := range []string{"", "123456", "@unknown"} {
		if e := s.Command(context.Background(), 1, m, "dc", arg); e != nil {
			t.Fatal(e)
		}
	}
	m.Reply = &domain.Message{From: &domain.User{ID: 77, FirstName: "被回复用户"}}
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
	for i, name := range map[int]string{0: "本人", 1: "查询目标 甲", 3: "被回复用户"} {
		if !strings.HasPrefix(messages[i], "用户昵称/姓名："+name+"\n用户 ID：") {
			t.Fatal(messages[i])
		}
	}
}

func TestDataCenterReplyRegionsAndLimits(t *testing.T) {
	for dc, region := range map[int]string{1: "美国 · 迈阿密", 2: "荷兰 · 阿姆斯特丹", 3: "美国 · 迈阿密", 4: "荷兰 · 阿姆斯特丹", 5: "新加坡", 6: "未知地区"} {
		got := dataCenterReply("测试姓名", 42, dc)
		if !strings.HasPrefix(got, "用户昵称/姓名：测试姓名\n用户 ID：42\n") {
			t.Fatal(got)
		}
		for _, want := range []string{"用户 ID：42", "数据中心：DC", region, "可见头像存储位置", "仅供参考"} {
			if !strings.Contains(got, want) {
				t.Fatalf("DC%d missing %q: %s", dc, want, got)
			}
		}
		if strings.Contains(got, "头像数据中心：") {
			t.Fatal(got)
		}
	}
}

func TestDCNameFallbackDoesNotMisidentifyTarget(t *testing.T) {
	tg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"ok":false,"error_code":403,"description":"unavailable"}`))
	}))
	defer tg.Close()
	s := Service{Bot: &telegram.Client{HTTP: tg.Client(), BaseURL: tg.URL}}
	m := domain.Message{From: &domain.User{ID: 42, FirstName: "本人"}}
	if got := s.dataCenterName(context.Background(), m, 77); got != "暂未获取" {
		t.Fatal(got)
	}
	m.From = &domain.User{ID: 42, Username: "test_user"}
	if got := s.dataCenterName(context.Background(), m, 42); got != "@test_user" {
		t.Fatal(got)
	}
	m.From.FirstName, m.From.LastName = "小\n明", " 张 "
	if got := s.dataCenterName(context.Background(), m, 42); got != "小 明 张" {
		t.Fatal(got)
	}
}
