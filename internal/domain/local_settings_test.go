package domain

import (
	"strings"
	"testing"
)

func TestLocalRuleValidationAndRoundTrip(t *testing.T) {
	text := "包含|删除|广告词\n停用精确|禁言|广告全文\n正则|封禁|加微.*(收益|赚钱)"
	rules, e := ParseAdRules(text)
	if e != nil {
		t.Fatal(e)
	}
	if len(rules) != 3 || rules[1].Enabled || FormatAdRules(rules) != text {
		t.Fatal(rules)
	}
	for _, invalid := range []string{"包含|未知|词", "正则|删除|[", "包含|删除|", "bad", "包含|删除|" + strings.Repeat("长", 501)} {
		if _, e := ParseAdRules(invalid); e == nil {
			t.Error("invalid rule accepted", invalid)
		}
	}
	v := DefaultSettings()
	if e := ApplySettingsPatch(&v, []byte(`{"welcome_text":"欢迎 {name}","rules":{"advertising":{"enabled":true,"score":25,"action":"mute"}},"ad_rules":[]}`)); e != nil {
		t.Fatal(e)
	}
	if v.Rules["advertising"].Action != "mute" || !v.VerificationEnabled {
		t.Fatal("settings patch lost fields")
	}
	if e := ApplySettingsPatch(&v, []byte(`{"rules":{"advertising":{"action":"bogus"}}}`)); e == nil {
		t.Fatal("invalid action accepted")
	}
}
