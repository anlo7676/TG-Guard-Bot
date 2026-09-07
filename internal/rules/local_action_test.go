package rules

import (
	"testing"
	"tgguard/internal/domain"
)

func TestDirectAdvertisingActions(t *testing.T) {
	for _, action := range []string{"delete", "mute", "ban"} {
		t.Run(action, func(t *testing.T) {
			s := domain.DefaultSettings()
			s.AutoDelete = false
			s.AutoMute = false
			s.AutoBan = false
			s.AutoWarn = false
			s.Rules["advertising"] = domain.RuleSetting{Enabled: true, Score: 0, Action: action}
			r := Evaluate(Normalize(domain.Message{Text: "接单"}), s)
			d := Decide(r, nil, s, 0, false)
			want := action
			if want == "delete" {
				want = "warn"
			}
			if d.Action != want || !d.Delete || r.Score != 100 {
				t.Fatal("direct rule not enforced", r, d)
			}
			if action == "mute" && d.Duration != s.MuteSeconds {
				t.Fatal("wrong mute duration")
			}
			if p := Decide(r, nil, s, 0, true); p.Action != "shadow_log" || p.Delete {
				t.Fatal("protected member punished", p)
			}
			s.Rules["advertising"] = domain.RuleSetting{Enabled: false, Score: 100, Action: action}
			if r := Evaluate(Normalize(domain.Message{Text: "接单"}), s); r.LocalAction != "" {
				t.Fatal("disabled rule enforced")
			}
		})
	}
}
func TestCustomAdvertisingRulesAndStrongestAction(t *testing.T) {
	s := domain.DefaultSettings()
	s.AdRules = []domain.AdRule{{Pattern: "ＡＤ Word", Mode: "contains", Enabled: true, Action: "delete"}, {Pattern: "ad word", Mode: "exact", Enabled: true, Action: "mute"}, {Pattern: "ad.*(word|cash)", Mode: "regex", Enabled: true, Action: "ban"}}
	n := Normalize(domain.Message{Caption: "AD\u200b Word"})
	r := Evaluate(n, s)
	if len(r.Matches) != 3 || r.LocalAction != "ban" {
		t.Fatal(r)
	}
	s.AdRules[2].Enabled = false
	if r := Evaluate(n, s); r.LocalAction != "mute" {
		t.Fatal(r)
	}
	if r := Evaluate(Normalize(domain.Message{Text: "普通技术交流"}), s); r.LocalAction != "" {
		t.Fatal("normal content matched", r)
	}
}

func TestLocalAdReviewThreshold(t *testing.T) {
	s := domain.DefaultSettings()
	s.AutoBan = true
	s.BanAfter = 1
	r := domain.Risk{Score: 100, LocalAction: "delete"}
	for _, n := range []int{0, 1, 2, 3, 4, 20} {
		d := Decide(r, nil, s, n, false)
		if n < 3 {
			if d.Action != "warn" || !d.Delete {
				t.Fatal(n, d)
			}
		} else if d.Action != "mute" || d.Duration != 0 || d.Reason != "local_ad_review" {
			t.Fatal(n, d)
		}
	}
	if d := Decide(r, nil, s, 10, true); d.Action != "shadow_log" || d.Delete {
		t.Fatal(d)
	}
}
