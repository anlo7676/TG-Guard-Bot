package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"
)

const MaxBackupBytes = 32 << 20

var backupTables = []string{"schema_migrations", "bot_groups", "users", "group_settings", "group_members", "list_entries", "keyword_rules", "verification_sessions", "moderation_logs", "punishments", "ai_usage_logs", "admin_audits", "feedback", "update_inbox", "bot_state", "system_settings", "welcome_cleanup", "verification_answers", "web_credentials", "authorization_epochs", "punishment_workflows", "ad_warning_memory"}
var backupIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type DatabaseBackup struct {
	Version   int           `json:"version"`
	BotID     int64         `json:"bot_id"`
	CreatedAt time.Time     `json:"created_at"`
	Tables    []BackupTable `json:"tables"`
}
type BackupTable struct {
	Name    string      `json:"name"`
	Schema  string      `json:"schema"`
	Columns []string    `json:"columns"`
	Rows    [][]*string `json:"rows"`
}

func backupSchema(ctx context.Context, tx *sql.Tx, table string) ([]string, string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE,COALESCE(COLUMN_DEFAULT,''),EXTRA,COALESCE(COLLATION_NAME,'') FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? ORDER BY ORDINAL_POSITION`, table)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	columns := []string{}
	hash := sha256.New()
	for rows.Next() {
		var fields [6]string
		if err := rows.Scan(&fields[0], &fields[1], &fields[2], &fields[3], &fields[4], &fields[5]); err != nil {
			return nil, "", err
		}
		if !backupIdentifier.MatchString(fields[0]) {
			return nil, "", fmt.Errorf("unsupported column")
		}
		columns = append(columns, fields[0])
		if err := json.NewEncoder(hash).Encode(fields); err != nil {
			return nil, "", err
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(columns) == 0 {
		return nil, "", fmt.Errorf("missing table %s", table)
	}
	return columns, hex.EncodeToString(hash.Sum(nil)), nil
}

// BackupDatabase reads all project tables from one repeatable-read snapshot.
func (s *Store) BackupDatabase(ctx context.Context, botID int64) (DatabaseBackup, error) {
	b := DatabaseBackup{Version: 1, BotID: botID, CreatedAt: time.Now().UTC()}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return b, err
	}
	defer tx.Rollback()
	size := 0
	for _, name := range backupTables {
		cols, schema, err := backupSchema(ctx, tx, name)
		if err != nil {
			return b, err
		}
		table := BackupTable{Name: name, Columns: cols, Schema: schema, Rows: [][]*string{}}
		rows, err := tx.QueryContext(ctx, "SELECT `"+strings.Join(cols, "`,`")+"` FROM `"+name+"`")
		if err != nil {
			return b, err
		}
		for rows.Next() {
			values, dest := make([]any, len(cols)), make([]any, len(cols))
			for i := range values {
				dest[i] = &values[i]
			}
			if err = rows.Scan(dest...); err != nil {
				rows.Close()
				return b, err
			}
			row := make([]*string, len(cols))
			for i, v := range values {
				if v == nil {
					size += 5
					continue
				}
				var text string
				switch v := v.(type) {
				case []byte:
					text = string(v)
				case time.Time:
					text = v.UTC().Format("2006-01-02 15:04:05.999999")
				default:
					text = fmt.Sprint(v)
				}
				row[i] = &text
				// Bound worst-case JSON escaping and cell bookkeeping.
				size += len(text)*6 + 32
			}
			if size > MaxBackupBytes {
				rows.Close()
				return b, fmt.Errorf("数据库超过 Web 备份上限，请使用服务器备份脚本")
			}
			table.Rows = append(table.Rows, row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return b, err
		}
		b.Tables = append(b.Tables, table)
	}
	return b, tx.Commit()
}

func validateBackup(b DatabaseBackup, botID int64) error {
	if b.Version != 1 || b.BotID != botID || botID <= 0 || len(b.Tables) != len(backupTables) {
		return fmt.Errorf("备份版本、机器人身份或数据表不匹配")
	}
	for i, table := range b.Tables {
		if table.Name != backupTables[i] || len(table.Columns) == 0 {
			return fmt.Errorf("备份数据表不匹配")
		}
		for _, row := range table.Rows {
			if len(row) != len(table.Columns) {
				return fmt.Errorf("备份行列数量不匹配")
			}
		}
	}
	return nil
}

// RestoreDatabase uses only trusted table/column names and parameterized values.
// No DDL/TRUNCATE: any failure before commit rolls the whole replacement back.
func (s *Store) RestoreDatabase(ctx context.Context, b DatabaseBackup, botID int64) error {
	if err := validateBackup(b, botID); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range b.Tables {
		cols, schema, err := backupSchema(ctx, tx, table.Name)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(cols, table.Columns) || schema != table.Schema {
			return fmt.Errorf("表 %s 结构不同，请使用与备份对应的程序版本", table.Name)
		}
	}
	for _, table := range b.Tables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM `"+table.Name+"`"); err != nil {
			return err
		}
		if len(table.Rows) == 0 {
			continue
		}
		query := "INSERT INTO `" + table.Name + "` (`" + strings.Join(table.Columns, "`,`") + "`) VALUES (" + strings.TrimSuffix(strings.Repeat("?,", len(table.Columns)), ",") + ")"
		stmt, err := tx.PrepareContext(ctx, query)
		if err != nil {
			return err
		}
		for _, row := range table.Rows {
			args := make([]any, len(row))
			for i, cell := range row {
				if cell != nil {
					args[i] = *cell
				}
			}
			if _, err := stmt.ExecContext(ctx, args...); err != nil {
				stmt.Close()
				return err
			}
		}
		stmt.Close()
	}
	var owner int64
	if err := tx.QueryRowContext(ctx, "SELECT value FROM bot_state WHERE name='bot_id'").Scan(&owner); err != nil {
		return err
	}
	if owner != botID {
		return fmt.Errorf("备份数据库属于其他机器人")
	}
	// Durable marker ensures a crash between MySQL commit and Redis cleanup cannot
	// leave restored data paired with old sessions or callback caches on restart.
	if _, err := tx.ExecContext(ctx, "INSERT INTO bot_state(name,value) VALUES ('restore_pending',1) ON DUPLICATE KEY UPDATE value=1"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RestorePending(ctx context.Context) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM bot_state WHERE name='restore_pending'").Scan(&n)
	return n > 0, err
}
func (s *Store) FinishRestore(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM bot_state WHERE name='restore_pending'")
	return err
}
