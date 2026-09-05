package service

import (
	"context"
	"fmt"
	"strings"
	"tgguard/internal/domain"
)

func (s *Service) RegisterMenus(ctx context.Context) error {
	common := []map[string]string{{"command": "start", "description": "打开主菜单"}, {"command": "menu", "description": "功能菜单"}, {"command": "id", "description": "查看我的 Telegram ID"}, {"command": "panel", "description": "打开管理面板"}, {"command": "help", "description": "使用帮助"}}
	if e := s.Bot.Call(ctx, "setMyCommands", map[string]any{"commands": common, "scope": map[string]string{"type": "all_private_chats"}}, nil); e != nil {
		return e
	}
	group := []map[string]string{{"command": "check", "description": "回复消息进行 AI 审核"}, {"command": "verify", "description": "查看验证状态"}, {"command": "id", "description": "查看用户和群 ID"}, {"command": "help", "description": "使用帮助"}}
	if e := s.Bot.Call(ctx, "setMyCommands", map[string]any{"commands": group, "scope": map[string]string{"type": "all_group_chats"}}, nil); e != nil {
		return e
	}
	admin := append(append([]map[string]string{}, group...), []map[string]string{{"command": "settings", "description": "群设置"}, {"command": "stats", "description": "群统计"}, {"command": "keywords", "description": "关键词回复"}, {"command": "whitelist", "description": "白名单"}, {"command": "blacklist", "description": "黑名单"}, {"command": "mute", "description": "回复消息禁言用户"}, {"command": "ban", "description": "回复消息封禁用户"}}...)
	if e := s.Bot.Call(ctx, "setMyCommands", map[string]any{"commands": admin, "scope": map[string]string{"type": "all_chat_administrators"}}, nil); e != nil {
		return e
	}
	return s.Bot.Call(ctx, "setChatMenuButton", map[string]any{"menu_button": map[string]string{"type": "commands"}}, nil)
}
func (s *Service) Home(ctx context.Context, m domain.Message) error {
	if m.From == nil {
		return nil
	}
	role := "普通用户"
	if s.IsSuperAdmin(m.From.ID) {
		role = "机器人管理员"
	}
	text := fmt.Sprintf("TG Guard · 智能群管理\n\n你好，%s。\n当前身份：%s\n\n请选择下方功能。新人验证请从群内验证链接进入。", m.From.FirstName, role)
	markup := map[string]any{"inline_keyboard": [][]map[string]string{
		{{"text": "🖥 管理面板", "callback_data": "menu:panel"}, {"text": "👤 我的身份", "callback_data": "menu:profile"}},
		{{"text": "🛡 管理员设置", "callback_data": "menu:admins"}, {"text": "🧠 AI 接口设置", "callback_data": "menu:ai"}},
		{{"text": "➕ 添加到群组", "url": "https://t.me/" + s.Bot.Username + "?startgroup=true"}, {"text": "📖 使用帮助", "callback_data": "menu:help"}},
	}}
	_, e := s.Bot.Send(ctx, m.Chat.ID, text, markup, 0)
	return e
}
func (s *Service) PrivateSection(ctx context.Context, m domain.Message, section string) error {
	if m.From == nil || m.Chat.Type != "private" || m.Chat.ID != m.From.ID {
		return nil
	}
	panel := "http://127.0.0.1:8080/"
	if s.Runtime != nil && s.Runtime.Snapshot().PanelURL != "" {
		panel = s.Runtime.Snapshot().PanelURL
	}
	var text string
	switch section {
	case "profile":
		role := "普通用户"
		if s.IsSuperAdmin(m.From.ID) {
			role = "机器人管理员"
		}
		text = fmt.Sprintf("我的身份\n\nTelegram ID：%d\n身份：%s\n\n要成为机器人管理员，请让部署者在 Web 面板 → 机器人管理员中添加此 ID。", m.From.ID, role)
	case "panel":
		text = "管理面板\n\n" + panel + "\n\n后台需要登录。部署电脑可运行 scripts/open-panel.ps1 一键登录。127.0.0.1 仅指当前设备；手机访问需由部署者配置可访问的后台地址。"
	case "admins":
		text = fmt.Sprintf("机器人管理员设置\n\n你的 Telegram ID：%d\n\n1. 部署者登录 Web 管理面板。\n2. 打开「机器人管理员」。\n3. 添加这个数字 ID 并保存，立即生效。\n\n群管理员由 Telegram 群内任命，只管理所在群；机器人管理员可管理所有接入群。\n后台：%s", m.From.ID, panel)
	case "ai":
		text = "AI 接口设置\n\n在 Web 面板 → AI 接口填写：\n• API Base URL（含 /v1）\n• 模型名称\n• API Key\n\n保存并启用全局 AI 后，还需到「群管理」打开目标群的 AI 审核。Key 加密保存，不通过私聊显示。\n后台：" + panel
	case "help":
		text = "使用流程\n\n① 将机器人添加到超级群并设为管理员，授予删除消息、限制成员权限。\n② 在 Web 面板配置群验证、审核和关键词。\n③ 新成员通过群内链接进行私聊验证。\n④ 回复可疑消息发送 /check 进行 AI 复核。\n\n/menu 主菜单\n/id 我的 ID\n/panel 管理面板"
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
	// Menu callbacks only display information. They never grant roles or reveal credentials.
	_ = s.Bot.AnswerCallback(ctx, c.ID, "")
	m := *c.Message
	m.From = &c.From
	return s.PrivateSection(ctx, m, strings.TrimPrefix(c.Data, "menu:"))
}
