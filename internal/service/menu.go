package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"tgguard/internal/domain"
)

func (s *Service) RegisterMenus(ctx context.Context) error {
	common := []map[string]string{{"command": "groups", "description": "选择我管理的群组"}, {"command": "settings", "description": "选择群组并修改群设置"}, {"command": "rules", "description": "选择群组查看和调整审核规则"}, {"command": "stats", "description": "选择群组查看统计"}, {"command": "keywords", "description": "选择群组管理关键词回复"}, {"command": "whitelist", "description": "选择群组管理白名单"}, {"command": "blacklist", "description": "选择群组管理黑名单"}, {"command": "start", "description": "打开主菜单"}, {"command": "menu", "description": "群管理菜单"}, {"command": "id", "description": "查看我的 Telegram ID"}, {"command": "help", "description": "使用帮助"}, {"command": "version", "description": "查看运行版本"}}
	if e := s.Bot.Call(ctx, "setMyCommands", map[string]any{"commands": common, "scope": map[string]string{"type": "all_private_chats"}}, nil); e != nil {
		return e
	}
	group := []map[string]string{{"command": "check", "description": "回复消息进行 AI 审核"}, {"command": "verify", "description": "查看验证状态"}, {"command": "id", "description": "查看用户和群 ID"}, {"command": "help", "description": "使用帮助"}}
	if e := s.Bot.Call(ctx, "setMyCommands", map[string]any{"commands": group, "scope": map[string]string{"type": "all_group_chats"}}, nil); e != nil {
		return e
	}
	admin := append(append([]map[string]string{}, group...), []map[string]string{{"command": "settings", "description": "打开本群管理菜单"}, {"command": "menu", "description": "打开本群管理菜单"}, {"command": "rules", "description": "查看本群审核规则"}, {"command": "warn", "description": "回复消息警告用户"}, {"command": "unmute", "description": "回复消息解除禁言"}, {"command": "unban", "description": "解除用户封禁"}, {"command": "stats", "description": "群统计"}, {"command": "keywords", "description": "关键词回复"}, {"command": "whitelist", "description": "白名单"}, {"command": "blacklist", "description": "黑名单"}, {"command": "mute", "description": "回复消息禁言用户"}, {"command": "ban", "description": "回复消息封禁用户"}}...)
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
		markup["inline_keyboard"] = append(markup["inline_keyboard"].([][]map[string]string), []map[string]string{{"text": "部署者后台说明", "callback_data": "menu:panel"}})
	}
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
	case "groups":
		return s.MyGroups(ctx, m, 0)
	case "profile":
		role := "普通用户"
		if s.IsSuperAdmin(m.From.ID) {
			role = "机器人管理员"
		}
		text = fmt.Sprintf("我的身份\n\nTelegram ID：%d\n身份：%s\n\n要成为机器人管理员，请让部署者在 Web 面板 → 机器人管理员中添加此 ID。", m.From.ID, role)
	case "panel":
		text = "管理面板\n\n" + panel + "\n\n后台需要登录。部署电脑可运行 scripts/open-panel.ps1 一键登录。127.0.0.1 仅指当前设备；手机访问需由部署者配置可访问的后台地址。"
	case "admins":
		text = fmt.Sprintf("机器人管理员设置\n\n你的 Telegram ID：%d\n\n1. 部署者登录 Web 管理面板。\n2. 打开「机器人管理员」。\n3. 添加这个数字 ID 并保存，立即生效。\n\n群管理员由 Telegram 群内任命，只管理所在群；机器人管理员负责审批，并管理已授权群。\n后台：%s", m.From.ID, panel)
	case "ai":
		text = "AI 接口设置\n\n在 Web 面板 → AI 接口填写：\n• API Base URL（含 /v1）\n• 模型名称\n• API Key\n\n保存并启用全局 AI 后，还需到「群管理」打开目标群的 AI 审核。Key 加密保存，不通过私聊显示。\n后台：" + panel
	case "help":
		text = "使用流程\n\n① 将机器人添加到超级群并设为管理员，授予删除消息、限制成员权限。\n② 联系部署者在网页后台「群组列表」审批授权，通过后点击「我的群组」选择群组，设置本群验证、审核和处罚；也可在群里发送 /settings 直达本群菜单。\n③ 新成员通过群内链接进行私聊验证。\n④ 回复可疑消息发送 /check 进行 AI 复核。\n\n/groups 我的群组\n/menu 主菜单\n/id 我的 ID\n\n群管理员只能管理已获授权的群，不能自行审批。机器人超级管理员可私聊使用 /approve 群ID、/reject 群ID、/revoke 群ID（可附原因）。"
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
