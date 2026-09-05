package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"tgguard/internal/domain"
)

func (s *Store) Enqueue(ctx context.Context, u domain.Update) error {
	_, e := s.DB.ExecContext(ctx, "INSERT IGNORE INTO update_inbox(update_id,partition_id,payload) VALUES(?,?,?)", u.ID, u.Partition(), JSON(u))
	return e
}
func (s *Store) Offset(ctx context.Context) (int64, error) {
	var n int64
	e := s.DB.QueryRowContext(ctx, "SELECT value FROM bot_state WHERE name='poll_offset'").Scan(&n)
	if e == sql.ErrNoRows {
		return 0, nil
	}
	return n, e
}
func (s *Store) SaveOffset(ctx context.Context, n int64) error {
	_, e := s.DB.ExecContext(ctx, "INSERT INTO bot_state(name,value) VALUES('poll_offset',?) ON DUPLICATE KEY UPDATE value=VALUES(value)", n)
	return e
}

type Job struct {
	Update   domain.Update
	Attempts int
}

func (s *Store) Claim(ctx context.Context) (Job, error) {
	var j Job
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return j, e
	}
	defer tx.Rollback()
	var b []byte
	// Keep events for the same chat ordered; independent chats can be processed concurrently.
	e = tx.QueryRowContext(ctx, `SELECT a.payload,a.attempts FROM update_inbox a
 WHERE ((a.status='pending' AND a.available_at<=UTC_TIMESTAMP(6)) OR (a.status='processing' AND a.lease_until<UTC_TIMESTAMP(6)))
 AND NOT EXISTS (SELECT 1 FROM update_inbox b WHERE b.partition_id=a.partition_id AND b.update_id<a.update_id AND b.status IN ('pending','processing'))
 ORDER BY a.update_id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&b, &j.Attempts)
	if e != nil {
		return j, e
	}
	if e = json.Unmarshal(b, &j.Update); e != nil {
		return j, e
	}
	j.Attempts++
	_, e = tx.ExecContext(ctx, "UPDATE update_inbox SET status='processing',attempts=?,lease_until=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 90 SECOND) WHERE update_id=?", j.Attempts, j.Update.ID)
	if e != nil {
		return j, e
	}
	return j, tx.Commit()
}
func (s *Store) Finish(ctx context.Context, j Job, jobErr error) error {
	status := "done"
	msg := ""
	delay := time.Now().UTC()
	if jobErr != nil {
		status = "pending"
		msg = clip(jobErr.Error(), 500)
		delay = delay.Add(time.Duration(j.Attempts*j.Attempts) * time.Second)
		if j.Attempts >= 5 {
			status = "dead"
		}
	}
	_, e := s.DB.ExecContext(ctx, "UPDATE update_inbox SET status=?,last_error=?,available_at=?,lease_until=NULL WHERE update_id=? AND attempts=? AND status='processing'", status, msg, delay, j.Update.ID, j.Attempts)
	return e
}

func (s *Store) RetryDead(ctx context.Context, update, actor int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var chat int64
	var attempts int
	if err = tx.QueryRowContext(ctx, "SELECT partition_id,attempts FROM update_inbox WHERE update_id=? AND status='dead' FOR UPDATE", update).Scan(&chat, &attempts); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE update_inbox SET status='pending',attempts=0,last_error='',available_at=UTC_TIMESTAMP(6),lease_until=NULL WHERE update_id=? AND status='dead'", update); err != nil {
		return err
	}
	if err = audit(ctx, tx, chat, actor, "queue.retry", map[string]any{"update_id": update, "status": "dead", "attempts": attempts}, map[string]any{"update_id": update, "status": "pending"}); err != nil {
		return err
	}
	return tx.Commit()
}
