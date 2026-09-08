package service

import (
	"context"
	"database/sql"
	"fmt"

	"strconv"
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
	if settings.SpamEnabled {
		text := n.Text
		if text == "" {
			text = strconv.FormatInt(update, 10)
		}
		rate, dup, err := s.State.Spam(ctx, m.Chat.ID, m.SenderChat.ID, update, text, settings.RateWindow)
		if err != nil {
			return err
		}
		if rate > settings.RateLimit || dup >= settings.DuplicateLimit {
			risk.Spam = true
			risk.Score = 100
			risk.Matches = append(risk.Matches, domain.Match{Rule: "spam", Score: 100, Reason: fmt.Sprintf("频道刷屏 rate=%d duplicate=%d", rate, dup)})
		}
	}
	var ai *domain.AIResult
	if settings.AIEnabled && s.AI != nil && risk.Score >= settings.AIThreshold {
		ai = s.reviewAI(ctx, n, &risk, "channel", key)
	}
	decision := rules.Decide(risk, ai, settings, 0, false)
	if decision.Action != "allow" && decision.Action != "shadow_log" {
		if decision.Action == "mute" || decision.Action == "ban" || decision.Action == "kick" {
			decision.Action = "warn"
		}
		decision.Duration = 0
		decision.Reason = "sender_chat"
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
	if !p.Deleted && (p.Decision.Delete || p.Decision.Action == "delete") {
		if e := s.executor().Delete(ctx, l.ChatID, l.MessageID); e != nil {
			return e
		}
		if e := s.Store.PunishmentStep(ctx, l.EventKey, "deleted"); e != nil {
			return e
		}
	}
	if p.Decision.Action == "warn" && !p.Acted {
		notice := "请停止通过频道身份发送广告或刷屏。群管理员可在 Telegram 中限制该频道发言。"
		if p.Decision.Delete {
			notice = "频道身份发送的违规消息已删除。" + notice
		}
		if _, e := s.Bot.Send(ctx, l.ChatID, notice, nil, 0); e != nil {
			return e
		}
		if e := s.Store.PunishmentStep(ctx, l.EventKey, "acted"); e != nil {
			return e
		}
	}
	return s.Store.PunishmentStep(ctx, l.EventKey, "done")
}
