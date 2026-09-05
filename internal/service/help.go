package service

import (
	"context"
	"fmt"
	"tgguard/internal/domain"
)

func (s *Service) GroupHelp(ctx context.Context, m domain.Message) error {
	v, e := s.Store.Settings(ctx, m.Chat.ID)
	if e != nil {
		return e
	}
	admin, e := s.Admin(ctx, m.Chat.ID, m.From.ID)
	if e != nil {
		return e
	}
	text := "本群使用帮助\n\n新成员验证\n"
	if v.VerificationEnabled {
		text += "入群后点击欢迎消息中的验证按钮，到机器人私聊按题目提示完成验证。验证期间不能在群里发言，无需发送命令。链接失效或找不到提示，请联系群管理员。"
	} else {
		text += "本群目前未开启新人验证，无需进行验证。"
	}
	text += "\n\n可疑消息"
	if v.AIEnabled {
		text += "\n先长按可疑消息选择「回复」，再发送 /check。能否使用由本群审核权限决定；无法使用时请联系群管理员。"
	} else {
		text += "\n本群尚未开启手动 AI 复核，发现广告或可疑内容请联系群管理员。"
	}
	rows := [][]menuButton{}
	if admin {
		text += "\n\n群管理员操作\n• 点击下方「本群设置」，在私聊中配置验证、审核规则、关键词和黑白名单。\n• 回复目标成员的消息后发送 /warn 警告、/mute 1h 禁言一小时、/unmute 解除禁言，或 /ban 封禁并清理该用户在本群的全部发言。\n• /unban 用户ID 可解除封禁；/stats 查看群统计。"
		rows = append(rows, []menuButton{{"text": "本群设置", "url": fmt.Sprintf("https://t.me/%s?start=group_%d", s.Bot.Username, m.Chat.ID)}})
	}
	rows = append(rows, []menuButton{{"text": "打开机器人私聊", "url": "https://t.me/" + s.Bot.Username + "?start=help"}})
	return s.groupMenuSend(ctx, m.Chat.ID, text, rows)
}
