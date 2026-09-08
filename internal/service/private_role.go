package service

import (
	"context"
	"errors"
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
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	before := int64(0)
	for {
		groups, err := s.Store.MenuGroups(check, before)
		if err != nil {
			return false, err
		}
		for _, g := range groups {
			allowed, err := s.Admin(check, g.ID, user)
			if err != nil {
				var te *telegram.APIError
				if errors.As(err, &te) && (te.Code == 400 || te.Code == 403) {
					before = g.ID
					continue
				}
				return false, err
			}
			if allowed {
				return true, nil
			}
			before = g.ID
		}
		if len(groups) < 11 {
			return false, nil
		}
	}
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
