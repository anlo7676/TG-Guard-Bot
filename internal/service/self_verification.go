package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"math/big"
	"strconv"
	"tgguard/internal/domain"
	"tgguard/internal/store"
	"time"
)

func (s *Service) SelfVerificationMenu(ctx context.Context, m domain.Message) error {
	if m.From == nil || m.Chat.Type != "private" || m.Chat.ID != m.From.ID {
		return nil
	}
	vs, err := s.Store.SelfVerifications(ctx, m.From.ID)
	if err != nil {
		return err
	}
	if len(vs) == 0 {
		return s.text(ctx, m.Chat.ID, "没有可自助恢复的入群验证。此功能仅处理待验证或验证超时后保持禁言的情况；广告处罚、管理员禁言和封禁请联系群管理员。")
	}
	rows := [][]map[string]string{}
	for _, v := range vs {
		rows = append(rows, []map[string]string{{"text": v.Title, "callback_data": "sv:" + v.Token}})
	}
	_, err = s.Bot.Send(ctx, m.Chat.ID, "自助入群验证\n请选择群组。验证通过后恢复发言；原验证已超时的会重新出题。每次最多显示最近 20 个群组。", map[string]any{"inline_keyboard": rows}, 0)
	return err
}

func (s *Service) SelfVerification(ctx context.Context, m domain.Message, token string) error {
	if m.From == nil || m.Chat.Type != "private" || m.Chat.ID != m.From.ID {
		return nil
	}
	v, err := s.Store.Verification(ctx, token)
	if err == sql.ErrNoRows {
		return s.text(ctx, m.Chat.ID, "验证记录已失效，请重新打开自助验证菜单。")
	}
	if err != nil {
		return err
	}
	if v.UserID != m.From.ID {
		return s.text(ctx, m.Chat.ID, "此验证不属于你。请从私聊主菜单打开自助验证。")
	}
	unlock, err := s.State.Lock(ctx, verifyLock(v.ChatID, v.UserID), 60*time.Second)
	if err != nil {
		return err
	}
	defer unlock()
	ok, err := s.Store.CanSelfVerify(ctx, token, m.From.ID)
	if err != nil {
		return err
	}
	if !ok {
		return s.text(ctx, m.Chat.ID, "该验证无法自助恢复，可能已完成、离群或权限已由管理员接管。请联系群管理员。")
	}
	v, err = s.Store.Verification(ctx, token)
	if err != nil {
		return err
	}
	settings, err := s.Store.Settings(ctx, v.ChatID)
	if err != nil {
		return err
	}
	member, err := s.Bot.Member(ctx, v.ChatID, v.UserID)
	if err != nil {
		return err
	}
	kind, err := s.Store.ListStatus(ctx, v.ChatID, v.UserID, member.User.Username)
	if err != nil {
		return err
	}
	if !settings.VerificationEnabled || !member.Present() || member.Status != "restricted" || kind == "black" {
		return s.text(ctx, m.Chat.ID, "当前不符合自助验证条件。请确认仍在群内且仅因入群验证被禁言；其他限制请联系群管理员。")
	}
	if v.Status == "expiring" || (v.Status == "pending" && time.Now().After(v.ExpiresAt)) {
		return s.text(ctx, m.Chat.ID, "上次验证正在结束，请稍后重新点击自助验证。")
	}
	if v.Status == "expired" {
		allowed, err := s.State.Limit(ctx, fmt.Sprintf("verify:restart:%d:%d", v.ChatID, v.UserID), 3, time.Hour)
		if err != nil {
			return err
		}
		if !allowed {
			return s.text(ctx, m.Chat.ID, "重新验证次数过多，请一小时后再试，或联系群管理员。")
		}
		v.Type = settings.VerificationType
		v.Question = "请选择「我是人类」完成验证。"
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
		v.ExpiresAt = time.Now().UTC().Add(time.Duration(settings.VerificationTimeout) * time.Second)
		if err = s.Store.ReopenVerification(ctx, v); err != nil {
			return err
		}
	}
	return s.StartVerification(ctx, m, token)
}

// A Telegram administrator changing restrictions takes ownership away from verification.
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
