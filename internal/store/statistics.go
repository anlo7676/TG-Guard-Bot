package store

import "context"

type GroupStatistics struct{ KnownMembers, ReviewedMessages, Punishments int64 }

func (s *Store) GroupStatistics(ctx context.Context, chat int64) (GroupStatistics, error) {
	var v GroupStatistics
	err := s.DB.QueryRowContext(ctx, viewSQL[ViewGroupStats], chat, chat, chat).Scan(&v.KnownMembers, &v.ReviewedMessages, &v.Punishments)
	return v, err
}
