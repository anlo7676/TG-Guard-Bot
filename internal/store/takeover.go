package store

import (
	"context"
	"encoding/json"
)

const takeoverActions = "JSON_UNQUOTE(JSON_EXTRACT(decision,'$.action')) IN ('mute','ban','kick','unmute')"

// Pending punishment is the durable takeover intent. Verification stays intact
// until the Telegram operation succeeds and the acted/cancelled state commits.
func (s *Store) VerificationTakenOver(ctx context.Context, chat, user int64) (bool, error) {
	var found bool
	err := s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM punishments WHERE chat_id=? AND user_id=? AND status='pending' AND "+takeoverActions+")", chat, user).Scan(&found)
	return found, err
}
func (s *Store) CompleteTakeover(ctx context.Context, l Log) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE verification_sessions SET status='cancelled' WHERE chat_id=? AND user_id=? AND status IN ('pending','completing','expiring','releasing')", l.ChatID, l.UserID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE punishments SET acted=TRUE WHERE event_key=? AND status='pending'", l.EventKey); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) FailPunishment(ctx context.Context, key string) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE punishments SET status='failed' WHERE event_key=? AND status='pending'", key)
	return err
}

type Takeover struct {
	Log   Log
	Actor int64
}

func (s *Store) PendingTakeovers(ctx context.Context) ([]Takeover, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT event_key,chat_id,user_id,message_id,decision,source,actor_id FROM punishments p WHERE status='pending' AND "+takeoverActions+" AND EXISTS(SELECT 1 FROM verification_sessions v WHERE v.chat_id=p.chat_id AND v.user_id=p.user_id AND v.status IN ('pending','completing','expiring','releasing')) ORDER BY updated_at LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Takeover{}
	for rows.Next() {
		var t Takeover
		var raw []byte
		if err = rows.Scan(&t.Log.EventKey, &t.Log.ChatID, &t.Log.UserID, &t.Log.MessageID, &raw, &t.Log.Source, &t.Actor); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &t.Log.Decision); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
