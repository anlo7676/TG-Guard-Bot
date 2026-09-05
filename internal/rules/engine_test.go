package rules

import (
	"strings"
	"testing"

	"tgguard/internal/domain"
)

func TestNormalizeHiddenLinksAndUnicode(t *testing.T) {
	m := domain.Message{ID: 1, Chat: domain.Chat{ID: -100}, From: &domain.User{ID: 42}, Text: "😀官网", Entities: []domain.Entity{{Type: "text_link", Offset: 2, Length: 2, URL: "https://t.me/spam"}}}
	n := Normalize(m)
	if len(n.URLs) != 1 || n.URLs[0] != "https://t.me/spam" {
		t.Fatalf("hidden link lost: %+v", n)
	}
	r := Evaluate(n, domain.DefaultSettings())
	if r.Score != 60 {
		t.Fatalf("risk=%d", r.Score)
	}
	if Clean(" Ｔ．ＭＥ/ab\u200bcd  ") != "t.me/abcd" {
		t.Fatal("normalization failed")
	}
}
func TestUTF16URLBounds(t *testing.T) {
	m := domain.Message{Text: "😀https://example.com", Entities: []domain.Entity{{Type: "url", Offset: 2, Length: 19}, {Type: "url", Offset: 999, Length: 100}, {Type: "url", Offset: -1, Length: 4}}}
	n := Normalize(m)
	if len(n.URLs) != 1 || n.URLs[0] != "https://example.com" {
		t.Fatalf("unexpected urls %v", n.URLs)
	}
}
func TestRiskAndDisabledRules(t *testing.T) {
	s := domain.DefaultSettings()
	n := domain.Normalized{Text: "稳赚 带单 联系 @someone https://t.me/spam", URLs: []string{"https://t.me/spam"}, Mentions: []string{"@someone"}}
	r := Evaluate(n, s)
	if r.Score != 100 {
		t.Fatalf("score=%d", r.Score)
	}
	for _, m := range r.Matches {
		s.Rules[m.Rule] = domain.RuleSetting{Enabled: false}
	}
	if got := Evaluate(n, s); got.Score != 0 {
		t.Fatalf("disabled rules scored %+v", got)
	}
}
func TestNormalTechnicalText(t *testing.T) {
	n := Normalize(domain.Message{Text: "MySQL 的索引应该如何设计？USDT 使用的区块链有什么区别？"})
	if r := Evaluate(n, domain.DefaultSettings()); r.Score != 0 {
		t.Fatalf("ordinary discussion scored %+v", r)
	}
}
func TestDecisionSafety(t *testing.T) {
	s := domain.DefaultSettings()
	cases := []struct {
		name      string
		r         domain.Risk
		a         *domain.AIResult
		count     int
		protected bool
		action    string
		deleted   bool
	}{
		{"safe", domain.Risk{Score: 0}, nil, 0, false, "allow", false},
		{"ai outage ambiguous", domain.Risk{Score: 70}, nil, 0, false, "allow", false},
		{"explicit rules", domain.Risk{Score: 90}, nil, 0, false, "warn", true},
		{"second violation", domain.Risk{Score: 90}, nil, 1, false, "mute", true},
		{"ban disabled", domain.Risk{Score: 90}, nil, 99, false, "mute", true},
		{"admin protected", domain.Risk{Score: 100, Spam: true}, nil, 9, true, "shadow_log", false},
		{"AI advice cannot ban", domain.Risk{Score: 50}, &domain.AIResult{IsAd: true, Confidence: .9, RecommendedAction: "ban"}, 0, false, "warn", true},
		{"AI low confidence", domain.Risk{Score: 50}, &domain.AIResult{IsAd: true, Confidence: .59, RecommendedAction: "ban"}, 0, false, "allow", false},
		{"AI negative", domain.Risk{Score: 60}, &domain.AIResult{IsAd: false, Confidence: 1}, 0, false, "allow", false},
		{"AI cannot pardon explicit spam", domain.Risk{Score: 100, Spam: true}, &domain.AIResult{IsAd: false, Confidence: 1}, 0, false, "warn", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Decide(tc.r, tc.a, s, tc.count, tc.protected)
			if d.Action != tc.action || d.Delete != tc.deleted {
				t.Fatalf("got %+v", d)
			}
		})
	}
	s.AutoBan = true
	if d := Decide(domain.Risk{Score: 90}, nil, s, 2, false); d.Action != "ban" {
		t.Fatalf("enabled cumulative ban: %+v", d)
	}
}
func TestKeywordModes(t *testing.T) {
	for _, tc := range []struct {
		mode, key, text string
		want            bool
	}{{"exact", "官网", " 官网 ", true}, {"contains", "官网", "请问官网是什么", true}, {"starts_with", "help", "HELP me", true}, {"ends_with", "谢谢", "好的谢谢", true}, {"regex", `^go\d+$`, "go123", true}, {"regex", "[", "text", false}, {"exact", "官网", "官网是什么", false}} {
		k := domain.Keyword{Enabled: true, MatchType: tc.mode, Keyword: tc.key}
		if got := KeywordMatch(k, tc.text); got != tc.want {
			t.Errorf("%+v got %v", tc, got)
		}
	}
	k := domain.Keyword{Enabled: true, MatchType: "regex", Keyword: `(a+)+$`}
	KeywordMatch(k, strings.Repeat("a", 10000)+"!")
}
func FuzzNormalize(f *testing.F) {
	f.Add("😀推广", 2, 5)
	f.Add("hello", -1, 999)
	f.Fuzz(func(t *testing.T, text string, offset, length int) {
		n := Normalize(domain.Message{Text: text, Entities: []domain.Entity{{Type: "url", Offset: offset, Length: length}}})
		r := Evaluate(n, domain.DefaultSettings())
		if r.Score < 0 || r.Score > 100 {
			t.Fatal("score out of range")
		}
	})
}
func TestKeywordRegexUsesMessageBody(t *testing.T) {
	k := domain.Keyword{Enabled: true, MatchType: "regex", Keyword: `^hello$`}
	for _, m := range []domain.Message{{Text: "hello"}, {Caption: "hello"}} {
		if !KeywordMatch(k, m.Body()) {
			t.Fatalf("anchored regex did not match %q", m.Body())
		}
	}
}
