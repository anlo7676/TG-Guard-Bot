package bot

import (
	"context"
	"fmt"
	"strings"

	"tgguard/internal/domain"
	"tgguard/internal/service"
)

type Handler struct {
	Service  *service.Service
	Queue    Inbox
	Telegram Caller
	Process  func(context.Context, domain.Update) error
	Health   *service.IngestionHealth
}

func (h *Handler) Handle(ctx context.Context, u domain.Update) error {
	s := h.Service
	switch {
	case u.MyMember != nil:
		return s.BotMembership(ctx, *u.MyMember)
	case u.Member != nil:
		m := u.Member
		if m.Old.Admin() != m.New.Admin() {
			if e := s.State.R.Del(ctx, fmt.Sprintf("group:admins:%d", m.Chat.ID)).Err(); e != nil {
				return e
			}
			if e := s.Store.MemberRole(ctx, m.Chat.ID, m.New.User, m.New.Status); e != nil {
				return e
			}
		}
		if !m.New.Present() {
			return s.Store.Leave(ctx, m.Chat.ID, m.New.User.ID)
		}
		if !m.Old.Present() && m.New.Present() {
			return s.Join(ctx, m.Chat, m.New.User)
		}
		return nil
	case u.Callback != nil:
		return s.Callback(ctx, *u.Callback)
	}
	m := u.Message
	if m == nil {
		m = u.Edited
	}
	if m == nil {
		return nil
	}
	if m.SenderChat != nil {
		return s.ModerateSenderChat(ctx, u.ID, *m)
	}
	if m.From == nil {
		return nil
	}
	if len(m.NewMembers) > 0 {
		for _, member := range m.NewMembers {
			if e := s.Join(ctx, m.Chat, member); e != nil {
				return e
			}
		}
		return nil
	}
	if m.LeftMember != nil {
		return s.Store.Leave(ctx, m.Chat.ID, m.LeftMember.ID)
	}
	if m.From.IsBot {
		return nil
	}
	if m.Chat.Type == "private" {
		if handled, e := s.GroupReply(ctx, *m); handled || e != nil {
			return e
		}
		if command, arg, ok := service.ParseCommand(m.Text, s.Bot.Username); ok {
			return s.Command(ctx, u.ID, *m, command, arg)
		}
		return s.VerificationReply(ctx, *m)
	}
	// Anonymous senders/channels cannot be mapped safely to a punishable user.
	if m.SenderChat != nil {
		return nil
	}
	if m.Chat.Type != "supergroup" {
		if m.Chat.Type == "group" && strings.HasPrefix(m.Text, "/settings") {
			return s.Say(ctx, m.Chat.ID, "zh_CN", "bot_permissions")
		}
		return nil
	}
	if e := s.Group(ctx, m.Chat); e != nil {
		return e
	}
	if ok, e := s.Store.GroupAuthorized(ctx, m.Chat.ID); e != nil {
		return e
	} else if !ok {
		if _, _, command := service.ParseCommand(m.Text, s.Bot.Username); command {
			return s.AuthorizationNotice(ctx, m.Chat.ID)
		}
		return nil
	}
	// A command or bot mention must not provide an escape hatch for advertising.
	if e := s.Moderate(ctx, u.ID, *m); e != nil {
		return e
	}
	l, e := s.Store.GetLog(ctx, fmt.Sprintf("auto:%d", u.ID))
	if e != nil {
		return e
	}
	if l.Decision.Action != "allow" && l.Decision.Action != "shadow_log" {
		return nil
	}
	if u.Edited == nil {
		if command, arg, ok := service.ParseCommand(m.Text, s.Bot.Username); ok {
			return s.Command(ctx, u.ID, *m, command, arg)
		}
		if MentionsBot(m.Text, s.Bot.Username) {
			return s.Review(ctx, u.ID, *m)
		}
	}
	return nil
}
func MentionsBot(text, username string) bool {
	needle := "@" + strings.ToLower(username)
	text = strings.ToLower(text)
	for start := 0; start < len(text); {
		i := strings.Index(text[start:], needle)
		if i < 0 {
			return false
		}
		i += start
		end := i + len(needle)
		if end == len(text) || !usernameChar(text[end]) {
			return true
		}
		start = end
	}
	return false
}
func usernameChar(c byte) bool { return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' }
