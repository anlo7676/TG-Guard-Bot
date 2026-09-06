package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"tgguard/internal/domain"
	"tgguard/internal/i18n"
	"tgguard/internal/state"
	"tgguard/internal/store"
)

func verifyLock(chat, user int64) string { return fmt.Sprintf("verify:lock:%d:%d", chat, user) }
func (s *Service) Join(ctx context.Context, chat domain.Chat, u domain.User) error {
	if chat.Type != "supergroup" || u.IsBot {
		return nil
	}
	if e := s.Group(ctx, chat); e != nil {
		return e
	}
	if ok, e := s.Store.GroupAuthorized(ctx, chat.ID); e != nil || !ok {
		return e
	}
	if e := s.Store.Join(ctx, chat.ID, u, "member"); e != nil {
		return e
	}
	protected, e := s.Protected(ctx, chat.ID, u)
	if e != nil {
		return e
	}
	settings, e := s.Store.Settings(ctx, chat.ID)
	if e != nil {
		return e
	}
	kind, e := s.Store.ListStatus(ctx, chat.ID, u.ID, u.Username)
	if e != nil {
		return e
	}
	if kind == "black" {
		return s.Punish(ctx, store.Log{EventKey: fmt.Sprintf("blackjoin:%d:%d:%d", chat.ID, u.ID, time.Now().Unix()/60), ChatID: chat.ID, UserID: u.ID, Decision: domain.Decision{Action: "ban", Reason: "blacklist"}, Source: "blacklist"}, 0)
	}
	if protected || !settings.VerificationEnabled {
		return s.welcomeOnly(ctx, chat, u, settings)
	}
	unlock, e := s.State.Lock(ctx, verifyLock(chat.ID, u.ID), 60*time.Second)
	if e != nil {
		return e
	}
	defer unlock()
	alreadyVerified, e := s.Store.VerifiedCurrentJoin(ctx, chat.ID, u.ID)
	if e != nil {
		return e
	}
	if alreadyVerified {
		return nil
	}
	v, e := s.Store.ActiveVerification(ctx, chat.ID, u.ID)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	if e == sql.ErrNoRows {
		token, e := state.Token()
		if e != nil {
			return e
		}
		v = store.Verification{Token: token, ChatID: chat.ID, UserID: u.ID, Type: settings.VerificationType, Status: "pending", FailAction: settings.VerificationFailAction, ExpiresAt: time.Now().UTC().Add(time.Duration(settings.VerificationTimeout) * time.Second)}
		answer := "human"
		v.Question = i18n.Text(settings.Language, "human_question")
		if v.Type == "math" {
			a, e := rand.Int(rand.Reader, big.NewInt(19))
			if e != nil {
				return e
			}
			b, e := rand.Int(rand.Reader, big.NewInt(19))
			if e != nil {
				return e
			}
			x, y := a.Int64()+1, b.Int64()+1
			v.Question = fmt.Sprintf("%d + %d = ?", x, y)
			answer = strconv.FormatInt(x+y, 10)
		}
		v.AnswerHash = store.HashAnswer(token, answer)
		if e = s.Store.CreateVerification(ctx, v); e != nil {
			return e
		}
	}
	if v.Status != "pending" {
		return nil
	}
	// Persist first so a crash after restriction can still be recovered by the timeout worker.
	if e = s.Bot.Restrict(ctx, chat.ID, u.ID, 0); e != nil {
		return e
	}
	if v.PromptID == 0 {
		markup := map[string]any{"inline_keyboard": [][]map[string]string{{{"text": i18n.Text(settings.Language, "verify_button"), "url": "https://t.me/" + s.Bot.Username + "?start=verify_" + v.Token}}}}
		id, e := s.Bot.Send(ctx, chat.ID, welcomeText(settings, chat, u, true), markup, 0)
		if e != nil {
			return e
		}
		if e = s.Store.VerificationPrompt(ctx, v.Token, id); e != nil {
			return e
		}
	}
	return s.State.Put(ctx, "verify:"+v.Token, v, time.Until(v.ExpiresAt)+time.Minute)
}

func (s *Service) StartVerification(ctx context.Context, m domain.Message, token string) error {
	if m.Chat.Type != "private" || m.From == nil {
		return nil
	}
	v, e := s.Store.Verification(ctx, token)
	if e == sql.ErrNoRows {
		return s.Say(ctx, m.Chat.ID, "zh_CN", "invalid_verify")
	}
	if e != nil {
		return e
	}
	if v.UserID == m.From.ID && v.Status == "verified" {
		return s.text(ctx, m.Chat.ID, "这次验证已完成，无需重复验证；重新入群请使用新的验证链接。")
	}
	if v.UserID != m.From.ID {
		return s.text(ctx, m.Chat.ID, "这不是你的入群验证，无需操作。只有该验证对应的新成员可以完成验证。")
	}
	if v.Status == "cancelled" || v.Status == "releasing" {
		return s.text(ctx, m.Chat.ID, "这次验证已取消，无需再答题；系统会自动解除本次验证造成的禁言。")
	}
	if v.Status != "pending" || time.Now().After(v.ExpiresAt) {
		return s.Say(ctx, m.Chat.ID, "zh_CN", "invalid_verify")
	}
	if ok, e := s.Store.GroupAuthorized(ctx, v.ChatID); e != nil {
		return e
	} else if !ok {
		return s.text(ctx, m.Chat.ID, "该群授权已暂停，验证已停止。")
	}
	member, e := s.Bot.Member(ctx, v.ChatID, v.UserID)
	if e != nil {
		return e
	}
	if !member.Present() {
		return s.Say(ctx, m.Chat.ID, "zh_CN", "invalid_verify")
	}
	settings, e := s.Store.Settings(ctx, v.ChatID)
	if e != nil {
		return e
	}
	var markup any
	if v.Type == "button" {
		markup = map[string]any{"inline_keyboard": [][]map[string]string{{{"text": i18n.Text(settings.Language, "robot"), "callback_data": "v:" + token + ":robot"}, {"text": i18n.Text(settings.Language, "human"), "callback_data": "v:" + token + ":human"}, {"text": i18n.Text(settings.Language, "skip"), "callback_data": "v:" + token + ":skip"}}}}
	} else {
		markup = map[string]any{"force_reply": true, "selective": true}
	}
	id, e := s.Bot.Send(ctx, m.Chat.ID, i18n.Text(settings.Language, "answer_hint", v.Question), markup, 0)
	if e != nil {
		return e
	}
	return s.State.Put(ctx, fmt.Sprintf("verify:prompt:%d:%d", m.From.ID, id), token, time.Until(v.ExpiresAt))
}
func (s *Service) VerificationReply(ctx context.Context, m domain.Message) error {
	if m.From == nil || m.Reply == nil || m.Reply.From == nil || m.Reply.From.ID != s.Bot.ID {
		return s.Home(ctx, m)
	}
	var token string
	if e := s.State.Get(ctx, fmt.Sprintf("verify:prompt:%d:%d", m.From.ID, m.Reply.ID), &token); e != nil {
		if !errors.Is(e, redis.Nil) {
			return e
		}
		return s.text(ctx, m.Chat.ID, "这条操作提示已过期或不再有效。设置操作请重新打开对应菜单；入群验证请从群内最新验证链接进入。")
	}
	return s.AnswerVerification(ctx, token, m.From.ID, strings.TrimSpace(m.Text))
}
func (s *Service) AnswerVerification(ctx context.Context, token string, user int64, answer string) error {
	v, e := s.Store.Verification(ctx, token)
	if e == sql.ErrNoRows {
		return s.Say(ctx, user, "zh_CN", "invalid_verify")
	}
	if e != nil {
		return e
	}
	if v.UserID != user {
		return s.text(ctx, user, "这不是你的入群验证，不能替他人提交答案。")
	}
	unlock, e := s.State.Lock(ctx, verifyLock(v.ChatID, user), 60*time.Second)
	if e != nil {
		return e
	}
	defer unlock()
	v, e = s.Store.Verification(ctx, token)
	if e != nil {
		return e
	}
	if v.Status == "verified" {
		return s.text(ctx, user, "你已通过这次验证，无需重复提交。")
	}
	if v.Status == "cancelled" || v.Status == "releasing" {
		return s.text(ctx, user, "这次验证已取消，无需再答题；系统会自动解除本次验证造成的禁言。")
	}
	if v.Status == "completing" {
		return s.finishVerification(ctx, v)
	}
	v, e = s.Store.Answer(ctx, token, user, answer)
	if errors.Is(e, store.ErrVerification) {
		return s.Say(ctx, user, "zh_CN", "invalid_verify")
	}
	if e != nil {
		return e
	}
	return s.finishVerification(ctx, v)
}

func verificationTerminal(status string) bool {
	return status == "verified" || status == "expired" || status == "cancelled" || status == "blocked" || status == "left"
}
func (s *Service) finishVerification(ctx context.Context, v store.Verification) error {
	if verificationTerminal(v.Status) {
		settings, e := s.Store.Settings(ctx, v.ChatID)
		if e != nil {
			return e
		}
		return s.finishVerificationNotices(ctx, v, settings)
	}

	m, e := s.Bot.Member(ctx, v.ChatID, v.UserID)
	if e != nil {
		return e
	}
	settings, e := s.Store.Settings(ctx, v.ChatID)
	if e != nil {
		return e
	}
	authorized, e := s.Store.GroupAuthorized(ctx, v.ChatID)
	if e != nil {
		return e
	}
	status := "expired"
	if !authorized || !settings.VerificationEnabled || v.Status == "releasing" {
		if m.Status == "restricted" {
			if e = s.Bot.Restore(ctx, v.ChatID, v.UserID); e != nil {
				return e
			}
		}
		if v.KickStarted && v.FailAction == "kick" {
			if e = s.Bot.Unban(ctx, v.ChatID, v.UserID); e != nil {
				return e
			}
		}
		status = "cancelled"
	} else if v.Status == "completing" {
		if !m.Present() {
			status = "left"
		} else {
			kind, e := s.Store.ListStatus(ctx, v.ChatID, v.UserID, m.User.Username)
			if e != nil {
				return e
			}
			if kind == "black" && !m.Admin() && !s.IsSuperAdmin(v.UserID) {
				if e = s.Bot.Ban(ctx, v.ChatID, v.UserID); e != nil {
					return e
				}
				status = "blocked"
			} else {
				if !m.Admin() {
					if e = s.Bot.Restore(ctx, v.ChatID, v.UserID); e != nil {
						return e
					}
				}
				status = "verified"
			}
		}
	} else if v.Status == "expiring" {
		protected, e := s.Protected(ctx, v.ChatID, m.User)
		if e != nil {
			return e
		}
		if protected && m.Status == "restricted" {
			if e = s.Bot.Restore(ctx, v.ChatID, v.UserID); e != nil {
				return e
			}
			status = "cancelled"
		}
		if !protected {
			// Always finish the unban half of a kick, including retries after a partial failure.
			if v.FailAction == "kick" {
				if m.Present() {
					if e = s.Store.BeginVerificationKick(ctx, v.Token); e != nil {
						return e
					}
					v.KickStarted = true
					if e = s.Bot.Ban(ctx, v.ChatID, v.UserID); e != nil {
						return e
					}
				}
				if v.KickStarted {
					if e = s.Bot.Unban(ctx, v.ChatID, v.UserID); e != nil {
						return e
					}
				}
			} else if v.FailAction == "ban" && m.Present() {
				if e = s.Bot.Ban(ctx, v.ChatID, v.UserID); e != nil {
					return e
				}
			}
		}
	} else {
		return nil
	}
	// Keep completing recoverable until the group welcome has been delivered.
	if status == "verified" && settings.WelcomeEnabled {
		var chat domain.Chat
		if e = s.Bot.Call(ctx, "getChat", map[string]any{"chat_id": v.ChatID}, &chat); e != nil {
			return e
		}
		chat.ID = v.ChatID
		if e = s.welcomeLocked(ctx, chat, m.User, settings); e != nil {
			return e
		}
	}
	if e = s.Store.FinishVerification(ctx, v, status); e != nil {
		return e
	}
	if e = s.State.R.Del(ctx, "verify:"+v.Token).Err(); e != nil {
		slog.Warn("verification cache cleanup failed", "error", e)
	}
	v.Status = status
	return s.finishVerificationNotices(ctx, v, settings)
}
func (s *Service) finishVerificationNotices(ctx context.Context, v store.Verification, settings domain.Settings) error {
	done, e := s.Store.VerificationNoticesDone(ctx, v.Token)
	if e != nil || done {
		return e
	}
	if v.PromptID > 0 {
		if e := s.Bot.Delete(ctx, v.ChatID, v.PromptID); e != nil {
			return e
		}
	}
	if v.Status == "verified" {
		if e := s.Say(ctx, v.UserID, settings.Language, "verified"); e != nil {
			return e
		}
	}
	if e := s.Store.FinishVerificationNotices(ctx, v.Token); e != nil {
		return e
	}
	s.LogChannel(ctx, v.ChatID, fmt.Sprintf("验证记录：用户 %d，状态 %s", v.UserID, v.Status))
	return nil
}

func (s *Service) SweepVerification(ctx context.Context) error {
	vs, e := s.Store.DueVerifications(ctx)
	if e != nil {
		return e
	}
	for _, v := range vs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c, cancel := context.WithTimeout(ctx, 40*time.Second)
		unlock, e := s.State.Lock(c, verifyLock(v.ChatID, v.UserID), 60*time.Second)
		if e == nil {
			current, readErr := s.Store.Verification(c, v.Token)
			if readErr != nil {
				e = readErr
			} else if current.Status == "completing" || current.Status == "expiring" || current.Status == "releasing" || verificationTerminal(current.Status) {
				e = s.finishVerification(c, current)
			}
			unlock()
		}
		cancel()
		if e != nil {
			slog.Error("verification recovery failed", "chat_id", v.ChatID, "user_id", v.UserID, "error", e)
			c, done := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			if retryErr := s.Store.VerificationRetry(c, v.Token, e); retryErr != nil {
				slog.Error("verification retry scheduling failed", "error", retryErr)
			}
			done()
		}
	}
	return nil
}
