package service

import (
	"strings"
	"testing"

	"tgguard/internal/domain"
	"tgguard/internal/store"
)

func TestReviewNoticeReflectsActualOutcome(t *testing.T) {
	l := store.Log{UserID: 123, AI: &domain.AIResult{IsAd: true, Confidence: .87, Category: "scam", Reason: "<b>引流</b>"}}
	p := store.Punishment{Status: "done", Decision: domain.Decision{Action: "warn", Delete: true}}
	text := reviewNotice(l, domain.User{FirstName: "<张三>"}, domain.DefaultSettings(), p)
	for _, want := range []string{"诈骗引流", "87%", "&lt;张三&gt;", "&lt;b&gt;引流&lt;/b&gt;", "禁言 1 小时", "tg://user?id=123"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	for _, a := range reviewActions(p) {
		if a.action == "delete" {
			t.Fatal("completed deletion still offered")
		}
	}
	p.Decision = domain.Decision{Action: "mute", Delete: true, Duration: 3600}
	for _, a := range reviewActions(p) {
		if a.action == "delete" || a.action == "mute" {
			t.Fatal("completed action still offered")
		}
	}
	p.Status = "skipped"
	text = reviewNotice(l, domain.User{}, domain.DefaultSettings(), p)
	if strings.Contains(text, "已禁言") || strings.Contains(text, "原消息已删除") {
		t.Fatal("skipped outcome claims punishment", text)
	}
}
