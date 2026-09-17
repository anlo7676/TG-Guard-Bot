package service

import (
	"context"
	"fmt"
)

// releaseKick verifies the observable Telegram state before the durable
// workflow is allowed to finish. Telegram may acknowledge an immediate
// ban/unban pair before the ban is fully visible, so a remaining kicked state
// must stay recoverable and be retried.
func (s *Service) releaseKick(ctx context.Context, chat, user int64) error {
	executor := s.executor()
	if err := executor.Unban(ctx, chat, user); err != nil {
		return err
	}
	member, err := executor.Member(ctx, chat, user)
	if err != nil {
		return err
	}
	if member.Status == "kicked" {
		return fmt.Errorf("kick release not confirmed: user remains kicked")
	}
	return nil
}
