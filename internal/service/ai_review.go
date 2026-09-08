package service

import (
	"context"
	"log/slog"
	"tgguard/internal/domain"
)

func (s *Service) reviewAI(ctx context.Context, n domain.Normalized, risk *domain.Risk, source, event string) *domain.AIResult {
	result, err := s.AI.Review(ctx, n, *risk)
	if err != nil {
		slog.Warn("AI review unavailable; local decision only", "source", source, "chat_id", n.ChatID, "message_id", n.MessageID, "event_key", event, "error", err)
		risk.Matches = append(risk.Matches, domain.Match{Rule: "ai_unavailable", Reason: "AI 审核未完成，已按本地规则处理"})
		return nil
	}
	risk.Matches = append(risk.Matches, domain.Match{Rule: "ai_reviewed", Reason: "AI 审核完成，最终处罚由群策略决定"})
	return &result
}
