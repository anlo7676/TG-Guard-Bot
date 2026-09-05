package service

import (
	"strings"
	"testing"
	"tgguard/internal/domain"
)

func TestWelcomeTemplateAndMandatoryVerification(t *testing.T) {
	s := domain.DefaultSettings()
	s.WelcomeText = "你好 {name} {username}，欢迎来到 {group}！ID {user_id}；{timeout} 秒"
	chat := domain.Chat{Title: "测试群"}
	user := domain.User{ID: 7, FirstName: "新人", Username: "newuser"}
	text := welcomeText(s, chat, user, true)
	for _, want := range []string{"新人", "@newuser", "测试群", "ID 7", "180 秒", "验证期间"} {
		if !strings.Contains(text, want) {
			t.Fatal(text)
		}
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
	if len([]rune(welcomeText(s, chat, user, true))) > 4096 {
		t.Fatal("expanded template too long")
	}
}
