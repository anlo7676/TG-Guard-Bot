package store

import "context"

func (s *Store) CancelExternallyManagedVerification(ctx context.Context, chat, user, date int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE verification_sessions SET status='cancelled' WHERE chat_id=? AND user_id=? AND created_at<FROM_UNIXTIME(?+1) AND status IN ('pending','expiring','completing','expired')`, chat, user, date)
	return err
}

type SelfVerification struct {
	Token string
	Title string
}

// Only the current join's latest verification can own the remaining restriction.
const selfVerificationEligible = `v.status IN ('pending','expiring','expired')
AND (v.status='pending' OR v.fail_action='mute')
AND EXISTS(SELECT 1 FROM bot_groups g WHERE g.chat_id=v.chat_id AND g.active=TRUE AND g.authorization='approved')
AND EXISTS(SELECT 1 FROM group_members m WHERE m.chat_id=v.chat_id AND m.user_id=v.user_id AND m.left_at IS NULL AND m.joined_at<=v.created_at AND (m.verified_at IS NULL OR m.verified_at<m.joined_at))
AND NOT EXISTS(SELECT 1 FROM verification_sessions newer WHERE newer.chat_id=v.chat_id AND newer.user_id=v.user_id AND newer.created_at>v.created_at)
AND NOT EXISTS(SELECT 1 FROM punishments p WHERE p.chat_id=v.chat_id AND p.user_id=v.user_id AND p.created_at>=v.created_at AND (p.status='pending' OR p.acted=TRUE OR EXISTS(SELECT 1 FROM punishment_workflows w WHERE w.event_key=p.event_key AND w.started=TRUE)) AND JSON_UNQUOTE(JSON_EXTRACT(p.decision,'$.action')) IN ('mute','ban','kick','unmute','unban'))`

func (s *Store) SelfVerifications(ctx context.Context, user int64) ([]SelfVerification, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT v.token,g.title FROM verification_sessions v JOIN bot_groups g ON g.chat_id=v.chat_id WHERE v.user_id=? AND `+selfVerificationEligible+` ORDER BY v.created_at DESC LIMIT 20`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SelfVerification{}
	for rows.Next() {
		var v SelfVerification
		if err := rows.Scan(&v.Token, &v.Title); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) CanSelfVerify(ctx context.Context, token string, user int64) (bool, error) {
	var ok bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM verification_sessions v WHERE v.token=? AND v.user_id=? AND `+selfVerificationEligible+`)`, token, user).Scan(&ok)
	return ok, err
}

func (s *Store) ReopenVerification(ctx context.Context, v Verification) error {
	r, err := s.DB.ExecContext(ctx, `UPDATE verification_sessions SET status='pending',challenge_type=?,question=?,answer_hash=?,attempts=0,expires_at=?,prompt_id=0,notice_done=FALSE,next_attempt_at=UTC_TIMESTAMP(6),recovery_attempts=0,last_error='' WHERE token=? AND user_id=? AND status='expired' AND fail_action='mute'`, v.Type, v.Question, v.AnswerHash, v.ExpiresAt, v.Token, v.UserID)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return ErrVerification
	}
	return err
}
