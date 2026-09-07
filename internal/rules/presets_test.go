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
	if count != 30 {
		t.Fatalf("expected 30 explicit ad categories, got %d", count)
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

func TestScreenshotAdsAndNormalCounterexamples(t *testing.T) {
	s := domain.DefaultSettings()
	for _, text := range []string{"足球红单推荐交流群.加入免费领红包 @losusnh9071bot", "拍店铺招牌🛍️ 8 o/张", "不想上班的来，上个j8的班 来帮我干活，一个月包提奥迪A7 看我兼届", "有绿色项目不做？来和我一起做黑U，交易所的来 一天五个达不溜轻轻松松"} {
		if d := Decide(Evaluate(Normalize(domain.Message{Text: text}), s), nil, s, 0, false); d.Action != "delete" {
			t.Errorf("missed %q: %+v", text, d)
		}
	}
	for _, text := range []string{"你好哈喽", "足球比赛今晚几点开始？", "这个足球交流群只讨论战术", "项目发红包庆祝上线", "拍店铺招牌留作纪念", "公司招聘 Go 工程师，月薪两万", "我攒了三年钱终于提奥迪A7", "黑U是什么意思？如何防范风险？", "警方提醒不要参与黑U项目", "跟我一起做 Go 开源项目"} {
		if r := Evaluate(Normalize(domain.Message{Text: text}), s); r.LocalAction != "" {
			t.Errorf("false positive %q: %+v", text, r)
		}
	}
}
