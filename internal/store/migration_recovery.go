package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type columnSpec struct {
	table, name, definition, kind, nullable string
	defaultValue                            sql.NullString
}

// Historical files remain immutable. Reconcile only known ALTER statements, and
// reject incompatible columns instead of silently accepting schema corruption.
func executeMigration(ctx context.Context, c *sql.Conn, file, stmt string) error {
	if !strings.HasPrefix(strings.TrimSpace(stmt), "ALTER TABLE ") {
		_, err := c.ExecContext(ctx, stmt)
		return err
	}
	var specs []columnSpec
	switch file {
	case "003_keyword_buttons.sql":
		specs = []columnSpec{{"keyword_rules", "buttons", "JSON NULL", "json", "YES", sql.NullString{}}}
	case "004_group_authorization.sql":
		specs = []columnSpec{
			{"bot_groups", "authorization", "VARCHAR(16) NOT NULL DEFAULT 'pending'", "varchar(16)", "NO", sql.NullString{String: "pending", Valid: true}},
			{"bot_groups", "authorization_reason", "VARCHAR(500) NOT NULL DEFAULT ''", "varchar(500)", "NO", sql.NullString{String: "", Valid: true}},
		}
	case "005_welcome.sql":
		specs = []columnSpec{{"group_members", "welcomed_at", "DATETIME(6) NULL", "datetime(6)", "YES", sql.NullString{}}}
	case "006_verification_notices.sql":
		specs = []columnSpec{{"verification_sessions", "notice_done", "BOOLEAN NOT NULL DEFAULT FALSE", "tinyint(1)", "NO", sql.NullString{String: "0", Valid: true}}}
	default:
		_, err := c.ExecContext(ctx, stmt)
		return err
	}
	for _, s := range specs {
		var kind, nullable string
		var def sql.NullString
		err := c.QueryRowContext(ctx, "SELECT COLUMN_TYPE,IS_NULLABLE,COLUMN_DEFAULT FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? AND column_name=?", s.table, s.name).Scan(&kind, &nullable, &def)
		if err == sql.ErrNoRows {
			if _, err = c.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", s.table, s.name, s.definition)); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if strings.ToLower(kind) != s.kind || nullable != s.nullable || def != s.defaultValue {
			return fmt.Errorf("migration %s: incompatible existing column %s.%s", file, s.table, s.name)
		}
	}
	return nil
}
