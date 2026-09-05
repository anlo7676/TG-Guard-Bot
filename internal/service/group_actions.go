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
		return true, s.groupMenuSend(ctx, m.Chat.ID, fmt.Sprintf("群 %d\n规则 %s：%s，风险加分 %d", chat, p[3], displayValue(r.Enabled), r.Score), [][]menuButton{{button("切换启用状态", prefix+"ruleSet:"+p[3]+":"+next), button("修改风险分", prefix+"ruleScore:"+p[3])}, {button("返回规则", prefix+"rules")}})
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
		return true, s.promptGroup(ctx, m, chat, "keyword", "new", "创建关键词：关键词 | 回复内容\n例如：官网 | https://example.com")
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
			return true, s.promptGroup(ctx, m, chat, "keyword", p[3], "修改关键词和回复：关键词 | 回复内容\n会保留匹配方式、优先级和其他选项。")
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
				k.Enabled = p[4] == "true"
				_, e = s.Store.SaveKeyword(ctx, k, m.From.ID)
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
		return true, s.promptGroup(ctx, m, chat, "list", p[3], "添加本群名单：用户数字 ID 或 @用户名，空格后可填写原因。\n黑名单将在该用户后续入群或发言时生效。")
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
