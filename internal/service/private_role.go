package service

import (
	"context"
	"errors"
	"fmt"
	"tgguard/internal/telegram"
	"time"
)

func (s *Service) canManageGroups(ctx context.Context, user int64) (bool, error) {
	if s.IsSuperAdmin(user) {
		return true, nil
	}
	if s.Store == nil {
		return false, nil
	}
	key := fmt.Sprintf("menu:role:%d", user)
	if s.State != nil {
		var cached bool
		if err := s.State.Get(ctx, key, &cached); err == nil {
			return cached, nil
		}
	}
	remember := func(value bool) (bool, error) {
		if s.State != nil {
			if err := s.State.Put(ctx, key, value, 30*time.Second); err != nil {
				return false, err
			}
		}
		return value, nil
	}
	check, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	groups, err := s.Store.MenuGroups(check, 0)
	if err != nil {
		return false, err
	}
	for i, g := range groups {
		if i == 10 {
			return false, errors.New("menu role discovery incomplete; use paginated groups")
		}
		allowed, err := s.Admin(check, g.ID, user)
		if err != nil {
			var te *telegram.APIError
			if errors.As(err, &te) && (te.Code == 400 || te.Code == 403) {
				continue
			}
			return false, err
		}
		if allowed {
			return remember(true)
		}
	}
	return remember(false)
}

func privateHomeRows(manage, super bool, username string) [][]map[string]string {
	rows := [][]map[string]string{
		{{"text": "✅ 自助验证 / 解除禁言", "callback_data": "menu:verify"}},
		{{"text": "👤 我的身份", "callback_data": "menu:profile"}, {"text": "📖 使用帮助", "callback_data": "menu:help"}},
	}
	if manage {
		rows = append(rows, []map[string]string{{"text": "📋 我的群组 / 群设置", "callback_data": "menu:groups"}}, []map[string]string{{"text": "➕ 添加到群组", "url": "https://t.me/" + username + "?startgroup=true"}})
	}
	if super {
		rows = append(rows, []map[string]string{{"text": "管理后台", "callback_data": "menu:panel"}})
	}
	return rows
}
