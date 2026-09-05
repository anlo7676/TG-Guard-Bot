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
	_, err := s.Bot.Send(ctx, user, text, map[string]any{"inline_keyboard": rows}, 0)
	return err
}
func (s *Service) MyGroups(ctx context.Context, m domain.Message, before int64) error {
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
		rows = append(rows, []menuButton{button(string(title), fmt.Sprintf("gm:%d:home", g.ID))})
	}
	text := "我的群组\n\n选择要管理的群组。每次查看和修改都会重新检查你的群管理员权限。"
	if len(rows) == 0 {
		text += "\n\n本页没有可管理的群组。可在目标超级群发送 /settings，直接打开该群设置。"
	}
	if failed {
		text += "\n部分群权限暂时无法确认，请稍后刷新。"
	}
	if len(groups) > 10 {
		rows = append(rows, []menuButton{button("继续查找下一页", fmt.Sprintf("menu:groups:%d", groups[9].ID))})
	}
	rows = append(rows, []menuButton{button("刷新群列表", "menu:groups"), button("主菜单", "menu:home")})
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
	if section == "set" {
		if len(parts) != 5 {
			return nil
		}
		if err = s.Store.ChangeSettings(ctx, chat, m.From.ID, func(v *domain.Settings) error { return setMenuField(v, parts[3], parts[4]) }); err != nil {
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
		rows = append(rows, []menuButton{button("⚙ 群设置", prefix+"settings"), button("📊 群统计", prefix+"stats")}, []menuButton{button("📏 审核规则", prefix+"rules"), button("💬 关键词回复", prefix+"keywords")}, []menuButton{button("白名单", prefix+"white"), button("黑名单", prefix+"black")})
	case "settings":
		text += "点击开关即可保存本群设置；按钮文字显示当前状态。\nAI 审核还要求部署者已配置全局模型接口。\n更多参数可在本群使用 /settings JSON 设置，或在 Web 后台群管理中修改。"
		for _, f := range []struct {
			key, label string
			value      bool
		}{{"verification_enabled", "新人验证", v.VerificationEnabled}, {"moderation_enabled", "内容审核", v.ModerationEnabled}, {"ai_enabled", "AI 辅助审核", v.AIEnabled}, {"spam_enabled", "防刷屏", v.SpamEnabled}, {"keyword_enabled", "关键词回复", v.KeywordEnabled}, {"auto_delete", "自动删除", v.AutoDelete}, {"auto_warn", "自动警告", v.AutoWarn}, {"auto_mute", "自动禁言", v.AutoMute}, {"auto_ban", "自动封禁", v.AutoBan}} {
			status, next := "关", "true"
			if f.value {
				status, next = "开", "false"
			}
			rows = append(rows, []menuButton{button(f.label+"："+status, prefix+"set:"+f.key+":"+next)})
		}
		text += fmt.Sprintf("\n\n验证：%s，%d 秒，失败 %s\n禁言时长：%d 秒", v.VerificationType, v.VerificationTimeout, v.VerificationFailAction, v.MuteSeconds)
		rows = append(rows, []menuButton{button("数学题验证", prefix+"set:verification_type:math"), button("按钮验证", prefix+"set:verification_type:button")}, []menuButton{button("验证 3 分钟", prefix+"set:verification_timeout:180"), button("验证 5 分钟", prefix+"set:verification_timeout:300")})
	case "rules":
		text += "本群规则覆盖配置：\n" + prettyMenu(v.Rules) + fmt.Sprintf("\nAI 门槛：%d；直接处理门槛：%d\n\n在本群发送以下命令修改 URL 规则（只影响本群）：\n/settings {\"rules\":{\"url\":{\"enabled\":true,\"score\":20}}}", v.AIThreshold, v.DirectThreshold)
	case "keywords":
		ks, e := s.Store.Keywords(ctx, chat)
		if e != nil {
			return e
		}
		text += fmt.Sprintf("关键词回复：%d 条\n", len(ks))
		for i, k := range ks {
			if i >= 10 {
				break
			}
			word := []rune(k.Keyword)
			if len(word) > 60 {
				word = word[:60]
			}
			text += fmt.Sprintf("#%d · %s · %s\n", k.ID, k.MatchType, string(word))
		}
		text += "\n在本群发送：\n/keywords add contains 官网 | https://example.com\n/keywords del 规则ID\n\n完整管理可使用 Web 后台的本群关键词页面。"
	case "white", "black":
		entries, e := s.Store.Rows(ctx, "SELECT user_id,username,expires_at FROM list_entries WHERE chat_id=? AND kind=? ORDER BY id DESC LIMIT 10", chat, section)
		if e != nil {
			return e
		}
		cmd := "whitelist"
		if section == "black" {
			cmd = "blacklist"
		}
		text += "本群名单（最近 10 条）：\n" + prettyMenu(entries) + "\n\n在本群发送：\n/" + cmd + " add 用户ID\n/" + cmd + " remove 用户ID"
	case "stats":
		stats, e := s.Store.Rows(ctx, "SELECT (SELECT COUNT(*) FROM group_members WHERE chat_id=? AND left_at IS NULL) AS known_members,(SELECT COUNT(*) FROM moderation_logs WHERE chat_id=?) AS reviewed_messages,(SELECT COUNT(*) FROM punishments WHERE chat_id=? AND status='done') AS punishments", chat, chat, chat)
		if e != nil {
			return e
		}
		text += "本群统计（已记录成员、累计审核、已完成处罚）：\n" + prettyMenu(stats)
	default:
		return nil
	}
	rows = append(rows, []menuButton{button("← 本群管理", prefix+"home"), button("切换群组", "menu:groups")})
	return s.groupMenuSend(ctx, m.Chat.ID, text, rows)
}
func prettyMenu(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	r := []rune(string(b))
	if len(r) > 2200 {
		return string(r[:2200]) + "\n…"
	}
	return string(r)
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
