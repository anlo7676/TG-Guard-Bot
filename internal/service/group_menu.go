package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"tgguard/internal/domain"
	"time"
)

type menuButton = map[string]string

func button(label, data string) menuButton { return menuButton{"text": label, "callback_data": data} }
func (s *Service) groupMenuSend(ctx context.Context, user int64, text string, rows [][]menuButton) error {
	rows, err := s.secureMenu(ctx, user, rows)
	if err != nil {
		return err
	}
	_, err = s.Bot.Send(ctx, user, text, map[string]any{"inline_keyboard": rows}, 0)
	return err
}
func (s *Service) MyGroups(ctx context.Context, m domain.Message, before int64, sections ...string) error {
	section := "home"
	if len(sections) > 0 {
		section = sections[0]
	}
	if !validMenuSection(section) {
		return nil
	}
	if m.From == nil || m.Chat.Type != "private" || m.Chat.ID != m.From.ID {
		return nil
	}
	groups, err := s.Store.MenuGroups(ctx, before)
	if err != nil {
		return err
	}
	rows := [][]menuButton{}
	count := len(groups)
	if count > 10 {
		count = 10
	}
	failed := false
	for _, g := range groups[:count] {
		check, cancel := context.WithTimeout(ctx, 2*time.Second)
		allowed, e := s.Admin(check, g.ID, m.From.ID)
		cancel()
		if e != nil {
			failed = true
			continue
		}
		if !allowed {
			continue
		}
		title := []rune(g.Title)
		if len(title) > 40 {
			title = title[:40]
		}
		rows = append(rows, []menuButton{button(string(title), fmt.Sprintf("gm:%d:%s", g.ID, section))})
	}
	text := "我的群组 · " + menuSectionLabel(section) + "\n\n选择要管理的群组。每次查看和修改都会重新检查你的群管理员权限。"
	if len(rows) == 0 {
		text += "\n\n本页没有可管理的已授权群组。请先联系部署者在网页后台批准接入。可在目标超级群发送 /settings，直接打开该群设置。"
	}
	if failed {
		text += "\n部分群权限暂时无法确认，请稍后刷新。"
	}
	if len(groups) > 10 {
		rows = append(rows, []menuButton{button("继续查找下一页", fmt.Sprintf("menu:groups:%d:%s", groups[9].ID, section))})
	}
	rows = append(rows, []menuButton{button("刷新群列表", fmt.Sprintf("menu:groups:0:%s", section)), button("主菜单", "menu:home")})
	return s.groupMenuSend(ctx, m.Chat.ID, text, rows)
}
func (s *Service) GroupMenuLink(ctx context.Context, m domain.Message) error {
	rows := [][]menuButton{{{"text": "打开本群设置", "url": fmt.Sprintf("https://t.me/%s?start=group_%d", s.Bot.Username, m.Chat.ID)}}}
	return s.groupMenuSend(ctx, m.Chat.ID, "本群管理\n点击按钮，在私聊中设置本群验证、审核和处罚策略。", rows)
}

// Callback data identifies the target explicitly; no shared selected-group state is used.
func (s *Service) GroupMenu(ctx context.Context, m domain.Message, data string) error {
	if m.From == nil || m.Chat.Type != "private" || m.Chat.ID != m.From.ID {
		return nil
	}
	parts := strings.Split(data, ":")
	if len(parts) < 3 || parts[0] != "gm" {
		return nil
	}
	chat, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || chat >= 0 {
		return nil
	}
	allowed, err := s.Admin(ctx, chat, m.From.ID)
	if err != nil {
		return s.text(ctx, m.Chat.ID, "暂时无法确认群权限，请稍后重试。")
	}
	if !allowed {
		return s.text(ctx, m.Chat.ID, "你已不是该群管理员，或没有管理该群的权限。")
	}
	group, err := s.Store.MenuGroup(ctx, chat)
	if err == sql.ErrNoRows {
		return s.text(ctx, m.Chat.ID, "机器人尚未接入该群，或已离开该群。")
	}
	if err != nil {
		return err
	}
	section := parts[2]
	if handled, e := s.groupAction(ctx, m, chat, parts); handled {
		return e
	}
	if section == "set" {
		if len(parts) != 5 {
			return nil
		}
		if err = s.Store.ChangeSettings(ctx, chat, m.From.ID, func(v *domain.Settings) error { return settingPatch(v, parts[3], parts[4]) }); err != nil {
			return err
		}
		section = "settings"
	}
	v, err := s.Store.Settings(ctx, chat)
	if err != nil {
		return err
	}
	prefix := fmt.Sprintf("gm:%d:", chat)
	text := fmt.Sprintf("%s\n群 ID：%d\n\n", group.Title, chat)
	rows := [][]menuButton{}
	switch section {
	case "home":
		text += "请选择本群管理功能。设置只影响这个群。"
		rows = append(rows, []menuButton{button("⚙ 群设置", prefix+"settings"), button("📊 群统计", prefix+"stats")}, []menuButton{button("📏 审核规则", prefix+"rules"), button("💬 关键词回复", prefix+"keywords")}, []menuButton{button("白名单", prefix+"white"), button("黑名单", prefix+"black")}, []menuButton{button("可信用户", prefix+"trusted")})
	case "settings":
		text += "请选择设置分类。保存立即生效，只影响本群。"
		for _, x := range []struct{ key, label string }{{"welcome", "入群欢迎语"}, {"verify", "新人验证"}, {"review", "消息与 AI 审核"}, {"spam", "防刷屏"}, {"punish", "自动处罚"}, {"other", "关键词、日志与语言"}} {
			rows = append(rows, []menuButton{button(x.label, prefix+"category:"+x.key)})
		}
	case "category":
		if len(parts) != 4 {
			return nil
		}
		values := settingValues(v)
		text += "点击项目修改，保存仅对本群生效。"
		for _, f := range domain.SettingFields {
			if f.Section == parts[3] {
				rows = append(rows, []menuButton{button(f.Label+"："+commandExcerpt(displayValue(values[f.Key]), 40), prefix+"field:"+f.Key)})
			}
		}
	case "rules":
		text += fmt.Sprintf("AI 触发风险分：%d；直接处理风险分：%d\n常见广告预设默认删除并警告，通用风险规则累计评分。点击规则可停用或修改动作。", v.AIThreshold, v.DirectThreshold)
		for _, r := range menuRules {
			value := ruleValue(v, r.Key)
			rows = append(rows, []menuButton{button(fmt.Sprintf("%s · %s · %d 分", r.Label, displayValue(value.Enabled), value.Score)+" · "+ruleActionLabel(value.Action), prefix+"rule:"+r.Key)})
		}
		text += fmt.Sprintf("\n自定义广告规则：%d 条。直接动作不依赖 AI 或累计次数；多条命中取封禁、禁言、删除中最强动作。", len(v.AdRules))
		rows = append(rows, []menuButton{button("编辑广告匹配词库", prefix+"adEdit")})
	case "keywords":
		ks, e := s.Store.Keywords(ctx, chat)
		if e != nil {
			return e
		}
		text += fmt.Sprintf("关键词回复：%d 条。点击规则查看、编辑、启停或删除。", len(ks))
		offset := 0
		if len(parts) == 4 {
			offset, _ = strconv.Atoi(parts[3])
		}
		if offset < 0 || offset > len(ks) {
			offset = 0
		}
		for i := offset; i < len(ks) && i < offset+8; i++ {
			k := ks[i]
			word := []rune(k.Keyword)
			if len(word) > 30 {
				word = word[:30]
			}
			rows = append(rows, []menuButton{button(fmt.Sprintf("#%d %s · %s", k.ID, string(word), displayValue(k.Enabled)), prefix+"kw:"+strconv.FormatInt(k.ID, 10))})
		}
		if offset+8 < len(ks) {
			rows = append(rows, []menuButton{button("下一页", fmt.Sprintf("%skeywords:%d", prefix, offset+8))})
		}
		rows = append(rows, []menuButton{button("＋ 新增关键词", prefix+"kwAdd")})
	case "white", "black", "trusted":
		before := int64(9223372036854775807)
		if len(parts) == 4 {
			if n, e := strconv.ParseInt(parts[3], 10, 64); e == nil && n > 0 {
				before = n
			}
		}
		entries, e := s.Store.Rows(ctx, "SELECT id,user_id,username FROM list_entries WHERE chat_id=? AND kind=? AND id<? ORDER BY id DESC LIMIT 9", chat, section, before)
		if e != nil {
			return e
		}
		labels := map[string]string{"white": "白名单", "black": "黑名单", "trusted": "可信用户"}
		text += labels[section] + " · 点击记录可移除。"
		if len(entries) == 0 {
			text += "\n暂无记录。"
		}
		for i, l := range entries {
			if i >= 8 {
				break
			}
			label := fmt.Sprintf("%v @%v", l["user_id"], l["username"])
			rows = append(rows, []menuButton{button(label, fmt.Sprintf("%slistDelete:%v", prefix, l["id"]))})
		}
		if len(entries) > 8 {
			rows = append(rows, []menuButton{button("下一页", fmt.Sprintf("%s%s:%v", prefix, section, entries[7]["id"]))})
		}
		rows = append(rows, []menuButton{button("＋ 添加"+labels[section], prefix+"listAdd:"+section)})
	case "stats":
		summary, e := s.statsSummary(ctx, chat)
		if e != nil {
			return e
		}
		text += summary

	default:
		return nil
	}
	rows = append(rows, []menuButton{button("← 本群管理", prefix+"home"), button("切换群组", "menu:groups")})
	return s.groupMenuSend(ctx, m.Chat.ID, text, rows)
}
func setMenuField(v *domain.Settings, key, value string) error {
	switch key {
	case "verification_enabled", "moderation_enabled", "ai_enabled", "spam_enabled", "keyword_enabled", "auto_delete", "auto_warn", "auto_mute", "auto_ban":
		if value != "true" && value != "false" {
			return fmt.Errorf("invalid menu value")
		}
	case "verification_type":
		if value != "math" && value != "button" {
			return fmt.Errorf("invalid verification type")
		}
		value = strconv.Quote(value)
	case "verification_timeout":
		if value != "180" && value != "300" {
			return fmt.Errorf("invalid timeout")
		}
	default:
		return fmt.Errorf("unsupported menu setting")
	}
	return json.Unmarshal([]byte(fmt.Sprintf("{%q:%s}", key, value)), v)
}
