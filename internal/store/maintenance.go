package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Preserve compact inbox IDs for deduplication and punishment rows for lifetime counters.
// Never purge pending/dead work or messages explicitly retained for feedback.
func (s *Store) RetainData(ctx context.Context, days int) error {
	if days <= 0 {
		return nil
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	statements := []string{
		"UPDATE update_inbox SET retention_done=TRUE,payload=JSON_OBJECT('update_id',update_id) WHERE retention_done=FALSE AND status='done' AND created_at<? ORDER BY created_at LIMIT 1000",
		"UPDATE moderation_logs SET retention_done=TRUE,message_text='[原文已按保留策略清理]' WHERE retention_done=FALSE AND created_at<? AND NOT EXISTS(SELECT 1 FROM feedback WHERE feedback.log_id=moderation_logs.id) AND NOT EXISTS(SELECT 1 FROM punishments WHERE punishments.event_key=moderation_logs.event_key AND punishments.status='pending') ORDER BY created_at LIMIT 1000",
		"DELETE FROM ai_usage_logs WHERE created_at<? ORDER BY created_at LIMIT 1000",
	}
	for round := 0; round < 100; round++ {
		busy := false
		for _, q := range statements {
			result, e := s.DB.ExecContext(ctx, q, cutoff)
			if e != nil {
				return e
			}
			n, e := result.RowsAffected()
			if e != nil {
				return e
			}
			busy = busy || n == 1000
		}
		if !busy {
			break
		}
	}
	return nil
}
func (s *Store) QueueHealth(ctx context.Context) (map[string]any, error) {
	var queued, dead, oldest int64
	e := s.DB.QueryRowContext(ctx, "SELECT COALESCE(SUM(status IN ('pending','processing')),0),COALESCE(SUM(status='dead'),0),COALESCE(MAX(IF(status IN ('pending','processing'),TIMESTAMPDIFF(SECOND,created_at,UTC_TIMESTAMP()),0)),0) FROM update_inbox WHERE status IN ('pending','processing','dead')").Scan(&queued, &dead, &oldest)
	return map[string]any{"pending": queued, "dead": dead, "oldest_pending_seconds": oldest}, e
}

func (s *Store) RecoveryHealth(ctx context.Context) (map[string]any, error) {
	var verificationDelay, welcomeDelay, failed int64
	err := s.DB.QueryRowContext(ctx, "SELECT COALESCE(MAX(GREATEST(0,TIMESTAMPDIFF(SECOND,next_attempt_at,UTC_TIMESTAMP()))),0) FROM verification_sessions WHERE status IN ('completing','expiring','releasing') OR notice_done=FALSE AND status IN ('verified','expired','cancelled','blocked','left')").Scan(&verificationDelay)
	if err != nil {
		return nil, err
	}
	err = s.DB.QueryRowContext(ctx, "SELECT (SELECT COALESCE(MAX(GREATEST(0,TIMESTAMPDIFF(SECOND,next_attempt_at,UTC_TIMESTAMP()))),0) FROM welcome_cleanup WHERE done=FALSE),(SELECT COUNT(*) FROM welcome_cleanup WHERE done=TRUE AND last_error<>'')").Scan(&welcomeDelay, &failed)
	if err != nil {
		return nil, err
	}
	var pending, exhausted, oldest int64
	err = s.DB.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(w.attempts>=20),0),COALESCE(MAX(GREATEST(0,TIMESTAMPDIFF(SECOND,w.next_attempt_at,UTC_TIMESTAMP()))),0) FROM punishment_workflows w JOIN punishments p ON p.event_key=w.event_key WHERE p.status='pending'`).Scan(&pending, &exhausted, &oldest)
	return map[string]any{"verification_due_delay_seconds": verificationDelay, "welcome_due_delay_seconds": welcomeDelay, "welcome_failed": failed, "punishment_pending": pending, "punishment_manual_attention": exhausted, "punishment_due_delay_seconds": oldest}, err
}

// Each index check is restart-safe even if a previous startup stopped midway through DDL.
func ensureMaintenanceIndexes(ctx context.Context, c *sql.Conn) error {
	for _, table := range []string{"moderation_logs", "update_inbox"} {
		var count int
		if e := c.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? AND column_name='retention_done'", table).Scan(&count); e != nil {
			return e
		}
		if count == 0 {
			if _, e := c.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN retention_done BOOLEAN NOT NULL DEFAULT FALSE"); e != nil {
				return e
			}
		}
	}
	for _, spec := range []struct{ table, name, columns string }{
		{"moderation_logs", "idx_moderation_created", "created_at"},
		{"punishments", "idx_punishments_created", "created_at"},
		{"ai_usage_logs", "idx_ai_created", "created_at"},
		{"update_inbox", "idx_inbox_retention_v2", "retention_done,status,created_at"},
		{"moderation_logs", "idx_moderation_retention", "retention_done,created_at"},
		{"verification_sessions", "idx_verification_notice_due", "notice_done,status,next_attempt_at"},
		{"welcome_cleanup", "idx_welcome_error", "done,last_error"},
		{"punishments", "idx_punishments_pending", "status,updated_at"},
		{"moderation_logs", "idx_moderation_member", "chat_id,user_id,id"},
		{"verification_sessions", "idx_verification_chat_time", "chat_id,created_at"},
	} {
		var count int
		if e := c.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name=? AND index_name=?", spec.table, spec.name).Scan(&count); e != nil {
			return e
		}
		if count == 0 {
			if _, e := c.ExecContext(ctx, fmt.Sprintf("CREATE INDEX %s ON %s (%s)", spec.name, spec.table, spec.columns)); e != nil {
				return e
			}
		}
	}
	return nil
}
