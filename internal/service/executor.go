package service

import (
	"context"

	"tgguard/internal/domain"
)

// MemberExecutor is shared by moderation and verification, while each owner
// retains its durable workflow and the common per-member lock.
type MemberExecutor interface {
	Member(context.Context, int64, int64) (domain.Member, error)
	Delete(context.Context, int64, int64) error
	Restrict(context.Context, int64, int64, int) error
	Restore(context.Context, int64, int64) error
	Ban(context.Context, int64, int64) error
	Unban(context.Context, int64, int64) error
	Kick(context.Context, int64, int64) error
}

func (s *Service) executor() MemberExecutor {
	if s.Executor != nil {
		return s.Executor
	}
	return s.Bot
}
