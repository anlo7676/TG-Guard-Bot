package store

import (
	"context"
)

func (s *Store) WelcomeSent(ctx context.Context, chat, user int64) (bool, error) {
	var sent bool
	e := s.DB.QueryRowContext(ctx, "SELECT COALESCE(welcomed_at>=joined_at,FALSE) FROM group_members WHERE chat_id=? AND user_id=?", chat, user).Scan(&sent)
	return sent, e
}
func (s *Store) MarkWelcomed(ctx context.Context, chat, user int64) error {
	_, e := s.DB.ExecContext(ctx, "UPDATE group_members SET welcomed_at=UTC_TIMESTAMP(6) WHERE chat_id=? AND user_id=?", chat, user)
	return e
}
