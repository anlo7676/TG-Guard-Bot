package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

func (s *Store) RepeatedAd(ctx context.Context, chat, user, message int64, fingerprint string) (bool, error) {
	if fingerprint == "" {
		return false, nil
	}
	var found bool
	err := s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM ad_warning_memory WHERE chat_id=? AND user_id=? AND fingerprint=? AND message_id<>?)", chat, user, fingerprint, message).Scan(&found)
	return found, err
}

// Only completed delete+warning effects can establish repeat-ad evidence.
func (s *Store) RememberAdWarning(ctx context.Context, l Log, fingerprint string) error {
	if fingerprint == "" {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `INSERT IGNORE INTO ad_warning_memory(chat_id,user_id,fingerprint,message_id,event_key)
SELECT chat_id,user_id,?,message_id,event_key FROM punishments WHERE event_key=? AND deleted=TRUE AND acted=TRUE AND decision->>'$.action'='warn'
AND NOT EXISTS(SELECT 1 FROM feedback f JOIN moderation_logs m ON m.id=f.log_id WHERE m.chat_id=punishments.chat_id AND m.user_id=punishments.user_id AND m.message_id=punishments.message_id)`, fingerprint, l.EventKey)
	return err
}

func (s *Store) MessageAlreadyPunished(ctx context.Context, l Log) (bool, error) {
	var found bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM punishments WHERE chat_id=? AND user_id=? AND message_id=? AND event_key<>? AND source IN ('automatic','review') AND status IN ('pending','done') AND decision->>'$.delete'='true')`, l.ChatID, l.UserID, l.MessageID, l.EventKey).Scan(&found)
	return found, err
}

func (s *Store) UpdateLogDecision(ctx context.Context, l Log) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE moderation_logs SET decision=? WHERE event_key=?", SQLJSON(l.Decision), l.EventKey)
	return err
}

func (s *Store) ReviewOutcome(ctx context.Context, l Log) (Punishment, error) {
	var p Punishment
	var raw []byte
	err := s.DB.QueryRowContext(ctx, "SELECT status,decision FROM punishments WHERE chat_id=? AND user_id=? AND message_id=? AND source IN ('automatic','review') ORDER BY id DESC LIMIT 1", l.ChatID, l.UserID, l.MessageID).Scan(&p.Status, &raw)
	if err == sql.ErrNoRows {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(raw, &p.Decision)
	return p, err
}
