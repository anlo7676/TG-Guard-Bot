package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

const takeoverActions = "JSON_UNQUOTE(JSON_EXTRACT(decision,'$.action')) IN ('mute','ban','kick','unmute')"

var ErrAuthorityChanged = errors.New("群授权已变更，权限操作进入补偿恢复")

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
	var authorized bool
	if err = tx.QueryRowContext(ctx, "SELECT active AND authorization='approved' FROM bot_groups WHERE chat_id=? FOR UPDATE", l.ChatID).Scan(&authorized); err != nil {
		return err
	}
	var valid bool
	if err = tx.QueryRowContext(ctx, "SELECT p.status='pending' AND w.authority_version=COALESCE((SELECT version FROM authorization_epochs WHERE chat_id=p.chat_id),0) FROM punishments p JOIN punishment_workflows w ON w.event_key=p.event_key WHERE p.event_key=? FOR UPDATE", l.EventKey).Scan(&valid); err != nil {
		return err
	}
	if !authorized || !valid {
		return ErrAuthorityChanged
	}
	if l.Decision.Action != "unban" {
		query := "UPDATE verification_sessions SET status='cancelled' WHERE chat_id=? AND user_id=? AND status IN ('pending','completing','expiring','releasing')"
		args := []any{l.ChatID, l.UserID}
		if l.Decision.Reason == "self_verification" {
			query += " AND token<>?"
			args = append(args, strings.TrimPrefix(l.EventKey, "self-unmute:"))
		}
		if _, err = tx.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE punishments SET acted=TRUE WHERE event_key=? AND status='pending'", l.EventKey); err != nil {
		return err
	}
	if l.Source == "manual" {
		if _, err = tx.ExecContext(ctx, `UPDATE punishments older JOIN punishments current ON current.event_key=? SET older.status='skipped' WHERE older.chat_id=current.chat_id AND older.user_id=current.user_id AND older.id<current.id AND older.status='pending'`, l.EventKey); err != nil {
			return err
		}
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
	rows, err := s.DB.QueryContext(ctx, `SELECT event_key,chat_id,user_id,message_id,decision,source,actor_id FROM (
 SELECT p.*,w.next_attempt_at,ROW_NUMBER() OVER(PARTITION BY p.chat_id ORDER BY w.next_attempt_at,p.id) pos
 FROM punishments p JOIN punishment_workflows w ON w.event_key=p.event_key
 WHERE p.status='pending' AND w.attempts<20 AND w.next_attempt_at<=UTC_TIMESTAMP(6)) q WHERE pos<=2 ORDER BY next_attempt_at LIMIT 100`)
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
