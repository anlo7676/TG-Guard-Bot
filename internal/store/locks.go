package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

func (s *Store) LockName(ctx context.Context, kind string) (string, error) {
	var database string
	if err := s.DB.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(database))
	return fmt.Sprintf("tg_guard:%s:%x", kind, sum[:16]), nil
}

// ReleaseLock runs independently of request cancellation with a bounded timeout.
func ReleaseLock(conn *sql.Conn, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var released sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", name).Scan(&released); err != nil {
		slog.Warn("MySQL lock release failed", "lock_name", name, "error", err)
	} else if !released.Valid || released.Int64 != 1 {
		slog.Warn("MySQL lock no longer owned at release", "lock_name", name)
	}
}
