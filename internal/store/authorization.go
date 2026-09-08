package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

var ErrUnauthorized = errors.New("群组未获授权，请联系机器人管理员审批")

func (s *Store) GroupAuthorized(ctx context.Context, chat int64) (bool, error) {
	var ok bool
	e := s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM bot_groups WHERE chat_id=? AND active=TRUE AND authorization='approved')", chat).Scan(&ok)
	return ok, e
}
func (s *Store) RequireAuthorized(ctx context.Context, chat int64) error {
	ok, e := s.GroupAuthorized(ctx, chat)
	if e != nil {
		return e
	}
	if !ok {
		return ErrUnauthorized
	}
	return nil
}
func (s *Store) AuthorizeGroup(ctx context.Context, chat, actor int64, status, reason string) error {
	return s.changeAuthorization(ctx, chat, actor, status, reason, false)
}
func (s *Store) changeAuthorization(ctx context.Context, chat, actor int64, status, reason string, leaving bool) error {
	if status != "approved" && status != "rejected" && status != "revoked" {
		return errors.New("无效的审批状态")
	}
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) > 500 {
		return errors.New("原因最多500字")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var old string
	var active bool
	if e = tx.QueryRowContext(ctx, "SELECT authorization,active FROM bot_groups WHERE chat_id=? FOR UPDATE", chat).Scan(&old, &active); e != nil {
		if leaving && e == sql.ErrNoRows {
			return nil
		}
		return e
	}
	if status == "approved" && !active {
		return errors.New("机器人已离开该群，不能批准")
	}
	if _, e = tx.ExecContext(ctx, "UPDATE bot_groups SET authorization=?,authorization_reason=?,active=IF(?,FALSE,active) WHERE chat_id=?", status, reason, leaving, chat); e != nil {
		return e
	}
	if status != "approved" {
		if _, e = tx.ExecContext(ctx, "UPDATE verification_sessions SET status='releasing',next_attempt_at=UTC_TIMESTAMP(6) WHERE chat_id=? AND status IN ('pending','completing','expiring')", chat); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "UPDATE punishments SET status='skipped' WHERE chat_id=? AND status='pending'", chat); e != nil {
			return e
		}
	}
	if e = audit(ctx, tx, chat, actor, "group.authorization", map[string]any{"status": old}, map[string]any{"status": status, "reason": reason}); e != nil {
		return e
	}
	return tx.Commit()
}

func (s *Store) GroupAuthorizationStatus(ctx context.Context, chat int64) (string, error) {
	var status string
	err := s.DB.QueryRowContext(ctx, "SELECT authorization FROM bot_groups WHERE chat_id=?", chat).Scan(&status)
	if err == sql.ErrNoRows {
		return "pending", nil
	}
	return status, err
}
