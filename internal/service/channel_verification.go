package service

import (
	"context"
	"fmt"

	"tgguard/internal/domain"
)

// Query Telegram directly: a cached membership must not grant verification.
func (s *Service) verificationChannelMember(ctx context.Context, user int64, settings domain.Settings) (bool, error) {
	if !settings.VerificationChannelEnabled {
		return true, nil
	}
	if err := settings.Validate(); err != nil {
		return false, err
	}
	var chat domain.Chat
	if err := s.Bot.Call(ctx, "getChat", map[string]any{"chat_id": settings.VerificationChannelID}, &chat); err != nil {
		return false, err
	}
	if chat.Type != "channel" {
		return false, fmt.Errorf("verification target is not a channel")
	}
	bot, err := s.Bot.Member(ctx, settings.VerificationChannelID, s.Bot.ID)
	if err != nil {
		return false, err
	}
	if !bot.Admin() {
		return false, fmt.Errorf("bot must be a channel administrator")
	}
	member, err := s.Bot.Member(ctx, settings.VerificationChannelID, user)
	return err == nil && member.Present(), err
}

func (s *Service) channelVerificationPrompt(ctx context.Context, user int64, settings domain.Settings) error {
	_, err := s.Bot.Send(ctx, user, "请先加入指定频道，再回答验证题或点击「我是人类」。尚未加入或加入申请待审批时不会通过验证，也不会计为答错；验证时限保持不变。", map[string]any{"inline_keyboard": [][]map[string]string{{{"text": "加入频道", "url": settings.VerificationChannelURL}}}}, 0)
	return err
}
