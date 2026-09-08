package service

import (
	"context"
	"fmt"
	"strconv"
	"tgguard/internal/domain"
	"tgguard/internal/rules"
)

// Consumer-owned contracts keep policy evaluation independent of SQL, Redis and menus.
type ViolationReader interface {
	ViolationCount(context.Context, int64, int64, ...string) (int, error)
}
type SpamCounter interface {
	Spam(context.Context, int64, int64, int64, string, int) (int, int, error)
}
type moderationEngine struct {
	violations ViolationReader
	spam       SpamCounter
	review     func(context.Context, domain.Normalized, *domain.Risk) *domain.AIResult
}

func (e moderationEngine) decide(ctx context.Context, n domain.Normalized, s domain.Settings, update int64, protected bool) (domain.Risk, *domain.AIResult, domain.Decision, error) {
	risk := rules.Evaluate(n, s)
	var result *domain.AIResult
	if s.SpamEnabled {
		text := n.Text
		if text == "" {
			text = strconv.FormatInt(update, 10)
		}
		rate, dup, err := e.spam.Spam(ctx, n.ChatID, n.UserID, update, text, s.RateWindow)
		if err != nil {
			return risk, nil, domain.Decision{}, err
		}
		if rate > s.RateLimit || dup >= s.DuplicateLimit {
			risk.Spam = true
			risk.Score = 100
			risk.Matches = append(risk.Matches, domain.Match{Rule: "spam", Score: 100, Reason: fmt.Sprintf("rate=%d duplicate=%d", rate, dup)})
		}
	}
	if s.AIEnabled && e.review != nil && risk.Score >= s.AIThreshold {
		result = e.review(ctx, n, &risk)
	}
	count, err := e.violations.ViolationCount(ctx, n.ChatID, n.UserID)
	if err != nil {
		return risk, result, domain.Decision{}, err
	}
	return risk, result, rules.Decide(risk, result, s, count, protected), nil
}
