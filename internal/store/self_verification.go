package store

import (
	"context"
	"tgguard/internal/domain"
)

func (s *Store) SelfUnmuteDone(ctx context.Context, key string) (bool, error) {
	var done bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM punishments WHERE event_key=? AND status='done' AND acted=TRUE)`, key).Scan(&done)
	return done, err
}

func (s *Store) CancelExternallyManagedVerification(ctx context.Context, chat, user, date int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE verification_sessions SET status='cancelled' WHERE chat_id=? AND user_id=? AND created_at<FROM_UNIXTIME(?+1) AND status IN ('pending','expiring','completing','expired')`, chat, user, date)
	return err
}

func (s *Store) SelfVerificationGroups(ctx context.Context, before int64) ([]domain.Chat, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT chat_id,title FROM bot_groups WHERE active=TRUE AND authorization='approved' AND chat_id<? ORDER BY chat_id DESC LIMIT 20`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Chat{}
	for rows.Next() {
		var g domain.Chat
		g.Type = "supergroup"
		if err := rows.Scan(&g.ID, &g.Title); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) CreateSelfVerification(ctx context.Context, v Verification) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE verification_sessions SET status='cancelled' WHERE chat_id=? AND user_id=? AND status IN ('pending','expiring','completing','releasing')`, v.ChatID, v.UserID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO verification_sessions(token,chat_id,user_id,challenge_type,question,answer_hash,fail_action,expires_at) VALUES(?,?,?,?,?,?,?,?)`, v.Token, v.ChatID, v.UserID, v.Type, v.Question, v.AnswerHash, "mute", v.ExpiresAt)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SelfVerificationBusy(ctx context.Context, v Verification) (bool, error) {
	var busy bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM punishments WHERE chat_id=? AND user_id=? AND status='pending' AND event_key<>? AND `+takeoverActions+`)`, v.ChatID, v.UserID, "self-unmute:"+v.Token).Scan(&busy)
	return busy, err
}
