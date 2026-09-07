package rules

import (
	"encoding/json"
	"strings"
	"testing"
	"tgguard/internal/domain"
)

func TestForwardedCollectionAdvertisements(t *testing.T) {
	samples := []string{
		`{"text":"这是诈骗，请勿参与","forward_origin":{"type":"hidden_user","sender_user_name":"需要群发 联系@SHxxbb"}}`,
		`{"text":"举报这条广告","external_reply":{"origin":{"type":"channel","chat":{"title":"洗钱一天3千有担保群"}}},"quote":{"text":"有收款码的来做，打钱爽快"}}`,
		`{"text":"搞 米","forward_origin":{"type":"hidden_user","sender_user_name":"需要群发 联系@SHxxbb"}}`,
		`{"text":"搞 米","external_reply":{"origin":{"type":"channel","chat":{"id":-88,"title":"洗钱一天3千有担保群"}}},"quote":{"text":"赌博料子没风险6分钟一单 有收款码的来做 好上手 有担保公群打钱爽快"}}`,
		`{"text":"搞 米","forward_origin":{"type":"user","sender_user":{"id":88,"first_name":"需要群发 联系","username":"SHxxbb"}}}`,
		`{"text":"赌博料子没风险6分钟一单 有收款码的来做 好上手 有担保公群打钱爽快"}`,
	}
	for _, raw := range samples {
		var m domain.Message
		if e := json.Unmarshal([]byte(raw), &m); e != nil {
			t.Fatal(e)
		}
		n := Normalize(m)
		r := Evaluate(n, domain.DefaultSettings())
		d := Decide(r, nil, domain.DefaultSettings(), 0, false)
		if d.Action != "warn" || !d.Delete {
			t.Fatal("missed", raw, r, d)
		}
		if d := Decide(r, nil, domain.DefaultSettings(), 3, false); d.Reason != "local_ad_review" {
			t.Fatal(d)
		}
	}
}
func TestForwardContextDoesNotPunishOrdinaryReplies(t *testing.T) {
	for _, raw := range []string{
		`{"text":"大家别信","reply_to_message":{"text":"有收款码的来做，打钱爽快"},"quote":{"text":"有收款码的来做，打钱爽快"}}`,
		`{"text":"搞米","forward_origin":{"type":"channel","chat":{"title":"技术交流群"}}}`,
		`{"text":"洗钱有什么危害？如何保护收款码？"}`,
		`{"text":"今天申请了店铺收款码"}`,
	} {
		var m domain.Message
		if e := json.Unmarshal([]byte(raw), &m); e != nil {
			t.Fatal(e, raw)
		}
		if r := Evaluate(Normalize(m), domain.DefaultSettings()); r.LocalAction != "" {
			t.Fatal("false positive", raw, r)
		}
	}
	m := domain.Message{Text: "官网", Forward: json.RawMessage(`{"type":"hidden_user","sender_user_name":"需要群发 联系@SHxxbb"}`)}
	if m.Body() != "官网" || !strings.Contains(m.ModerationText(), "[转发来源]") {
		t.Fatal("keyword body changed or evidence lost")
	}
}

func TestContextBoundariesAndEvidenceDeduplication(t *testing.T) {
	s := domain.DefaultSettings()
	m := domain.Message{Text: "部署问题请联系管理员", Forward: json.RawMessage(`{"type":"hidden_user","sender_user_name":"群发工具开源项目"}`)}
	if r := Evaluate(Normalize(m), s); r.Score != 0 || r.LocalAction != "" {
		t.Fatal("cross-field phrase", r)
	}
	m.Text = "举报"
	m.Forward = json.RawMessage(`{"type":"hidden_user","sender_user_name":"需要群发 联系@SHxxbb"}`)
	n := Normalize(m)
	r := Evaluate(n, s)
	if r.Score != 100 || r.LocalAction != "delete" || !strings.Contains(m.ModerationText(), "需要群发") {
		t.Fatal("report prefix lost evidence", r)
	}
	if !strings.Contains(r.Matches[0].Reason, "[转发来源]") {
		t.Fatal("missing source label", r)
	}
	m.Text = "兼职"
	m.Forward = json.RawMessage(`{"type":"hidden_user","sender_user_name":"兼职"}`)
	n = Normalize(m)
	n.IsNew = true
	r = Evaluate(n, s)
	if r.Score != 35 {
		t.Fatal("same rule counted twice", r)
	}
	s.AdRules = []domain.AdRule{{Enabled: true, Mode: "regex", Pattern: "联系.*群发", Action: "ban"}}
	m.Text = "联系管理员"
	m.Forward = json.RawMessage(`{"type":"hidden_user","sender_user_name":"群发工具"}`)
	if r = Evaluate(Normalize(m), s); r.LocalAction != "" {
		t.Fatal("custom rule crossed fields", r)
	}
}
