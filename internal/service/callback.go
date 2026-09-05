package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"tgguard/internal/domain"
	"tgguard/internal/state"
	"tgguard/internal/store"
)

type ReviewAction struct {
	LogID  int64 `json:"log_id"`
	ChatID int64 `json:"chat_id"`
	UserID int64 `json:"user_id"`
}

func (s *Service) ReviewButtons(ctx context.Context, l store.Log) (any, error) {
	token, e := state.Token()
	if e != nil {
		return nil, e
	}
	if e = s.State.Put(ctx, "review:action:"+token, ReviewAction{LogID: l.ID, ChatID: l.ChatID, UserID: l.UserID}, 15*time.Minute); e != nil {
		return nil, e
	}
	row := []map[string]string{}
	for _, a := range []struct{ label, action string }{{"删除", "delete"}, {"禁言", "mute"}, {"封禁", "ban"}, {"误判", "false"}, {"白名单", "white"}} {
		row = append(row, map[string]string{"text": a.label, "callback_data": "r:" + token + ":" + a.action})
	}
	return map[string]any{"inline_keyboard": [][]map[string]string{row}}, nil
}
func (s *Service) Callback(ctx context.Context, c domain.Callback) error {
	if strings.HasPrefix(c.Data, "menu:") || strings.HasPrefix(c.Data, "gm:") || strings.HasPrefix(c.Data, "gmc:") {
		return s.MenuCallback(ctx, c)
	}
	if c.Message == nil {
		return nil
	}
	parts := strings.Split(c.Data, ":")
	if len(parts) != 3 {
		return nil
	}
	if parts[0] == "v" {
		if c.Message.Chat.Type != "private" || c.Message.Chat.ID != c.From.ID {
			return nil
		}
		if e := s.Bot.AnswerCallback(ctx, c.ID, ""); e != nil {
			slog.Debug("callback acknowledgement failed", "error", e)
		}
		return s.AnswerVerification(ctx, parts[1], c.From.ID, parts[2])
	}
	if parts[0] != "r" {
		return nil
	}
	var target ReviewAction
	if e := s.State.Get(ctx, "review:action:"+parts[1], &target); e != nil || target.ChatID != c.Message.Chat.ID {
		return s.Bot.AnswerCallback(ctx, c.ID, "按钮已过期或不属于此群")
	}
	admin, e := s.Admin(ctx, target.ChatID, c.From.ID)
	if e != nil {
		return e
	}
	if !admin {
		return s.Bot.AnswerCallback(ctx, c.ID, "只有管理员可操作")
	}
	unlock, e := s.State.Lock(ctx, "review:lock:"+parts[1], 60*time.Second)
	if e != nil {
		return e
	}
	defer unlock()
	if n, e := s.State.R.Exists(ctx, "review:used:"+parts[1]).Result(); e != nil {
		return e
	} else if n > 0 {
		return s.Bot.AnswerCallback(ctx, c.ID, "该审核已处理")
	}
	l, e := s.Store.GetLogID(ctx, target.LogID)
	if e != nil {
		return e
	}
	if l.ChatID != target.ChatID || l.UserID != target.UserID {
		return fmt.Errorf("review target mismatch")
	}
	if e = s.Bot.AnswerCallback(ctx, c.ID, ""); e != nil {
		slog.Debug("callback acknowledgement failed", "error", e)
	}
	switch parts[2] {
	case "false":
		e = s.Store.Feedback(ctx, l.ChatID, l.ID, c.From.ID, "管理员标记误判")
	case "white":
		e = s.Store.SaveList(ctx, domain.ListEntry{ChatID: l.ChatID, UserID: l.UserID, Kind: "white", Reason: "人工审核白名单"}, c.From.ID, false)
	case "delete", "mute", "ban":
		settings, se := s.Store.Settings(ctx, l.ChatID)
		if se != nil {
			return se
		}
		l.EventKey = "callback:" + parts[1]
		l.Source = "manual"
		l.Decision = domain.Decision{Action: parts[2], Delete: true, Duration: settings.MuteSeconds, Reason: "administrator_review"}
		e = s.Punish(ctx, l, c.From.ID)
	default:
		return nil
	}
	if e != nil {
		return e
	}
	if e = s.State.R.Set(ctx, "review:used:"+parts[1], "1", 15*time.Minute).Err(); e != nil {
		return e
	}
	return s.Say(ctx, l.ChatID, "zh_CN", "done")
}
