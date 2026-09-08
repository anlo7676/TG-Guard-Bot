package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"tgguard/internal/telegram"
	"time"
	"unicode/utf16"

	"tgguard/internal/domain"
	"tgguard/internal/store"
)

var welcomeVariable = regexp.MustCompile(`\{(?:name|username|user_id|group|timeout)\}`)

func welcomeText(v domain.Settings, chat domain.Chat, u domain.User, verify bool) string {
	text, _ := welcomeMessage(v, chat, u, verify)
	return text
}

// Entity offsets use Telegram's UTF-16 units, including emoji in names/templates.
func welcomeMessage(v domain.Settings, chat domain.Chat, u domain.User, verify bool) (string, []domain.Entity) {
	if !verify && !v.WelcomeEnabled {
		return "", nil
	}
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name == "" {
		name = fmt.Sprint(u.ID)
	}
	username := name
	if u.Username != "" {
		username = "@" + u.Username
	}
	template := strings.TrimSpace(v.WelcomeText)
	if verify {
		template = "{username}，请在 {timeout} 秒内点击下方验证按钮，进入私聊完成验证。验证期间暂时不能发言。"
	}
	if !strings.Contains(template, "{name}") && !strings.Contains(template, "{username}") && !strings.Contains(template, "{user_id}") {
		template = "{username}，" + template
	}
	values := map[string]string{"{name}": name, "{username}": username, "{user_id}": fmt.Sprint(u.ID), "{group}": chat.Title, "{timeout}": fmt.Sprint(v.VerificationTimeout)}
	var out strings.Builder
	entities := []domain.Entity{}
	offset := 0
	exhausted := false
	appendText := func(text string, mention bool) {
		if exhausted {
			return
		}
		start := offset
		for _, r := range text {
			n := utf16.RuneLen(r)
			if offset+n > 3000 {
				exhausted = true
				break
			}
			out.WriteRune(r)
			offset += n
		}
		if mention && offset > start {
			entities = append(entities, domain.Entity{Type: "text_link", Offset: start, Length: offset - start, URL: fmt.Sprintf("tg://user?id=%d", u.ID)})
		}
	}
	pos := 0
	for _, span := range welcomeVariable.FindAllStringIndex(template, -1) {
		appendText(template[pos:span[0]], false)
		key := template[span[0]:span[1]]
		appendText(values[key], key == "{name}" || key == "{username}" || key == "{user_id}")
		pos = span[1]
	}
	appendText(template[pos:], false)
	return out.String(), entities
}

func (s *Service) sendWelcome(ctx context.Context, chat domain.Chat, u domain.User, v domain.Settings, verify bool, markup any) (int64, error) {
	text, entities := welcomeMessage(v, chat, u, verify)
	in := map[string]any{"chat_id": chat.ID, "text": text, "entities": entities}
	if markup != nil {
		in["reply_markup"] = markup
	}
	var sent domain.Message
	e := s.Bot.Call(ctx, "sendMessage", in, &sent)
	return sent.ID, e
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
	message, e := s.sendWelcome(ctx, chat, u, v, false, nil)
	if e != nil {
		return e
	}
	if e = s.Store.MarkWelcomed(ctx, chat.ID, u.ID, message); e != nil {
		// Avoid leaving an unscheduled welcome when recording fails.
		if cleanupErr := s.executor().Delete(ctx, chat.ID, message); cleanupErr != nil {
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
	recoverGroups(ctx, items, func(v store.WelcomeCleanup) int64 { return v.ChatID }, func(ctx context.Context, item store.WelcomeCleanup) {
		if ctx.Err() != nil {
			return
		}
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := s.executor().Delete(c, item.ChatID, item.MessageID)
		cancel()
		done, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		var te *telegram.APIError
		permanent := errors.As(err, &te) && te.Code == 400
		recordErr := s.Store.FinishWelcomeCleanup(done, item, err, permanent)
		stop()
		if recordErr != nil {
			slog.Error("welcome cleanup audit failed", "chat_id", item.ChatID, "error", recordErr)
			return
		}
		if err != nil {
			slog.Warn("welcome deletion failed", "terminal", permanent, "chat_id", item.ChatID, "message_id", item.MessageID, "error", err)
		}
	})
	return nil
}
