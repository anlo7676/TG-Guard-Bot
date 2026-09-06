package rules

import (
	"testing"
	"tgguard/internal/domain"
)

func TestEveryDefaultAdPresetDetectsItsCategory(t *testing.T) {
	s := domain.DefaultSettings()
	seen := map[string]bool{}
	count := 0
	for _, preset := range domain.BuiltinRules {
		if seen[preset.Key] {
			t.Fatal("duplicate key", preset.Key)
		}
		seen[preset.Key] = true
		if preset.Pattern == "" {
			continue
		}
		count++
		t.Run(preset.Key, func(t *testing.T) {
			r := Evaluate(Normalize(domain.Message{Text: preset.Example}), s)
			found := false
			for _, m := range r.Matches {
				if m.Rule == preset.Key {
					found = true
				}
			}
			if !found {
				t.Fatal("category example not detected", preset.Example, r)
			}
			d := Decide(r, nil, s, 0, false)
			if d.Action != "delete" || !d.Delete {
				t.Fatal("default must delete locally without AI", d)
			}
			if d := Decide(r, nil, s, 0, true); d.Action != "shadow_log" || d.Delete {
				t.Fatal("protected user not respected", d)
			}
		})
	}
	if count != 25 {
		t.Fatalf("expected 25 explicit ad categories, got %d", count)
	}
}
func TestDefaultPresetsKeepNormalTopicsOutOfDirectPunishment(t *testing.T) {
	for _, text := range []string{"家电维修上门服务，请联系预约", "我在学习代理模式设计", "周末兼职开发 Go 项目", "今天买的东西有优惠", "请问机场节点连接失败怎么排查", "公司招聘 Go 开发工程师", "讨论贷款利率和征信政策", "保护个人信息，不要泄露验证码", "介绍 USDT 和区块链基础知识", "开源软件如何管理永久许可证", "这是我们的官网 https://example.com"} {
		r := Evaluate(Normalize(domain.Message{Text: text}), domain.DefaultSettings())
		if r.LocalAction != "" {
			t.Errorf("normal topic directly punished: %s %+v", text, r)
		}
	}
}
func TestPresetOverridesPreserveDisableAndCustomAction(t *testing.T) {
	s := domain.DefaultSettings()
	s.Rules["ad_tasks"] = domain.RuleSetting{Enabled: false, Score: 60, Action: "ban"}
	n := Normalize(domain.Message{Text: "刷单返佣，佣金日结"})
	if r := Evaluate(n, s); r.LocalAction != "" {
		t.Fatal("disabled preset enforced", r)
	}
	s.Rules["ad_tasks"] = domain.RuleSetting{Enabled: true, Score: 60, Action: "mute"}
	r := Evaluate(n, s)
	if d := Decide(r, nil, s, 0, false); d.Action != "mute" || !d.Delete {
		t.Fatal(d)
	}
	s.Rules["ad_tasks"] = domain.RuleSetting{Enabled: true, Score: 0}
	if r := Evaluate(n, s); r.LocalAction != "" {
		t.Fatal("score-only override lost", r)
	}
	s.AdRules = []domain.AdRule{{Enabled: true, Mode: "contains", Pattern: "我的自定义广告词", Action: "ban"}}
	if r := Evaluate(Normalize(domain.Message{Text: "我的自定义广告词"}), s); r.LocalAction != "ban" {
		t.Fatal("custom rules lost", r)
	}
	if e := s.Validate(); e != nil {
		t.Fatal("new preset overrides rejected", e)
	}
}

func TestPresetDetectsHiddenGroupLink(t *testing.T) {
	r := Evaluate(domain.Normalized{Text: "加入福利群", URLs: []string{"https://t.me/freebonus"}}, domain.DefaultSettings())
	if r.LocalAction != "delete" {
		t.Fatal("hidden group link missed", r)
	}
}
