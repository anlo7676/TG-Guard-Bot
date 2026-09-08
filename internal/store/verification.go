package store

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

var ErrVerification = errors.New("verification invalid, expired, wrong answer or wrong user")

type Verification struct {
	Token       string    `json:"token"`
	ChatID      int64     `json:"chat_id"`
	UserID      int64     `json:"user_id"`
	Type        string    `json:"type"`
	Question    string    `json:"question"`
	AnswerHash  string    `json:"-"`
	Status      string    `json:"status"`
	Attempts    int       `json:"attempts"`
	FailAction  string    `json:"fail_action"`
	ExpiresAt   time.Time `json:"expires_at"`
	PromptID    int64     `json:"prompt_id"`
	KickStarted bool      `json:"-"`
}

const verifyColumns = "token,chat_id,user_id,challenge_type,question,answer_hash,status,attempts,fail_action,expires_at,prompt_id,kick_started"

func scanVerification(row interface{ Scan(...any) error }) (Verification, error) {
	var v Verification
	e := row.Scan(&v.Token, &v.ChatID, &v.UserID, &v.Type, &v.Question, &v.AnswerHash, &v.Status, &v.Attempts, &v.FailAction, &v.ExpiresAt, &v.PromptID, &v.KickStarted)
	return v, e
}
func HashAnswer(token, answer string) string {
	h := sha256.Sum256([]byte(token + ":" + answer))
	return hex.EncodeToString(h[:])
}
func (s *Store) Verification(ctx context.Context, token string) (Verification, error) {
	return scanVerification(s.DB.QueryRowContext(ctx, "SELECT "+verifyColumns+" FROM verification_sessions WHERE token=?", token))
}
func (s *Store) ActiveVerification(ctx context.Context, chat, user int64) (Verification, error) {
	return scanVerification(s.DB.QueryRowContext(ctx, "SELECT "+verifyColumns+" FROM verification_sessions WHERE chat_id=? AND user_id=? AND status IN ('pending','completing','expiring','releasing') ORDER BY created_at DESC LIMIT 1", chat, user))
}
func (s *Store) CreateVerification(ctx context.Context, v Verification) error {
	_, e := s.DB.ExecContext(ctx, "INSERT INTO verification_sessions(token,chat_id,user_id,challenge_type,question,answer_hash,fail_action,expires_at) VALUES(?,?,?,?,?,?,?,?)", v.Token, v.ChatID, v.UserID, v.Type, v.Question, v.AnswerHash, v.FailAction, v.ExpiresAt)
	return e
}
func (s *Store) BeginVerificationKick(ctx context.Context, token string) error {
	_, e := s.DB.ExecContext(ctx, "UPDATE verification_sessions SET kick_started=TRUE WHERE token=? AND status='expiring'", token)
	return e
}

func (s *Store) VerificationPrompt(ctx context.Context, token string, id int64) error {
	_, e := s.DB.ExecContext(ctx, "UPDATE verification_sessions SET prompt_id=? WHERE token=?", id, token)
	return e
}
func (s *Store) Answer(ctx context.Context, token string, user int64, answer string, events ...string) (Verification, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Verification{}, e
	}
	defer tx.Rollback()
	v, e := scanVerification(tx.QueryRowContext(ctx, "SELECT "+verifyColumns+" FROM verification_sessions WHERE token=? FOR UPDATE", token))
	if e == sql.ErrNoRows {
		return v, ErrVerification
	}
	if e != nil {
		return v, e
	}
	if v.UserID != user {
		return v, ErrVerification
	}
	event := ""
	if len(events) > 0 {
		event = events[0]
	}
	if event != "" {
		var correct bool
		err := tx.QueryRowContext(ctx, "SELECT correct FROM verification_answers WHERE token=? AND event_key=?", token, event).Scan(&correct)
		if err == nil {
			if !correct {
				return v, ErrVerification
			}
			return v, nil
		}
		if err != sql.ErrNoRows {
			return v, err
		}
	}
	record := func(correct bool) error {
		if event == "" {
			return nil
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO verification_answers(token,event_key,correct) VALUES(?,?,?)", token, event, correct)
		return err
	}
	if v.UserID != user || v.Status != "pending" || time.Now().After(v.ExpiresAt) || v.Attempts >= 3 {
		return v, ErrVerification
	}
	if subtle.ConstantTimeCompare([]byte(v.AnswerHash), []byte(HashAnswer(token, answer))) != 1 {
		if e = record(false); e != nil {
			return v, e
		}
		_, e = tx.ExecContext(ctx, "UPDATE verification_sessions SET attempts=attempts+1,expires_at=IF(attempts>=3,UTC_TIMESTAMP(6),expires_at) WHERE token=?", token)
		if e != nil {
			return v, e
		}
		if e = tx.Commit(); e != nil {
			return v, e
		}
		return v, ErrVerification
	}
	_, e = tx.ExecContext(ctx, "UPDATE verification_sessions SET status='completing' WHERE token=?", token)
	if e != nil {
		return v, e
	}
	if e = record(true); e != nil {
		return v, e
	}
	v.Status = "completing"
	return v, tx.Commit()
}
func (s *Store) DueVerifications(ctx context.Context) ([]Verification, error) {
	_, e := s.DB.ExecContext(ctx, "UPDATE verification_sessions SET status='expiring' WHERE status='pending' AND expires_at<=UTC_TIMESTAMP(6)")
	if e != nil {
		return nil, e
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT "+verifyColumns+" FROM (SELECT v.*,ROW_NUMBER() OVER(PARTITION BY chat_id ORDER BY next_attempt_at,created_at) AS recovery_position FROM verification_sessions v WHERE (status IN ('expiring','completing','releasing') OR (status IN ('verified','expired','cancelled','blocked','left') AND notice_done=FALSE)) AND next_attempt_at<=UTC_TIMESTAMP(6)) due WHERE recovery_position<=2 ORDER BY next_attempt_at LIMIT 100")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Verification{}
	for rows.Next() {
		v, e := scanVerification(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) FinishVerification(ctx context.Context, v Verification, status string) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	r, e := tx.ExecContext(ctx, "UPDATE verification_sessions SET status=?,verified_at=IF(?='verified',UTC_TIMESTAMP(6),NULL),last_error='' WHERE token=? AND status=?", status, status, v.Token, v.Status)
	if e != nil {
		return e
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return ErrVerification
	}
	if status == "verified" {
		_, e = tx.ExecContext(ctx, "UPDATE group_members SET verified_at=UTC_TIMESTAMP(6) WHERE chat_id=? AND user_id=?", v.ChatID, v.UserID)
		if e != nil {
			return e
		}
	}
	return tx.Commit()
}

func (s *Store) VerificationRetry(ctx context.Context, token string, cause error) error {
	_, e := s.DB.ExecContext(ctx, "UPDATE verification_sessions SET next_attempt_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL LEAST(300,5*(recovery_attempts+1)) SECOND),recovery_attempts=recovery_attempts+1,last_error=? WHERE token=? AND (status IN ('completing','expiring','releasing') OR notice_done=FALSE)", clip(cause.Error(), 500), token)
	return e
}

func (s *Store) FinishVerificationNotices(ctx context.Context, token string) error {
	_, e := s.DB.ExecContext(ctx, "UPDATE verification_sessions SET notice_done=TRUE,last_error='' WHERE token=?", token)
	return e
}

func (s *Store) VerificationNoticesDone(ctx context.Context, token string) (bool, error) {
	var done bool
	e := s.DB.QueryRowContext(ctx, "SELECT notice_done FROM verification_sessions WHERE token=?", token).Scan(&done)
	return done, e
}
