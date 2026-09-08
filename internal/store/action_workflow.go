package store

import (
	"context"
	"time"
)

// Commit the recovery intent before making a non-transactional Telegram call.
func (s *Store) StartPermission(ctx context.Context, l Log, until time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var allowed bool
	if err = tx.QueryRowContext(ctx, "SELECT active AND authorization='approved' FROM bot_groups WHERE chat_id=? FOR UPDATE", l.ChatID).Scan(&allowed); err != nil {
		return err
	}
	var current bool
	if err = tx.QueryRowContext(ctx, "SELECT p.status='pending' AND w.authority_version=COALESCE((SELECT version FROM authorization_epochs WHERE chat_id=p.chat_id),0) FROM punishments p JOIN punishment_workflows w ON w.event_key=p.event_key WHERE p.event_key=? FOR UPDATE", l.EventKey).Scan(&current); err != nil {
		return err
	}
	if !allowed || !current {
		return ErrAuthorityChanged
	}
	var deadline any
	if !until.IsZero() {
		deadline = until
	}
	_, err = tx.ExecContext(ctx, "UPDATE punishment_workflows SET started=TRUE,release_needed=IF(? IS NULL,release_needed,TRUE),release_at=COALESCE(?,release_at) WHERE event_key=?", deadline, deadline, l.EventKey)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) KickBanned(ctx context.Context, key string) (bool, error) {
	var yes bool
	err := s.DB.QueryRowContext(ctx, "SELECT kick_banned FROM punishment_workflows WHERE event_key=?", key).Scan(&yes)
	return yes, err
}
func (s *Store) MarkKickBanned(ctx context.Context, key string) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE punishment_workflows SET kick_banned=TRUE WHERE event_key=?", key)
	return err
}
func (s *Store) RetryPunishment(ctx context.Context, key string) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE punishment_workflows SET attempts=attempts+1,next_attempt_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL LEAST(300,5*(attempts+1)) SECOND) WHERE event_key=?", key)
	return err
}

// Only use for a definite rejection before any effect or earlier uncertain attempt.
func (s *Store) RejectPunishment(ctx context.Context, key string) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE punishments p JOIN punishment_workflows w ON w.event_key=p.event_key SET p.status='failed',w.release_needed=FALSE,w.started=FALSE WHERE p.event_key=? AND p.status='pending'", key)
	return err
}

type PermissionRelease struct {
	EventKey       string
	ChatID, UserID int64
	Action         string
	CreatedAt      time.Time
}

func (s *Store) NewerPermissionAction(ctx context.Context, chat, user int64, after time.Time) (bool, error) {
	var yes bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM punishments p JOIN punishment_workflows w ON w.event_key=p.event_key WHERE p.chat_id=? AND p.user_id=? AND p.created_at>? AND w.started=TRUE AND p.status<>'failed' AND JSON_UNQUOTE(JSON_EXTRACT(p.decision,'$.action')) IN ('mute','ban','kick','unmute','unban')) OR EXISTS(SELECT 1 FROM verification_sessions WHERE chat_id=? AND user_id=? AND created_at>? AND status IN ('pending','completing','expiring','releasing'))`, chat, user, after, chat, user, after).Scan(&yes)
	return yes, err
}

func (s *Store) DuePermissionReleases(ctx context.Context) ([]PermissionRelease, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT event_key,chat_id,user_id,action,created_at FROM (
 SELECT p.event_key,p.chat_id,p.user_id,JSON_UNQUOTE(JSON_EXTRACT(p.decision,'$.action')) action,p.created_at,
 ROW_NUMBER() OVER(PARTITION BY p.chat_id ORDER BY w.release_at) pos
 FROM punishment_workflows w JOIN punishments p ON p.event_key=w.event_key
 WHERE w.release_needed=TRUE AND w.release_at<=UTC_TIMESTAMP(6)) q WHERE pos<=2 LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PermissionRelease{}
	for rows.Next() {
		var r PermissionRelease
		if err = rows.Scan(&r.EventKey, &r.ChatID, &r.UserID, &r.Action, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) FinishPermissionRelease(ctx context.Context, key string, cause error, released bool) error {
	if cause != nil {
		_, err := s.DB.ExecContext(ctx, "UPDATE punishment_workflows SET release_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 30 SECOND) WHERE event_key=?", key)
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if released {
		if _, err = tx.ExecContext(ctx, `UPDATE verification_sessions v JOIN punishments p ON p.chat_id=v.chat_id AND p.user_id=v.user_id SET v.status='cancelled' WHERE p.event_key=? AND v.created_at<=p.created_at AND v.status IN ('pending','completing','expiring','releasing')`, key); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE punishment_workflows SET release_needed=FALSE WHERE event_key=?", key); err != nil {
		return err
	}
	return tx.Commit()
}
