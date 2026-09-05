package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"tgguard/internal/ai"
	"tgguard/internal/domain"
	"tgguard/internal/i18n"
	"tgguard/internal/state"
	"tgguard/internal/store"
	"tgguard/internal/telegram"
)

type Service struct {
	Store       *store.Store
	State       *state.State
	Bot         *telegram.Client
	AI          ai.Provider
	SuperAdmins map[int64]bool
}

func (s *Service) Admin(ctx context.Context, chat, user int64) (bool, error) {
	if s.SuperAdmins[user] {
		return true, nil
	}
	m, e := s.Bot.Member(ctx, chat, user)
	return m.Admin(), e
}
func (s *Service) Protected(ctx context.Context, chat int64, u domain.User) (bool, error) {
	if u.IsBot || u.ID == s.Bot.ID || s.SuperAdmins[u.ID] {
		return true, nil
	}
	m, e := s.Bot.Member(ctx, chat, u.ID)
	if e != nil {
		return true, e
	}
	if m.Admin() {
		return true, nil
	}
	kind, e := s.Store.ListStatus(ctx, chat, u.ID, u.Username)
	return kind == "white" || kind == "trusted", e
}

// Cache is only for the analysis pipeline. Every punishment rechecks live permissions.
func (s *Service) ModerationProtected(ctx context.Context, chat int64, u domain.User) (bool, error) {
	if u.IsBot || u.ID == s.Bot.ID || s.SuperAdmins[u.ID] {
		return true, nil
	}
	key := fmt.Sprintf("group:admins:%d", chat)
	var admins []domain.Member
	if s.State.Get(ctx, key, &admins) != nil {
		if e := s.Bot.Call(ctx, "getChatAdministrators", map[string]any{"chat_id": chat}, &admins); e != nil {
			return true, e
		}
		if e := s.State.Put(ctx, key, admins, 30*time.Second); e != nil {
			return true, e
		}
	}
	for _, a := range admins {
		if a.User.ID == u.ID {
			return true, nil
		}
	}
	kind, e := s.Store.ListStatus(ctx, chat, u.ID, u.Username)
	return kind == "white" || kind == "trusted", e
}
func (s *Service) Say(ctx context.Context, chat int64, lang, key string, args ...any) error {
	_, e := s.Bot.Send(ctx, chat, i18n.Text(lang, key, args...), nil, 0)
	return e
}
func (s *Service) LogChannel(ctx context.Context, chat int64, text string) {
	settings, e := s.Store.Settings(ctx, chat)
	if e != nil {
		slog.Error("log settings failed", "chat_id", chat, "error", e)
		return
	}
	if settings.LogChannel != 0 {
		if _, e = s.Bot.Send(ctx, settings.LogChannel, text, nil, 0); e != nil {
			slog.Error("log channel send failed", "chat_id", chat, "error", e)
		}
	}
}
func (s *Service) Group(ctx context.Context, c domain.Chat) error {
	if c.Type != "supergroup" {
		return nil
	}
	return s.Store.RegisterGroup(ctx, c)
}
func (s *Service) BotMembership(ctx context.Context, u domain.MemberUpdate) error {
	if !u.New.Present() {
		return s.Store.DeactivateGroup(ctx, u.Chat.ID)
	}
	if e := s.Store.RegisterGroup(ctx, u.Chat); e != nil {
		return e
	}
	if u.Chat.Type != "supergroup" || !u.New.Admin() || !u.New.CanDelete || !u.New.CanRestrict {
		return s.Say(ctx, u.Chat.ID, "zh_CN", "bot_permissions")
	}
	var admins []domain.Member
	if e := s.Bot.Call(ctx, "getChatAdministrators", map[string]any{"chat_id": u.Chat.ID}, &admins); e != nil {
		return e
	}
	for _, a := range admins {
		if e := s.Store.Join(ctx, u.Chat.ID, a.User, a.Status); e != nil {
			return e
		}
	}
	return nil
}
func eventKey(update int64, source string) string { return fmt.Sprintf("%s:%d", source, update) }
