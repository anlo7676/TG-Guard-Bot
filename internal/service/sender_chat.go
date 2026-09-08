package service

import (
	"context"
	"database/sql"
	"fmt"
	"tgguard/internal/domain"
	"tgguard/internal/rules"
	"tgguard/internal/store"
)

// A channel identity is not a Telegram user. Never mute or ban its synthetic sender.
func (s *Service) ModerateSenderChat(ctx context.Context, update int64, m domain.Message) error {
	if m.SenderChat == nil || m.SenderChat.ID >= 0 || m.Chat.Type != "supergroup" || m.SenderChat.ID == m.Chat.ID || m.IsAutomaticForward {
		return nil
	}
	if ok, e := s.Store.GroupAuthorized(ctx, m.Chat.ID); e != nil || !ok {
		return e
	}
	key := eventKey(update, "auto")
	if prior, e := s.Store.GetLog(ctx, key); e == nil {
		return s.Punish(ctx, prior, 0)
	} else if e != sql.ErrNoRows {
		return e
	}
	settings, e := s.Store.Settings(ctx, m.Chat.ID)
	if e != nil {
		return e
	}
	if !settings.ModerationEnabled {
		return nil
	}
	n := rules.Normalize(m)
	n.UserID = m.SenderChat.ID
	risk := rules.Evaluate(n, settings)
	var ai *domain.AIResult
	if settings.AIEnabled && s.AI != nil && risk.Score >= settings.AIThreshold && risk.Score < settings.DirectThreshold {
		if result, err := s.AI.Review(ctx, n, risk); err == nil {
			ai = &result
		}
	}
	decision := rules.Decide(risk, ai, settings, 0, false)
	if decision.Action != "allow" && decision.Action != "shadow_log" {
		decision = domain.Decision{Action: "warn", Delete: true, Reason: "sender_chat"}
	}
	l, e := s.Store.SaveLog(ctx, store.Log{EventKey: key, ChatID: m.Chat.ID, UserID: m.SenderChat.ID, MessageID: m.ID, Text: m.ModerationText(), Risk: risk, AI: ai, Decision: decision, Source: "channel"})
	if e != nil {
		return e
	}
	return s.Punish(ctx, l, 0)
}

func (s *Service) punishSenderChat(ctx context.Context, l store.Log, p store.Punishment) error {
	if p.Decision.Action != "warn" && p.Decision.Action != "delete" {
		return fmt.Errorf("频道身份不支持用户禁言或封禁，请删除消息或在 Telegram 管理频道发言权限")
	}
	if !p.Deleted {
		if e := s.Bot.Delete(ctx, l.ChatID, l.MessageID); e != nil {
			return e
		}
		if e := s.Store.PunishmentStep(ctx, l.EventKey, "deleted"); e != nil {
			return e
		}
	}
	if p.Decision.Action == "warn" && !p.Acted {
		if _, e := s.Bot.Send(ctx, l.ChatID, "频道身份发送的违规消息已删除。请停止发送广告；群管理员可在 Telegram 中限制该频道发言。", nil, 0); e != nil {
			return e
		}
		if e := s.Store.PunishmentStep(ctx, l.EventKey, "acted"); e != nil {
			return e
		}
	}
	return s.Store.PunishmentStep(ctx, l.EventKey, "done")
}
