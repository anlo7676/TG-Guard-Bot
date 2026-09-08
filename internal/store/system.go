package store

import (
	"context"
	"encoding/json"
)

func (s *Store) SystemSettings(ctx context.Context) (string, error) {
	var value string
	e := s.DB.QueryRowContext(ctx, "SELECT value_encrypted FROM system_settings WHERE id=1").Scan(&value)
	return value, e
}
func (s *Store) SaveSystemSettings(ctx context.Context, value string, before, after any) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var public struct {
		Admins []int64 `json:"super_admins"`
	}
	if e = json.Unmarshal([]byte(JSON(after)), &public); e != nil {
		return e
	}
	// Removing an administrator also destroys their credential in the same commit;
	// adding the ID again cannot resurrect an old credential or session.
	if _, e = tx.ExecContext(ctx, "DELETE FROM web_credentials WHERE user_id NOT IN (SELECT id FROM JSON_TABLE(?, '$[*]' COLUMNS(id BIGINT PATH '$')) ids)", JSON(public.Admins)); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO system_settings(id,value_encrypted) VALUES(1,?) ON DUPLICATE KEY UPDATE value_encrypted=VALUES(value_encrypted)", value); e != nil {
		return e
	}
	if e = audit(ctx, tx, 0, 0, "system.settings", before, after); e != nil {
		return e
	}
	return tx.Commit()
}
