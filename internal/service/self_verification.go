package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"tgguard/internal/domain"
	"tgguard/internal/state"
	"tgguard/internal/store"
	"tgguard/internal/telegram"
	"time"
)

func (s *Service) SelfVerificationMenu(ctx context.Context, m domain.Message, pages ...int64) error {
	if m.From == nil || m.Chat.Type != "private" || m.Chat.ID != m.From.ID {
		return nil
	}
	before := int64(0)
	if len(pages) > 0 {
		before = pages[0]
	}
	groups, err := s.Store.SelfVerificationGroups(ctx, before)
	if err != nil {
		return err
	}
	rows := [][]map[string]string{}
	for _, g := range groups {
		member, err := s.Bot.Member(ctx, g.ID, m.From.ID)
		if err != nil {
			var te *telegram.APIError
			if errors.As(err, &te) && (te.Code == 400 || te.Code == 403) {
				continue
			}
			return err
		}
		if member.Present() && member.Status == "restricted" {
			rows = append(rows, []map[string]string{{"text": g.Title, "callback_data": fmt.Sprintf("sg:%d", g.ID)}})
		}
	}
	text := "自助验证解除禁言\n选择群组并完成验证，即可恢复发言。支持验证超时、广告处罚和管理员禁言；解禁后不发送欢迎语。封禁或已离群不适用。"
	if len(rows) == 0 {
		text = "本页未找到你仍在群内且被禁言的群组。"
	}
	if len(groups) == 20 {
		rows = append(rows, []map[string]string{{"text": "继续查找群组", "callback_data": fmt.Sprintf("sp:%d", groups[len(groups)-1].ID)}})
	}
	rows = append(rows, []map[string]string{{"text": "返回主菜单", "callback_data": "menu:home"}})
	_, err = s.Bot.Send(ctx, m.Chat.ID, text, map[string]any{"inline_keyboard": rows}, 0)
	return err
}

// Old verification menu buttons are still bound to their original user.
func (s *Service) SelfVerification(ctx context.Context, m domain.Message, token string) error {
	if m.From == nil || m.Chat.Type != "private" || m.Chat.ID != m.From.ID {
		return nil
	}
	v, err := s.Store.Verification(ctx, token)
	if err == sql.ErrNoRows {
		return s.SelfVerificationMenu(ctx, m)
	}
	if err != nil {
		return err
	}
	if v.UserID != m.From.ID {
		return s.text(ctx, m.Chat.ID, "此验证不属于你，请重新打开自助验证菜单。")
	}
	return s.StartSelfVerification(ctx, m, v.ChatID)
}

func (s *Service) StartSelfVerification(ctx context.Context, m domain.Message, chat int64) error {
	if m.From == nil || m.Chat.Type != "private" || m.Chat.ID != m.From.ID || chat >= 0 {
		return nil
	}
	unlock, err := s.State.Lock(ctx, verifyLock(chat, m.From.ID), 60*time.Second)
	if err != nil {
		return err
	}
	defer unlock()
	ok, err := s.Store.GroupAuthorized(ctx, chat)
	if err != nil {
		return err
	}
	if !ok {
		return s.text(ctx, m.Chat.ID, "该群尚未授权或已暂停服务。")
	}
	member, err := s.Bot.Member(ctx, chat, m.From.ID)
	if err != nil {
		return err
	}
	if !member.Present() || member.Status != "restricted" {
		return s.text(ctx, m.Chat.ID, "你目前不在该群，或没有被禁言，无需自助解禁。封禁请联系群管理员。")
	}
	if busy, err := s.Store.VerificationTakenOver(ctx, chat, m.From.ID); err != nil {
		return err
	} else if busy {
		return s.text(ctx, m.Chat.ID, "当前权限操作正在处理，请稍后重新验证。")
	}
	old, err := s.Store.ActiveVerification(ctx, chat, m.From.ID)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil && strings.HasPrefix(old.Token, "self_") && old.Status == "pending" && time.Now().Before(old.ExpiresAt) {
		return s.StartVerification(ctx, m, old.Token)
	}
	allowed, err := s.State.Limit(ctx, fmt.Sprintf("verify:restart:%d:%d", chat, m.From.ID), 3, time.Hour)
	if err != nil {
		return err
	}
	if !allowed {
		return s.text(ctx, m.Chat.ID, "验证次数过多，请一小时后再试，或联系群管理员。")
	}
	settings, err := s.Store.Settings(ctx, chat)
	if err != nil {
		return err
	}
	token, err := state.Token()
	if err != nil {
		return err
	}
	v := store.Verification{Token: "self_" + token, ChatID: chat, UserID: m.From.ID, Type: settings.VerificationType, FailAction: "mute", ExpiresAt: time.Now().UTC().Add(time.Duration(settings.VerificationTimeout) * time.Second), Question: "请选择「我是人类」完成验证。"}
	answer := "human"
	if v.Type == "math" {
		x, e := rand.Int(rand.Reader, big.NewInt(19))
		if e != nil {
			return e
		}
		y, e := rand.Int(rand.Reader, big.NewInt(19))
		if e != nil {
			return e
		}
		v.Question = fmt.Sprintf("%d + %d = ?", x.Int64()+1, y.Int64()+1)
		answer = strconv.FormatInt(x.Int64()+y.Int64()+2, 10)
	}
	v.AnswerHash = store.HashAnswer(v.Token, answer)
	if err = s.Store.CreateSelfVerification(ctx, v); err != nil {
		return err
	}
	return s.StartVerification(ctx, m, v.Token)
}

// Called under the same member lock as AnswerVerification and recovery.
func (s *Service) finishSelfVerification(ctx context.Context, v store.Verification) error {
	settings, err := s.Store.Settings(ctx, v.ChatID)
	if err != nil {
		return err
	}
	if verificationTerminal(v.Status) {
		return s.finishVerificationNotices(ctx, v, settings)
	}
	status := "expired"
	authorized, err := s.Store.GroupAuthorized(ctx, v.ChatID)
	if err != nil {
		return err
	}
	if !authorized || v.Status == "releasing" {
		status = "cancelled"
	} else if v.Status == "completing" {
		member, err := s.Bot.Member(ctx, v.ChatID, v.UserID)
		if err != nil {
			return err
		}
		if !member.Present() {
			status = "left"
		} else {
			busy, err := s.Store.SelfVerificationBusy(ctx, v)
			if err != nil {
				return err
			}
			if busy {
				return fmt.Errorf("self verification awaiting permission reconciliation")
			}
			l := store.Log{EventKey: "self-unmute:" + v.Token, ChatID: v.ChatID, UserID: v.UserID, Source: "manual", Decision: domain.Decision{Action: "unmute", Reason: "self_verification"}}
			if err = s.punishLocked(ctx, l, v.UserID); err != nil {
				return err
			}
			done, err := s.Store.SelfUnmuteDone(ctx, l.EventKey)
			if err != nil {
				return err
			}
			if !done {
				status = "cancelled"
			} else {
				status = "verified"
			}
		}
	} else if v.Status != "expiring" {
		return nil
	}
	if err = s.Store.FinishVerification(ctx, v, status); err != nil {
		return err
	}
	v.Status = status
	return s.finishVerificationNotices(ctx, v, settings)
}

func (s *Service) ObserveVerificationTakeover(ctx context.Context, m domain.MemberUpdate) error {
	if m.From.ID == 0 || m.From.ID == s.Bot.ID || m.Date == 0 || !m.Old.Present() || (m.Old.Status != "restricted" && m.New.Status != "restricted") {
		return nil
	}
	unlock, err := s.State.Lock(ctx, verifyLock(m.Chat.ID, m.New.User.ID), 60*time.Second)
	if err != nil {
		return err
	}
	defer unlock()
	return s.Store.CancelExternallyManagedVerification(ctx, m.Chat.ID, m.New.User.ID, m.Date)
}
