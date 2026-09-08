package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"tgguard/internal/domain"
)

func (s *Service) groupAction(ctx context.Context, m domain.Message, chat int64, p []string) (bool, error) {
	section := p[2]
	prefix := fmt.Sprintf("gm:%d:", chat)
	switch section {
	case "field":
		if len(p) != 4 {
			return true, nil
		}
		f, ok := domain.FindSetting(p[3])
		if !ok {
			return true, nil
		}
		v, e := s.Store.Settings(ctx, chat)
		if e != nil {
			return true, e
		}
		if f.Kind == "text" {
			return true, s.promptGroup(ctx, m, chat, "setting", f.Key, "设置欢迎语，最多 1000 字。支持 {name} 成员名称、{username} 用户名、{user_id} 用户 ID、{group} 群名、{timeout} 验证秒数。开启验证时，欢迎语在验证通过后发送，并在 5 分钟内自动删除。\n当前："+v.WelcomeText)
		}
		if f.Kind == "number" {
			return true, s.promptGroup(ctx, m, chat, "setting", f.Key, "设置"+f.Label+"，当前："+displayValue(settingValues(v)[f.Key]))
		}
		choices := f.Choices
		if f.Kind == "bool" {
			choices = []string{"true", "false"}
		}
		rows := [][]menuButton{}
		for _, value := range choices {
			label := displayValue(value)
			if value == "true" {
				label = "开启"
			}
			if value == "false" {
				label = "关闭"
			}
			rows = append(rows, []menuButton{button(label, prefix+"set:"+f.Key+":"+value)})
		}
		rows = append(rows, []menuButton{button("取消", prefix+"settings")})
		return true, s.groupMenuSend(ctx, m.Chat.ID, fmt.Sprintf("群 %d\n%s\n当前：%s\n请选择新的值。", chat, f.Label, displayValue(settingValues(v)[f.Key])), rows)
	case "adEdit":
		v, e := s.Store.Settings(ctx, chat)
		if e != nil {
			return true, e
		}
		current := domain.FormatAdRules(v.AdRules)
		if len([]rune(current)) > 2200 {
			return true, s.text(ctx, m.Chat.ID, "当前广告词库较长，请到网页后台「群管理 → 配置群组」编辑，避免私聊输入长度限制。")
		}
		if current == "" {
			current = "（暂无）"
		}
		return true, s.promptGroup(ctx, m, chat, "adRules", "", "当前广告词库：\n"+current+"\n\n编辑本群广告词库（替换全部自定义规则）：每行填写 匹配方式|动作|匹配内容。\n匹配方式：包含、精确、正则；动作：删除、禁言、封禁。\n例：包含|禁言|稳赚包赔\n停用例：停用包含|删除|广告词\n输入 清空 删除全部自定义规则。欢迎语在群设置的「入群欢迎语」中设置。")
	case "ruleAction":
		if len(p) != 5 || !knownRule(p[3]) {
			return true, nil
		}
		action := p[4]
		if action == "score" {
			action = ""
		}
		if !domain.ValidRuleAction(action) {
			return true, nil
		}
		e := s.Store.ChangeSettings(ctx, chat, m.From.ID, func(v *domain.Settings) error {
			r := ruleValue(*v, p[3])
			r.Action = action
			v.Rules[p[3]] = r
			return nil
		})
		if e != nil {
			return true, e
		}
		return true, s.GroupMenu(ctx, m, prefix+"rules")
	case "rule":
		if len(p) != 4 || !knownRule(p[3]) {
			return true, nil
		}
		v, e := s.Store.Settings(ctx, chat)
		if e != nil {
			return true, e
		}
		r := ruleValue(v, p[3])
		next := strconv.FormatBool(!r.Enabled)
		return true, s.groupMenuSend(ctx, m.Chat.ID, fmt.Sprintf("群 %d\n规则 %s：%s，风险加分 %d\n命中动作：%s", chat, p[3], displayValue(r.Enabled), r.Score, ruleActionLabel(r.Action)), [][]menuButton{{button("切换启用状态", prefix+"ruleSet:"+p[3]+":"+next), button("修改风险分", prefix+"ruleScore:"+p[3])}, {button("仅累计评分", prefix+"ruleAction:"+p[3]+":score"), button("删除并警告", prefix+"ruleAction:"+p[3]+":delete")}, {button("删除并禁言", prefix+"ruleAction:"+p[3]+":mute"), button("封禁并清理发言", prefix+"ruleAction:"+p[3]+":ban")}, {button("返回规则", prefix+"rules")}})
	case "ruleSet":
		if len(p) != 5 || !knownRule(p[3]) || (p[4] != "true" && p[4] != "false") {
			return true, nil
		}
		e := s.Store.ChangeSettings(ctx, chat, m.From.ID, func(v *domain.Settings) error {
			r := ruleValue(*v, p[3])
			r.Enabled = p[4] == "true"
			v.Rules[p[3]] = r
			return nil
		})
		if e != nil {
			return true, e
		}
		return true, s.GroupMenu(ctx, m, prefix+"rules")
	case "ruleScore":
		if len(p) != 4 || !knownRule(p[3]) {
			return true, nil
		}
		return true, s.promptGroup(ctx, m, chat, "rule", p[3], "输入规则 "+p[3]+" 的风险加分（0–100 整数）。")
	case "kwAdd":
		return true, s.promptGroup(ctx, m, chat, "keywordWords", "new", "第 1/2 步：填写关键词。\n例如：安卓\n多个同义词可用 | 分隔，例如：官网|网站|网址。\n下一步再填写回复内容。")
	case "kw", "kwEdit", "kwToggle", "kwDelete", "kwDeleteYes":
		if len(p) != 4 {
			return true, nil
		}
		id, e := strconv.ParseInt(p[3], 10, 64)
		if e != nil || id <= 0 {
			return true, nil
		}
		ks, e := s.Store.Keywords(ctx, chat)
		if e != nil {
			return true, e
		}
		var k domain.Keyword
		for _, v := range ks {
			if v.ID == id {
				k = v
				break
			}
		}
		if k.ID == 0 {
			return true, s.text(ctx, m.Chat.ID, "关键词不存在或已删除。")
		}
		target := prefix + "kw:" + p[3]
		switch section {
		case "kwEdit":
			return true, s.promptGroup(ctx, m, chat, "keywordWords", p[3], "第 1/2 步：填写新的关键词或正则。\n当前："+k.Keyword+"\n保留原有匹配方式，下一步填写回复内容。", &k)
		case "kwToggle":
			// This action opens explicit choices, preventing retries from toggling twice.
			return true, s.groupMenuSend(ctx, m.Chat.ID, "选择关键词状态", [][]menuButton{{button("开启", prefix+"kwState:"+p[3]+":true"), button("关闭", prefix+"kwState:"+p[3]+":false")}, {button("返回", target)}})
		case "kwDelete":
			return true, s.groupMenuSend(ctx, m.Chat.ID, fmt.Sprintf("确认删除本群关键词 #%d「%s」？", id, k.Keyword), [][]menuButton{{button("确认删除", prefix+"kwDeleteYes:"+p[3]), button("取消", target)}})
		case "kwDeleteYes":
			if e = s.Store.DeleteKeyword(ctx, chat, id, m.From.ID); e != nil {
				return true, e
			}
			return true, s.GroupMenu(ctx, m, prefix+"keywords")
		}
		content := []rune(k.Content)
		if len(content) > 1600 {
			content = content[:1600]
		}
		return true, s.groupMenuSend(ctx, m.Chat.ID, fmt.Sprintf("群 %d · 关键词 #%d\n%s\n匹配：%s，优先级：%d，%s\n\n%s", chat, id, k.Keyword, k.MatchType, k.Priority, displayValue(k.Enabled), string(content)), [][]menuButton{{button("编辑内容", prefix+"kwEdit:"+p[3]), button("启用／停用", prefix+"kwToggle:"+p[3])}, {button("删除", prefix+"kwDelete:"+p[3]), button("返回关键词", prefix+"keywords")}})
	case "kwState":
		if len(p) != 5 || (p[4] != "true" && p[4] != "false") {
			return true, nil
		}
		id, e := strconv.ParseInt(p[3], 10, 64)
		if e != nil {
			return true, nil
		}
		ks, e := s.Store.Keywords(ctx, chat)
		if e != nil {
			return true, e
		}
		for _, k := range ks {
			if k.ID == id {
				e = s.Store.ChangeKeyword(ctx, chat, k.ID, m.From.ID, func(latest *domain.Keyword) error { latest.Enabled = p[4] == "true"; return nil })
				if e != nil {
					return true, e
				}
				return true, s.GroupMenu(ctx, m, prefix+"kw:"+p[3])
			}
		}
		return true, s.text(ctx, m.Chat.ID, "规则不存在。")
	case "listAdd":
		if len(p) != 4 || !strings.Contains("|white|black|trusted|", "|"+p[3]+"|") {
			return true, nil
		}
		return true, s.promptGroup(ctx, m, chat, "list", p[3], "添加本群名单：用户数字 ID，空格后可填写原因；不使用可转让的用户名授权。\n黑名单将在该用户后续入群或发言时生效。")
	case "listDelete", "listDeleteYes":
		if len(p) != 4 {
			return true, nil
		}
		id, e := strconv.ParseInt(p[3], 10, 64)
		if e != nil || id <= 0 {
			return true, nil
		}
		l, e := s.Store.MenuListEntry(ctx, chat, id)
		if e != nil {
			return true, s.text(ctx, m.Chat.ID, "名单已不存在。")
		}
		if section == "listDelete" {
			return true, s.groupMenuSend(ctx, m.Chat.ID, fmt.Sprintf("确认移除本群名单：%d @%s（%s）？", l.UserID, l.Username, l.Kind), [][]menuButton{{button("确认移除", prefix+"listDeleteYes:"+p[3]), button("取消", prefix+l.Kind)}})
		}
		if e = s.Store.SaveList(ctx, l, m.From.ID, true); e != nil {
			return true, e
		}
		return true, s.GroupMenu(ctx, m, prefix+l.Kind)
	}
	return false, nil
}
