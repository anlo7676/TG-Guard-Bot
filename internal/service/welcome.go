package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"tgguard/internal/domain"
	"time"
)

func welcomeText(v domain.Settings, chat domain.Chat, u domain.User, verify bool) string {
	if verify {
		name := u.FirstName
		if name == "" {
			name = fmt.Sprint(u.ID)
		}
		return fmt.Sprintf("%s，请在 %d 秒内点击下方验证按钮，进入私聊完成验证。验证期间暂时不能发言。", name, v.VerificationTimeout)
	}
	text := ""
	if v.WelcomeEnabled {
		name := u.FirstName
		if name == "" {
			name = fmt.Sprint(u.ID)
		}
		username := u.Username
		if username != "" {
			username = "@" + username
		} else {
			username = name
		}
		text = strings.NewReplacer("{name}", name, "{username}", username, "{user_id}", fmt.Sprint(u.ID), "{group}", chat.Title, "{timeout}", fmt.Sprint(v.VerificationTimeout)).Replace(v.WelcomeText)
		r := []rune(text)
		if len(r) > 3000 {
			text = string(r[:3000])
		}
	}
	return strings.TrimSpace(text)
}
func (s *Service) welcomeOnly(ctx context.Context, chat domain.Chat, u domain.User, v domain.Settings) error {
	if !v.WelcomeEnabled {
		return nil
	}
	unlock, e := s.State.Lock(ctx, verifyLock(chat.ID, u.ID), 60*time.Second)
	if e != nil {
		return e
	}
	defer unlock()
	return s.welcomeLocked(ctx, chat, u, v)
}

// Caller holds verifyLock, including verification recovery.
func (s *Service) welcomeLocked(ctx context.Context, chat domain.Chat, u domain.User, v domain.Settings) error {
	if !v.WelcomeEnabled {
		return nil
	}
	sent, e := s.Store.WelcomeSent(ctx, chat.ID, u.ID)
	if e != nil || sent {
		return e
	}
	if ok, e := s.Store.GroupAuthorized(ctx, chat.ID); e != nil || !ok {
		return e
	}
	message, e := s.Bot.Send(ctx, chat.ID, welcomeText(v, chat, u, false), nil, 0)
	if e != nil {
		return e
	}
	if e = s.Store.MarkWelcomed(ctx, chat.ID, u.ID, message); e != nil {
		// Avoid leaving an unscheduled welcome when recording fails.
		if cleanupErr := s.Bot.Delete(ctx, chat.ID, message); cleanupErr != nil {
			slog.Error("unscheduled welcome cleanup failed", "chat_id", chat.ID, "message_id", message, "error", cleanupErr)
		}
		return e
	}
	return nil
}

func (s *Service) SweepWelcomeCleanup(ctx context.Context) error {
	items, e := s.Store.DueWelcomeCleanup(ctx)
	if e != nil {
		return e
	}
	for _, item := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := s.Bot.Delete(c, item.ChatID, item.MessageID)
		cancel()
		done, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		recordErr := s.Store.FinishWelcomeCleanup(done, item, err)
		stop()
		if recordErr != nil {
			return recordErr
		}
		if err != nil {
			slog.Warn("welcome deletion will retry", "chat_id", item.ChatID, "message_id", item.MessageID, "error", err)
		}
	}
	return nil
}
