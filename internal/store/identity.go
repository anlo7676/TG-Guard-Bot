package store

import (
	"context"
	"database/sql"
	"fmt"
)

// BindBot is persistent: changing credentials must not replay another bot's inbox.
func (s *Store) BindBot(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("invalid bot identity")
	}
	if _, e := s.DB.ExecContext(ctx, "INSERT IGNORE INTO bot_state(name,value) SELECT 'bot_id',? WHERE NOT EXISTS(SELECT 1 FROM update_inbox) AND NOT EXISTS(SELECT 1 FROM verification_sessions) AND NOT EXISTS(SELECT 1 FROM punishments) AND NOT EXISTS(SELECT 1 FROM bot_state WHERE name='poll_offset')", id); e != nil {
		return e
	}
	var owner int64
	if e := s.DB.QueryRowContext(ctx, "SELECT value FROM bot_state WHERE name='bot_id'").Scan(&owner); e != nil {
		if e == sql.ErrNoRows {
			return fmt.Errorf("unbound database has previous runtime data; use a fresh database or explicitly bind its original bot before upgrade")
		}
		return e
	}
	if owner != id {
		return fmt.Errorf("MySQL database belongs to bot %d; use a separate database when changing bots", owner)
	}
	return nil
}
