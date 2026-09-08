package store

import (
	"context"
	"crypto/sha256"
	"fmt"
)

func (s *Store) LockName(ctx context.Context, kind string) (string, error) {
	var database string
	if err := s.DB.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(database))
	return fmt.Sprintf("tg_guard:%s:%x", kind, sum[:16]), nil
}
