package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"tgguard/internal/domain"
)

func (s *Service) RegisterMenus(ctx context.Context) error {
	common := []map[string]string{{"command": "groups", "description": "选择我管理的群组"}, {"command": "settings", "description": "选择群组并修改群设置"}, {"command": "rules", "description": "选择群组查看和调整审核规则"}, {"command": "stats", "description": "选择群组查看统计"}, {"command": "keywords", "description": "选择群组管理关键词回复"}, {"command": "whitelist", "description": "选择群组管理白名单"}, {"command": "blacklist", "description": "选择群组管理黑名单"}, {"command": "start", "description": "打开主菜单"}, {"command": "menu", "description": "群管理菜单"}, {"command": "id", "description": "查看我的 Telegram ID"}, {"command": "version", "description": "查看运行版本"}}
	if e := s.Bot.Call(ctx, "setMyCommands", map[string]any{"commands": common, "scope": map[string]string{"type": "all_private_chats"}}, nil); e != nil {
		return e
	}
	group := []map[string]string{{"command": "check", "description": "回复消息进行 AI 审核"}}
	if e := s.Bot.Call(ctx, "setMyCommands", map[string]any{"commands": group, "scope": map[string]string{"type": "all_group_chats"}}, nil); e != nil {
		return e
	}
	admin := append(append([]map[string]string{}, group...), []map[string]string{{"command": "id", "description": "查看管理所需的群和用户 ID"}, {"command": "settings", "description": "打开本群管理菜单"}, {"command": "menu", "description": "打开本群管理菜单"}, {"command": "rules", "description": "查看本群审核规则"}, {"command": "warn", "description": "回复消息警告用户"}, {"command": "unmute", "description": "回复消息解除禁言"}, {"command": "unban", "description": "解除用户封禁"}, {"command": "stats", "description": "群统计"}, {"command": "keywords", "description": "关键词回复"}, {"command": "whitelist", "description": "白名单"}, {"command": "blacklist", "description": "黑名单"}, {"command": "mute", "description": "回复消息禁言用户"}, {"command": "ban", "description": "封禁用户并清理其全部群发言"}}...)
	if e := s.Bot.Call(ctx, "setMyCommands", map[string]any{"commands": admin, "scope": map[string]string{"type": "all_chat_administrators"}}, nil); e != nil {
		return e
	}
	return s.Bot.Call(ctx, "setChatMenuButton", map[string]any{"menu_button": map[string]string{"type": "commands"}}, nil)
}
func (s *Service) Home(ctx context.Context, m domain.Message) error {
	if m.From == nil || m.Chat.Type != "private" || m.Chat.ID != m.From.ID {
		return nil
	}
	role := "按所在群管理员权限管理已授权群组"
	if s.IsSuperAdmin(m.From.ID) {
		role = "机器人管理员"
	}
	text := fmt.Sprintf("TG Guard · 智能群管理\n\n你好，%s。\n当前身份：%s\n\n请选择下方功能。新人验证请从群内验证链接进入。", m.From.FirstName, role)
	markup := map[string]any{"inline_keyboard": [][]map[string]string{
		{{"text": "📋 我的群组 / 群设置", "callback_data": "menu:groups"}},
		{{"text": "👤 我的身份", "callback_data": "menu:profile"}},
		{{"text": "➕ 添加到群组", "url": "https://t.me/" + s.Bot.Username + "?startgroup=true"}, {"text": "📖 使用帮助", "callback_data": "menu:help"}},
	}}
	if s.IsSuperAdmin(m.From.ID) {
		markup["inline_keyboard"] = append(markup["inline_keyboard"].([][]map[string]string), []map[string]string{{"text": "管理后台", "callback_data": "menu:panel"}})
	}
	_, e := s.Bot.Send(ctx, m.Chat.ID, text, markup, 0)
	return e
}
func (s *Service) PrivateSection(ctx context.Context, m domain.Message, section string) error {
	if m.From == nil || m.Chat.Type != "private" || m.Chat.ID != m.From.ID {
		return nil
	}
	panel := "后台地址尚未设置，请联系机器人管理员获取。"
	if s.Runtime != nil && s.Runtime.Snapshot().PanelURL != "" {
		panel = s.Runtime.Snapshot().PanelURL
	}
	var text string
	switch section {
	case "groups":
		return s.MyGroups(ctx, m, 0)
	case "profile":
		role := "普通用户"
		if s.IsSuperAdmin(m.From.ID) {
			role = "机器人管理员"
		}
		text = fmt.Sprintf("我的身份\n\nTelegram ID：%d\n身份：%s\n\n要成为机器人管理员，请将此 ID 提供给现有机器人管理员，由其添加权限。", m.From.ID, role)
	case "panel":
		text = "管理面板\n\n" + panel + "\n\n后台需要管理员登录凭据。若地址无法访问，请联系机器人管理员确认。"
	case "admins":
		text = fmt.Sprintf("机器人管理员设置\n\n你的 Telegram ID：%d\n\n1. 机器人管理员登录 Web 管理面板。\n2. 打开「机器人管理员」。\n3. 添加这个数字 ID 并保存，立即生效。\n\n群管理员由 Telegram 群内任命，只管理所在群；机器人管理员负责审批，并管理已授权群。\n后台：%s", m.From.ID, panel)
	case "ai":
		text = "AI 接口设置\n\n在 Web 面板 → AI 接口填写：\n• API Base URL（含 /v1）\n• 模型名称\n• API Key\n\n保存并启用全局 AI 后，还需到「群管理」打开目标群的 AI 审核。Key 加密保存，不通过私聊显示。\n后台：" + panel
	case "help":
		text = "使用帮助\n\n新成员\n点击群内入群提示的验证按钮，进入私聊后按题目提示作答。无需在群里发命令；链接失效或找不到提示，请联系群管理员。\n\n群管理员\n点击「我的群组」选择群，再用按钮设置新人验证、审核规则和关键词回复。也可以在目标群发送 /settings 直达该群设置。\n\n接入新群\n把机器人设为群管理员，并授予删除消息、限制成员权限；联系机器人管理员批准接入后，群管理才会启用。\n\n模型接口和机器人超级管理员由机器人管理员在网页后台设置。"
		if s.IsSuperAdmin(m.From.ID) {
			text += "\n\n机器人超级管理员\n私聊 /approve 群ID 批准接入；/reject 群ID 拒绝；/revoke 群ID 撤销授权。命令后可附原因。"
		}
		return s.groupMenuSend(ctx, m.Chat.ID, text, [][]menuButton{{button("我的群组 / 群设置", "menu:groups"), button("主菜单", "menu:home")}})

	default:
		return s.Home(ctx, m)
	}
	_, e := s.Bot.Send(ctx, m.Chat.ID, text, map[string]any{"inline_keyboard": [][]map[string]string{{{"text": "← 返回主菜单", "callback_data": "menu:home"}}}}, 0)
	return e
}
func (s *Service) MenuCallback(ctx context.Context, c domain.Callback) error {
	if c.Message == nil || c.Message.Chat.Type != "private" || c.Message.Chat.ID != c.From.ID {
		return nil
	}
	// Group callbacks always recheck live group permissions before accessing data.
	_ = s.Bot.AnswerCallback(ctx, c.ID, "")
	m := *c.Message
	m.From = &c.From
	if strings.HasPrefix(c.Data, "gmc:") {
		data, e := s.resolveMenu(ctx, c)
		if e != nil {
			return e
		}
		if data == "" {
			return s.text(ctx, m.Chat.ID, "菜单已过期或不属于你，请发送 /menu 重新打开。")
		}
		return s.GroupMenu(ctx, m, data)
	}
	if strings.HasPrefix(c.Data, "gm:") {
		return s.text(ctx, m.Chat.ID, "旧版菜单已更新，请发送 /menu 重新打开。")
	}
	if strings.HasPrefix(c.Data, "menu:groups:") {
		parts := strings.Split(strings.TrimPrefix(c.Data, "menu:groups:"), ":")
		before, e := strconv.ParseInt(parts[0], 10, 64)
		section := "home"
		if len(parts) == 2 {
			section = parts[1]
		}
		if e != nil || before > 0 || len(parts) > 2 || !validMenuSection(section) {
			return nil
		}
		return s.MyGroups(ctx, m, before, section)
	}
	return s.PrivateSection(ctx, m, strings.TrimPrefix(c.Data, "menu:"))
}
