package domain

import "testing"

func TestSettingsPatchRejectsInvalidAndPreservesFields(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{"ai_enabled":null}`, `{"unknown":true}`, `{"ai_warn_confidence":2}`, `{"verification_type":"web"}`} {
		v := DefaultSettings()
		if e := ApplySettingsPatch(&v, []byte(raw)); e == nil {
			t.Fatal("accepted", raw)
		}
	}
	v := DefaultSettings()
	v.RateLimit = 23
	if e := ApplySettingsPatch(&v, []byte(`{"ai_enabled":true}`)); e != nil || !v.AIEnabled || v.RateLimit != 23 {
		t.Fatal(v, e)
	}
}
func TestKeywordButtonsAndRandomValidation(t *testing.T) {
	k := Keyword{Keyword: "官网", MatchType: "contains", ReplyType: "text", Content: "内容", Buttons: [][]LinkButton{{{Text: "官网", URL: "https://example.com"}}}}
	if e := k.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, url := range []string{"javascript:alert(1)", "file:///C:/private", "https://user:password@example.com", ""} {
		k.Buttons[0][0].URL = url
		if e := k.Validate(); e == nil {
			t.Fatal("accepted", url)
		}
	}
	k.Buttons = nil
	k.ReplyType = "random"
	k.Content = "一条\n\n另一条"
	if e := k.Validate(); e == nil {
		t.Fatal("empty random message accepted")
	}
}
