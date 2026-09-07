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
	"unicode/utf16"
)

func TestWelcomeTemplateAndMandatoryVerification(t *testing.T) {
	s := domain.DefaultSettings()
	s.WelcomeText = "你好 {name} {username}，欢迎来到 {group}！ID {user_id}；{timeout} 秒"
	chat := domain.Chat{Title: "测试群"}
	user := domain.User{ID: 7, FirstName: "新人", Username: "newuser"}
	text := welcomeText(s, chat, user, false)
	for _, want := range []string{"新人", "@newuser", "测试群", "ID 7", "180 秒"} {
		if !strings.Contains(text, want) {
			t.Fatal(text)
		}
	}
	if prompt := welcomeText(s, chat, user, true); strings.Contains(prompt, "你好") || !strings.Contains(prompt, "验证按钮") {
		t.Fatal("welcome sent before verification", prompt)
	}
	s.WelcomeEnabled = false
	text = welcomeText(s, chat, user, true)
	if strings.Contains(text, "你好") || !strings.Contains(text, "验证按钮") {
		t.Fatal("verification entry lost", text)
	}
	if text = welcomeText(s, chat, user, false); text != "" {
		t.Fatal("disabled welcome rendered", text)
	}
	s.WelcomeEnabled = true
	s.WelcomeText = strings.Repeat("{group}", 140)
	chat.Title = strings.Repeat("群", 255)
	if len([]rune(welcomeText(s, chat, user, false))) > 4096 {
		t.Fatal("expanded template too long")
	}
}

func TestWelcomeMentionsUTF16AndLiteralTemplates(t *testing.T) {
	v := domain.DefaultSettings()
	v.WelcomeEnabled = true
	v.WelcomeText = "😀欢迎 {name} {username} {user_id}，{group} <b>原文</b>"
	u := domain.User{ID: 42, FirstName: "新😀人", LastName: "<&>"}
	chat := domain.Chat{Title: "群😀"}
	text, entities := welcomeMessage(v, chat, u, false)
	units := utf16.Encode([]rune(text))
	if len(entities) != 3 {
		t.Fatal(entities)
	}
	for i, e := range entities {
		if e.Type != "text_link" || e.URL != "tg://user?id=42" {
			t.Fatal(e)
		}
		label := string(utf16.Decode(units[e.Offset : e.Offset+e.Length]))
		want := "新😀人 <&>"
		if i == 2 {
			want = "42"
		}
		if label != want {
			t.Fatal(label, want)
		}
	}
	if !strings.Contains(text, "<b>原文</b>") {
		t.Fatal("template interpreted as markup")
	}
	v.WelcomeText = "欢迎加入"
	text, entities = welcomeMessage(v, chat, u, false)
	if len(entities) != 1 || !strings.Contains(text, "欢迎加入") {
		t.Fatal(text, entities)
	}
	u.Username = "alice"
	text, entities = welcomeMessage(v, chat, u, true)
	if !strings.HasPrefix(text, "@alice") || len(entities) != 1 {
		t.Fatal(text, entities)
	}
	v.WelcomeText = strings.Repeat("😀", 1498) + "{name}"
	text, entities = welcomeMessage(v, chat, u, false)
	if len(utf16.Encode([]rune(text))) > 3000 {
		t.Fatal("message exceeds limit")
	}
	for _, e := range entities {
		if e.Offset+e.Length > len(utf16.Encode([]rune(text))) {
			t.Fatal("entity exceeds text")
		}
	}
}
func TestSendWelcomeCarriesMentionAndVerificationButton(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text     string
			Entities []domain.Entity
			Markup   json.RawMessage `json:"reply_markup"`
		}
		if e := json.NewDecoder(r.Body).Decode(&in); e != nil {
			t.Error(e)
		}
		if len(in.Entities) != 1 || in.Entities[0].URL != "tg://user?id=42" || len(in.Markup) == 0 {
			t.Error("mention or button missing", in)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"result":{"message_id":99}}`))
	}))
	defer server.Close()
	svc := Service{Bot: &telegram.Client{BaseURL: server.URL, HTTP: server.Client()}}
	id, e := svc.sendWelcome(context.Background(), domain.Chat{ID: -1001}, domain.User{ID: 42, FirstName: "新人"}, domain.DefaultSettings(), true, map[string]any{"inline_keyboard": [][]map[string]string{{{"text": "验证", "url": "https://example.com"}}}})
	if e != nil || id != 99 {
		t.Fatal(id, e)
	}
}
