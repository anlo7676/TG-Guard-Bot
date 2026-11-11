package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"tgguard/internal/telegram"
)

type menuRequestKey struct{}

// Nested menu routes share one admission and deadline. Production always has State.
func (s *Service) beginMenu(ctx context.Context, user int64) (context.Context, context.CancelFunc, bool, error) {
	if ctx.Value(menuRequestKey{}) != nil {
		return ctx, func() {}, true, nil
	}
	if s.State != nil {
		ok, err := s.State.Limit(ctx, fmt.Sprintf("menu:request:%d", user), 6, 10*time.Second)
		if err != nil || !ok {
			return ctx, func() {}, false, err
		}
	}
	ctx = context.WithValue(telegram.MenuContext(ctx), menuRequestKey{}, true)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	return ctx, cancel, true, nil
}

// Ordinary throttling is terminal, not a failed inbox job to replay five times.
func finishMenu(err *error) {
	if errors.Is(*err, telegram.ErrMenuLimited) || errors.Is(*err, context.DeadlineExceeded) || errors.Is(*err, context.Canceled) {
		*err = nil
	}
}

func (s *Service) acknowledgeMenu(ctx context.Context, id string) {
	ctx, cancel := context.WithTimeout(telegram.MenuContext(ctx), time.Second)
	defer cancel()
	_ = s.Bot.AnswerCallback(ctx, id, "")
}

func (s *Service) menuText(ctx context.Context, user int64, text string) (result error) {
	ctx, cancel, admitted, err := s.beginMenu(ctx, user)
	defer cancel()
	defer finishMenu(&result)
	if err != nil || !admitted {
		return err
	}
	return s.text(ctx, user, text)
}
