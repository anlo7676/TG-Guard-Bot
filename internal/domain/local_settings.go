package domain

import (
	"fmt"
	"regexp"
	"strings"
)

type AdRule struct {
	Pattern string `json:"pattern"`
	Mode    string `json:"mode"`
	Action  string `json:"action"`
	Enabled bool   `json:"enabled"`
}

func ValidRuleAction(a string) bool { return one(a, "", "delete", "mute", "ban") }
func (s Settings) validateLocalSettings() error {
	if len([]rune(s.WelcomeText)) > 1000 {
		return fmt.Errorf("欢迎语最多 1000 字")
	}
	if s.WelcomeEnabled && strings.TrimSpace(s.WelcomeText) == "" {
		return fmt.Errorf("开启欢迎语时，内容不能为空")
	}
	for _, r := range s.Rules {
		if !ValidRuleAction(r.Action) {
			return fmt.Errorf("规则动作只能选择评分、删除、禁言或封禁")
		}
	}
	if len(s.AdRules) > 50 {
		return fmt.Errorf("每群最多 50 条广告匹配规则")
	}
	for i, r := range s.AdRules {
		if strings.TrimSpace(r.Pattern) == "" || strings.ContainsAny(r.Pattern, "\r\n") || len([]rune(r.Pattern)) > 500 || !one(r.Mode, "contains", "exact", "regex") || r.Action == "" || !ValidRuleAction(r.Action) {
			return fmt.Errorf("第 %d 条广告规则格式无效", i+1)
		}
		if r.Mode == "regex" {
			if _, e := regexp.Compile("(?i)" + r.Pattern); e != nil {
				return fmt.Errorf("第 %d 条正则表达式无效：%v", i+1, e)
			}
		}
	}
	return nil
}

// Human-readable full replacement format; SplitN preserves regex alternation pipes.
func ParseAdRules(text string) ([]AdRule, error) {
	out := []AdRule{}
	if strings.TrimSpace(text) == "清空" || strings.TrimSpace(text) == "" {
		return out, nil
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		p := strings.SplitN(line, "|", 3)
		if len(p) != 3 {
			return nil, fmt.Errorf("格式：包含|删除|广告词，每行一条")
		}
		mode := strings.TrimSpace(p[0])
		enabled := !strings.HasPrefix(mode, "停用")
		mode = strings.TrimPrefix(mode, "停用")
		mode = map[string]string{"包含": "contains", "精确": "exact", "正则": "regex"}[mode]
		action := map[string]string{"删除": "delete", "禁言": "mute", "封禁": "ban"}[strings.TrimSpace(p[1])]
		out = append(out, AdRule{Pattern: strings.TrimSpace(p[2]), Mode: mode, Action: action, Enabled: enabled})
	}
	v := DefaultSettings()
	v.AdRules = out
	if e := v.Validate(); e != nil {
		return nil, e
	}
	return out, nil
}
func FormatAdRules(rules []AdRule) string {
	lines := []string{}
	for _, r := range rules {
		mode := map[string]string{"contains": "包含", "exact": "精确", "regex": "正则"}[r.Mode]
		if !r.Enabled {
			mode = "停用" + mode
		}
		lines = append(lines, mode+"|"+map[string]string{"delete": "删除", "mute": "禁言", "ban": "封禁"}[r.Action]+"|"+r.Pattern)
	}
	return strings.Join(lines, "\n")
}
