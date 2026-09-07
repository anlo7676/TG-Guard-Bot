package service

import (
	"strings"
	"testing"
	"tgguard/internal/domain"
	"tgguard/internal/store"
)

func TestWarningExplainsIdentitySourceAndActualPolicy(t *testing.T) {
	s := domain.DefaultSettings()
	l := store.Log{UserID: 42, Source: "automatic", AI: &domain.AIResult{IsAd: true, Confidence: .92}, Decision: domain.Decision{Action: "warn", Delete: true}}
	text := warningNotice(l, domain.User{Username: "alice"}, s, 0)
	for _, want := range []string{"@alice", "tg://user?id=42", "AI 复核", "92%", "原消息已删除", "第 1 次", "第 2 次及以后禁言 1 小时", "未开启累计自动封禁", "删除并警告规则不会"} {
		if !strings.Contains(text, want) {
			t.Fatal(want, text)
		}
	}
	l.Risk.Spam = true
	text = warningNotice(l, domain.User{FirstName: "<张&三>"}, s, 1)
	if !strings.Contains(text, "&lt;张&amp;三&gt;") || !strings.Contains(text, "本地刷屏") || strings.Contains(text, "AI 复核") {
		t.Fatal(text)
	}
	s.AutoMute = false
	s.AutoBan = true
	s.BanAfter = 5
	l.Source = "manual"
	l.Decision.Delete = false
	text = warningNotice(l, domain.User{}, s, 8)
	for _, want := range []string{"用户 42", "管理员人工警告", "未开启累计自动禁言", "第 5 次及以后封禁"} {
		if !strings.Contains(text, want) {
			t.Fatal(want, text)
		}
	}
	if strings.Contains(text, "第 9 次") || strings.Contains(text, "原消息已删除") {
		t.Fatal(text)
	}
}
