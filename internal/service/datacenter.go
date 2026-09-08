package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"tgguard/internal/domain"
	"tgguard/internal/telegram"
	"time"
)

func (s *Service) dataCenter(ctx context.Context, m domain.Message, arg string) error {
	target := m.From.ID
	if m.Reply != nil {
		if m.Reply.From == nil || m.Reply.SenderChat != nil {
			return s.text(ctx, m.Chat.ID, "请回复用户发送的消息，匿名管理员或频道消息无法查询账号。")
		}
		target = m.Reply.From.ID
	}
	if arg != "" {
		id, e := strconv.ParseInt(arg, 10, 64)
		if e != nil || id <= 0 || id > 9007199254740991 {
			return s.text(ctx, m.Chat.ID, "用法：/dc 查询自己；回复用户消息发送 /dc；或 /dc 数字用户ID。")
		}
		target = id
	}
	allowed, e := s.State.Limit(ctx, fmt.Sprintf("dc:query:%d", m.From.ID), 5, time.Minute)
	if e != nil {
		return e
	}
	if !allowed {
		return s.text(ctx, m.Chat.ID, "查询较频繁，请一分钟后再试。")
	}
	dc, e := s.Bot.UserPhotoDC(ctx, target)
	if errors.Is(e, telegram.ErrDCUnavailable) {
		return s.text(ctx, m.Chat.ID, fmt.Sprintf("用户 ID：%d\n暂时无法查询：没有机器人可见的头像，或头像格式暂不支持。\nTelegram 不提供直接查询账号归属 DC 的 Bot API。", target))
	}
	if e != nil {
		slog.Warn("DC photo lookup failed", "target_user_id", target, "requester_id", m.From.ID, "error", e)
		return s.text(ctx, m.Chat.ID, "暂时无法读取该用户头像，请检查用户 ID，稍后重试。")
	}
	return s.text(ctx, m.Chat.ID, dataCenterReply(target, dc))
}

func dataCenterReply(user int64, dc int) string {
	region := "未知地区"
	switch dc {
	case 1, 3:
		region = "美国 · 迈阿密"
	case 2, 4:
		region = "荷兰 · 阿姆斯特丹"
	case 5:
		region = "新加坡"
	}
	return fmt.Sprintf("用户 ID：%d\n数据中心：DC%d\n地区：%s\n\n基于用户可见头像存储位置推测数据中心，仅供参考，不能确认账号归属或用户所在地。", user, dc, region)
}
