package store

import (
	"context"
)

func (s *Store) WelcomeSent(ctx context.Context, chat, user int64) (bool, error) {
	var sent bool
	e := s.DB.QueryRowContext(ctx, "SELECT COALESCE(welcomed_at>=joined_at,FALSE) FROM group_members WHERE chat_id=? AND user_id=?", chat, user).Scan(&sent)
	return sent, e
}
func (s *Store) MarkWelcomed(ctx context.Context, chat, user, message int64) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "INSERT IGNORE INTO welcome_cleanup(chat_id,message_id,delete_at,next_attempt_at) VALUES(?,?,DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 295 SECOND),DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 295 SECOND))", chat, message); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE group_members SET welcomed_at=UTC_TIMESTAMP(6) WHERE chat_id=? AND user_id=?", chat, user); e != nil {
		return e
	}
	return tx.Commit()
}

type WelcomeCleanup struct{ ChatID, MessageID int64 }

func (s *Store) DueWelcomeCleanup(ctx context.Context) ([]WelcomeCleanup, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT chat_id,message_id FROM welcome_cleanup WHERE done=FALSE AND next_attempt_at<=UTC_TIMESTAMP(6) ORDER BY next_attempt_at LIMIT 100")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []WelcomeCleanup{}
	for rows.Next() {
		var item WelcomeCleanup
		if e = rows.Scan(&item.ChatID, &item.MessageID); e != nil {
			return nil, e
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (s *Store) FinishWelcomeCleanup(ctx context.Context, item WelcomeCleanup, cause error) error {
	if cause == nil {
		_, e := s.DB.ExecContext(ctx, "UPDATE welcome_cleanup SET done=TRUE,last_error='' WHERE chat_id=? AND message_id=?", item.ChatID, item.MessageID)
		return e
	}
	_, e := s.DB.ExecContext(ctx, "UPDATE welcome_cleanup SET attempts=attempts+1,last_error=?,next_attempt_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL LEAST(300,5*attempts) SECOND) WHERE chat_id=? AND message_id=?", clip(cause.Error(), 500), item.ChatID, item.MessageID)
	return e
}
