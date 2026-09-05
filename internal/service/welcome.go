package service

import (
	"context"
	"fmt"
	"strings"
	"tgguard/internal/domain"
	"time"
)

func welcomeText(v domain.Settings, chat domain.Chat, u domain.User, verify bool) string {
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
	if verify {
		text += fmt.Sprintf("\n\n请在 %d 秒内点击下方验证按钮，进入私聊完成验证。验证期间暂时不能发言。", v.VerificationTimeout)
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
	sent, e := s.Store.WelcomeSent(ctx, chat.ID, u.ID)
	if e != nil || sent {
		return e
	}
	if ok, e := s.Store.GroupAuthorized(ctx, chat.ID); e != nil || !ok {
		return e
	}
	if _, e = s.Bot.Send(ctx, chat.ID, welcomeText(v, chat, u, false), nil, 0); e != nil {
		return e
	}
	return s.Store.MarkWelcomed(ctx, chat.ID, u.ID)
}
