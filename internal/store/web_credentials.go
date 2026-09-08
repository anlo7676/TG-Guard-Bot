package store

import "context"

type auditActorKey struct{}

func WithAuditActor(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, auditActorKey{}, id)
}

func (s *Store) WebCredential(ctx context.Context, user int64) (string, error) {
	var hash string
	err := s.DB.QueryRowContext(ctx, "SELECT token_hash FROM web_credentials WHERE user_id=?", user).Scan(&hash)
	return hash, err
}
func (s *Store) SetWebCredential(ctx context.Context, user int64, hash string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if hash == "" {
		_, err = tx.ExecContext(ctx, "DELETE FROM web_credentials WHERE user_id=?", user)
	} else {
		_, err = tx.ExecContext(ctx, "INSERT INTO web_credentials(user_id,token_hash) VALUES(?,?) ON DUPLICATE KEY UPDATE token_hash=VALUES(token_hash)", user, hash)
	}
	if err != nil {
		return err
	}
	if err = audit(ctx, tx, 0, 0, "web.credential", nil, map[string]any{"user_id": user, "enabled": hash != ""}); err != nil {
		return err
	}
	return tx.Commit()
}
