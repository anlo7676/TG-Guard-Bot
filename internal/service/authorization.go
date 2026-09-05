package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"tgguard/internal/domain"
	"time"
)

func (s *Service) AuthorizationNotice(ctx context.Context, chat int64) error {
	ok, e := s.State.R.SetNX(ctx, fmt.Sprintf("authorization:notice:%d", chat), 1, time.Minute).Result()
	if e != nil {
		return e
	}
	if !ok {
		return nil
	}
	return s.text(ctx, chat, fmt.Sprintf("本群尚未获准使用机器人（待审批、已拒绝或已撤销）。群组 ID：%d。请联系部署者在网页后台「群组列表」审批；群主身份不能代替机器人授权。", chat))
}
func (s *Service) authorizationCommand(ctx context.Context, m domain.Message, command, arg string) error {
	if !s.IsSuperAdmin(m.From.ID) {
		return s.text(ctx, m.Chat.ID, "只有机器人超级管理员可以审批群组。部署者可在网页后台操作。")
	}
	parts := strings.Fields(arg)
	if len(parts) == 0 {
		return s.text(ctx, m.Chat.ID, "用法：/"+command+" -100群组ID [原因]")
	}
	chat, e := strconv.ParseInt(parts[0], 10, 64)
	if e != nil || chat >= 0 {
		return s.text(ctx, m.Chat.ID, "请输入有效的负数群组 ID。")
	}
	status := map[string]string{"approve": "approved", "reject": "rejected", "revoke": "revoked"}[command]
	if e = s.Store.AuthorizeGroup(ctx, chat, m.From.ID, status, strings.Join(parts[1:], " ")); e != nil {
		return s.text(ctx, m.Chat.ID, "审批失败："+e.Error())
	}
	return s.text(ctx, m.Chat.ID, fmt.Sprintf("群组 %d 审批状态：%s", chat, status))
}
