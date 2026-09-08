package service

import (
	"context"
	"errors"
	"testing"
	"tgguard/internal/domain"
)

type unavailableAI struct{}

func (unavailableAI) Review(context.Context, domain.Normalized, domain.Risk) (domain.AIResult, error) {
	return domain.AIResult{}, errors.New("test outage")
}
func TestAIFallbackLeavesAuditableEvidence(t *testing.T) {
	s := &Service{AI: unavailableAI{}}
	r := domain.Risk{Score: 60}
	if s.reviewAI(context.Background(), domain.Normalized{}, &r, "channel", "test") != nil || len(r.Matches) != 1 || r.Matches[0].Rule != "ai_unavailable" || r.Score != 60 {
		t.Fatal(r)
	}
}
