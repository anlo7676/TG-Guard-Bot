package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"tgguard/internal/domain"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct{ DB *sql.DB }

func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, e := mysql.ParseDSN(dsn)
	if e != nil {
		return nil, fmt.Errorf("invalid MySQL DSN")
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.MultiStatements = false
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 10 * time.Second
	cfg.WriteTimeout = 10 * time.Second
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	db, e := sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(24)
	db.SetMaxIdleConns(12)
	db.SetConnMaxLifetime(5 * time.Minute)
	if e = db.PingContext(ctx); e != nil {
		db.Close()
		return nil, fmt.Errorf("MySQL connection failed: %w", e)
	}
	return &Store{DB: db}, nil
}
func (s *Store) Migrate(ctx context.Context) error {
	c, e := s.DB.Conn(ctx)
	if e != nil {
		return e
	}
	defer c.Close()
	var locked int
	if e = c.QueryRowContext(ctx, "SELECT GET_LOCK('tg_guard_migrations',30)").Scan(&locked); e != nil {
		return e
	}
	if locked != 1 {
		return fmt.Errorf("migration lock unavailable")
	}
	defer c.ExecContext(context.Background(), "SELECT RELEASE_LOCK('tg_guard_migrations')")
	if _, e = c.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version VARCHAR(255) PRIMARY KEY, applied_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6))"); e != nil {
		return e
	}
	files, e := migrations.ReadDir("migrations")
	if e != nil {
		return e
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
	for _, f := range files {
		var n int
		if e = c.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version=?", f.Name()).Scan(&n); e != nil {
			return e
		}
		if n > 0 {
			continue
		}
		b, e := migrations.ReadFile("migrations/" + f.Name())
		if e != nil {
			return e
		}
		for _, stmt := range strings.Split(string(b), ";") {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if _, e = c.ExecContext(ctx, stmt); e != nil {
				return fmt.Errorf("migration %s: %w", f.Name(), e)
			}
		}
		if _, e = c.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES(?)", f.Name()); e != nil {
			return e
		}
	}
	return nil
}
func JSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func (s *Store) RegisterGroup(ctx context.Context, c domain.Chat) error {
	_, e := s.DB.ExecContext(ctx, "INSERT INTO bot_groups(chat_id,title) VALUES(?,?) ON DUPLICATE KEY UPDATE title=VALUES(title),active=TRUE", c.ID, c.Title)
	return e
}
func (s *Store) DeactivateGroup(ctx context.Context, chat int64) error {
	_, e := s.DB.ExecContext(ctx, "UPDATE bot_groups SET active=FALSE WHERE chat_id=?", chat)
	return e
}
func (s *Store) VerifiedCurrentJoin(ctx context.Context, chat, user int64) (bool, error) {
	var verified bool
	e := s.DB.QueryRowContext(ctx, "SELECT verified_at IS NOT NULL AND verified_at>=joined_at FROM group_members WHERE chat_id=? AND user_id=?", chat, user).Scan(&verified)
	return verified, e
}
func (s *Store) User(ctx context.Context, u domain.User) error {
	_, e := s.DB.ExecContext(ctx, "INSERT INTO users(user_id,username,display_name,is_bot) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE username=VALUES(username),display_name=VALUES(display_name),is_bot=VALUES(is_bot)", u.ID, u.Username, strings.TrimSpace(u.FirstName+" "+u.LastName), u.IsBot)
	return e
}
func (s *Store) Join(ctx context.Context, chat int64, u domain.User, role string) error {
	if e := s.User(ctx, u); e != nil {
		return e
	}
	_, e := s.DB.ExecContext(ctx, "INSERT INTO group_members(chat_id,user_id,role,joined_at) VALUES(?,?,?,UTC_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE verified_at=IF(left_at IS NOT NULL,NULL,verified_at),joined_at=IF(left_at IS NOT NULL OR joined_at IS NULL,UTC_TIMESTAMP(6),joined_at),message_count=IF(left_at IS NOT NULL,0,message_count),role=VALUES(role),left_at=NULL", chat, u.ID, role)
	return e
}
func (s *Store) Leave(ctx context.Context, chat, user int64) error {
	_, e := s.DB.ExecContext(ctx, "UPDATE group_members SET left_at=UTC_TIMESTAMP(6) WHERE chat_id=? AND user_id=?", chat, user)
	return e
}
func (s *Store) Context(ctx context.Context, chat, user int64) (isNew, first bool, err error) {
	var joined sql.NullTime
	var count int64
	err = s.DB.QueryRowContext(ctx, "SELECT joined_at,message_count FROM group_members WHERE chat_id=? AND user_id=?", chat, user).Scan(&joined, &count)
	if err == sql.ErrNoRows {
		return false, false, nil
	}
	return joined.Valid && time.Since(joined.Time) < 10*time.Minute, joined.Valid && count == 0, err
}
func (s *Store) Settings(ctx context.Context, chat int64) (domain.Settings, error) {
	v := domain.DefaultSettings()
	var b []byte
	e := s.DB.QueryRowContext(ctx, "SELECT settings FROM group_settings WHERE chat_id=?", chat).Scan(&b)
	if e == sql.ErrNoRows {
		return v, nil
	}
	if e != nil {
		return v, e
	}
	e = json.Unmarshal(b, &v)
	if e == nil {
		e = v.Validate()
	}
	return v, e
}
func audit(ctx context.Context, tx *sql.Tx, chat, actor int64, action string, old, new any) error {
	_, e := tx.ExecContext(ctx, "INSERT INTO admin_audits(chat_id,actor_id,action,old_value,new_value) VALUES(?,?,?,?,?)", chat, actor, action, JSON(old), JSON(new))
	return e
}
func (s *Store) SaveSettings(ctx context.Context, chat, actor int64, v domain.Settings) error {
	if e := v.Validate(); e != nil {
		return e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var old []byte
	e = tx.QueryRowContext(ctx, "SELECT settings FROM group_settings WHERE chat_id=? FOR UPDATE", chat).Scan(&old)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO group_settings(chat_id,settings) VALUES(?,?) ON DUPLICATE KEY UPDATE settings=VALUES(settings)", chat, JSON(v)); e != nil {
		return e
	}
	var before any
	if len(old) > 0 {
		json.Unmarshal(old, &before)
	}
	if e = audit(ctx, tx, chat, actor, "settings.update", before, v); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) ListStatus(ctx context.Context, chat, user int64, username string) (string, error) {
	var kind string
	e := s.DB.QueryRowContext(ctx, `SELECT kind FROM list_entries WHERE chat_id IN (0,?) AND (user_id=? OR (username<>'' AND username=?)) AND (expires_at IS NULL OR expires_at>UTC_TIMESTAMP(6)) ORDER BY FIELD(kind,'black','white','trusted') LIMIT 1`, chat, user, username).Scan(&kind)
	if e == sql.ErrNoRows {
		return "", nil
	}
	return kind, e
}
func (s *Store) SaveList(ctx context.Context, l domain.ListEntry, actor int64, remove bool) error {
	if e := l.Validate(); e != nil {
		return e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if remove {
		_, e = tx.ExecContext(ctx, "DELETE FROM list_entries WHERE chat_id=? AND user_id=? AND username=? AND kind=?", l.ChatID, l.UserID, l.Username, l.Kind)
	} else {
		_, e = tx.ExecContext(ctx, "INSERT INTO list_entries(chat_id,user_id,username,kind,reason,created_by,expires_at) VALUES(?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE reason=VALUES(reason),expires_at=VALUES(expires_at),created_by=VALUES(created_by)", l.ChatID, l.UserID, l.Username, l.Kind, l.Reason, actor, l.ExpiresAt)
	}
	if e != nil {
		return e
	}
	action := "list.upsert"
	if remove {
		action = "list.delete"
	}
	if e = audit(ctx, tx, l.ChatID, actor, action, nil, l); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Keywords(ctx context.Context, chat int64) ([]domain.Keyword, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT id,chat_id,keyword,match_type,reply_type,content,priority,enabled,reply FROM keyword_rules WHERE chat_id=? ORDER BY priority DESC,id ASC", chat)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []domain.Keyword{}
	for rows.Next() {
		var k domain.Keyword
		if e = rows.Scan(&k.ID, &k.ChatID, &k.Keyword, &k.MatchType, &k.ReplyType, &k.Content, &k.Priority, &k.Enabled, &k.Reply); e != nil {
			return nil, e
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
func (s *Store) SaveKeyword(ctx context.Context, k domain.Keyword, actor int64) (int64, error) {
	if e := k.Validate(); e != nil {
		return 0, e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	if k.ID == 0 {
		r, e := tx.ExecContext(ctx, "INSERT INTO keyword_rules(chat_id,keyword,match_type,reply_type,content,priority,enabled,reply,created_by) VALUES(?,?,?,?,?,?,?,?,?)", k.ChatID, k.Keyword, k.MatchType, k.ReplyType, k.Content, k.Priority, k.Enabled, k.Reply, actor)
		if e != nil {
			return 0, e
		}
		k.ID, e = r.LastInsertId()
		if e != nil {
			return 0, e
		}
	} else {
		var id int64
		if e = tx.QueryRowContext(ctx, "SELECT id FROM keyword_rules WHERE id=? AND chat_id=? FOR UPDATE", k.ID, k.ChatID).Scan(&id); e != nil {
			return 0, e
		}
		_, e = tx.ExecContext(ctx, "UPDATE keyword_rules SET keyword=?,match_type=?,reply_type=?,content=?,priority=?,enabled=?,reply=? WHERE id=? AND chat_id=?", k.Keyword, k.MatchType, k.ReplyType, k.Content, k.Priority, k.Enabled, k.Reply, k.ID, k.ChatID)
		if e != nil {
			return 0, e
		}
	}
	if e = audit(ctx, tx, k.ChatID, actor, "keyword.upsert", nil, k); e != nil {
		return 0, e
	}
	return k.ID, tx.Commit()
}
func (s *Store) DeleteKeyword(ctx context.Context, chat, id, actor int64) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	r, e := tx.ExecContext(ctx, "DELETE FROM keyword_rules WHERE chat_id=? AND id=?", chat, id)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	if e = audit(ctx, tx, chat, actor, "keyword.delete", nil, map[string]int64{"id": id}); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) ViolationCount(ctx context.Context, chat, user int64) (int, error) {
	var n int
	e := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM punishments WHERE chat_id=? AND user_id=? AND status='done' AND source='automatic'", chat, user).Scan(&n)
	return n, e
}

type Log struct {
	ID        int64            `json:"id"`
	EventKey  string           `json:"event_key"`
	ChatID    int64            `json:"chat_id"`
	UserID    int64            `json:"user_id"`
	MessageID int64            `json:"message_id"`
	Text      string           `json:"text"`
	Risk      domain.Risk      `json:"risk"`
	AI        *domain.AIResult `json:"ai"`
	Decision  domain.Decision  `json:"decision"`
	Source    string           `json:"source"`
}

func scanLog(row interface{ Scan(...any) error }) (Log, error) {
	var l Log
	var matches, ai, decision []byte
	e := row.Scan(&l.ID, &l.EventKey, &l.ChatID, &l.UserID, &l.MessageID, &l.Text, &l.Risk.Score, &matches, &ai, &decision, &l.Source)
	if e != nil {
		return l, e
	}
	if e = json.Unmarshal(matches, &l.Risk.Matches); e != nil {
		return l, e
	}
	if len(ai) > 0 {
		if e = json.Unmarshal(ai, &l.AI); e != nil {
			return l, e
		}
	}
	e = json.Unmarshal(decision, &l.Decision)
	return l, e
}

const logColumns = "id,event_key,chat_id,user_id,message_id,message_text,risk_score,matched_rules,ai_result,decision,source"

func (s *Store) GetLog(ctx context.Context, key string) (Log, error) {
	return scanLog(s.DB.QueryRowContext(ctx, "SELECT "+logColumns+" FROM moderation_logs WHERE event_key=?", key))
}
func (s *Store) GetLogID(ctx context.Context, id int64) (Log, error) {
	return scanLog(s.DB.QueryRowContext(ctx, "SELECT "+logColumns+" FROM moderation_logs WHERE id=?", id))
}
func (s *Store) SaveLog(ctx context.Context, l Log) (Log, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return l, e
	}
	defer tx.Rollback()
	var ai any
	if l.AI != nil {
		ai = JSON(l.AI)
	}
	r, e := tx.ExecContext(ctx, "INSERT IGNORE INTO moderation_logs(event_key,chat_id,user_id,message_id,message_text,risk_score,matched_rules,ai_result,decision,source) VALUES(?,?,?,?,?,?,?,?,?,?)", l.EventKey, l.ChatID, l.UserID, l.MessageID, l.Text, l.Risk.Score, JSON(l.Risk.Matches), ai, JSON(l.Decision), l.Source)
	if e != nil {
		return l, e
	}
	n, _ := r.RowsAffected()
	if n > 0 && l.Source == "automatic" {
		if _, e = tx.ExecContext(ctx, "INSERT INTO group_members(chat_id,user_id,message_count) VALUES(?,?,1) ON DUPLICATE KEY UPDATE message_count=message_count+1", l.ChatID, l.UserID); e != nil {
			return l, e
		}
	}
	if e = tx.Commit(); e != nil {
		return l, e
	}
	return s.GetLog(ctx, l.EventKey)
}

type Punishment struct {
	Decision       domain.Decision
	Status         string
	Deleted, Acted bool
	CreatedAt      time.Time
}

func (s *Store) PreparePunishment(ctx context.Context, l Log, actor int64) (Punishment, error) {
	var p Punishment
	_, e := s.DB.ExecContext(ctx, "INSERT IGNORE INTO punishments(event_key,chat_id,user_id,message_id,decision,source,actor_id) VALUES(?,?,?,?,?,?,?)", l.EventKey, l.ChatID, l.UserID, l.MessageID, JSON(l.Decision), l.Source, actor)
	if e != nil {
		return p, e
	}
	var b []byte
	e = s.DB.QueryRowContext(ctx, "SELECT decision,status,deleted,acted,created_at FROM punishments WHERE event_key=?", l.EventKey).Scan(&b, &p.Status, &p.Deleted, &p.Acted, &p.CreatedAt)
	if e == nil {
		e = json.Unmarshal(b, &p.Decision)
	}
	return p, e
}
func (s *Store) PunishmentStep(ctx context.Context, key, step string) error {
	var q string
	switch step {
	case "deleted":
		q = "UPDATE punishments SET deleted=TRUE WHERE event_key=?"
	case "acted":
		q = "UPDATE punishments SET acted=TRUE WHERE event_key=?"
	case "done":
		q = "UPDATE punishments SET status='done',last_error='' WHERE event_key=?"
	case "skipped":
		q = "UPDATE punishments SET status='skipped' WHERE event_key=?"
	default:
		return fmt.Errorf("invalid step")
	}
	_, e := s.DB.ExecContext(ctx, q, key)
	return e
}
func (s *Store) PunishmentError(ctx context.Context, key string, err error) error {
	_, e := s.DB.ExecContext(ctx, "UPDATE punishments SET last_error=? WHERE event_key=?", clip(err.Error(), 500), key)
	return e
}
func (s *Store) AIUsage(ctx context.Context, chat int64, model string, in, out int, ms int64, cached bool, result string) error {
	_, e := s.DB.ExecContext(ctx, "INSERT INTO ai_usage_logs(chat_id,model,input_tokens,output_tokens,latency_ms,cached,result) VALUES(?,?,?,?,?,?,?)", chat, model, in, out, ms, cached, result)
	return e
}
func (s *Store) Feedback(ctx context.Context, chat, log, actor int64, note string) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var exists int
	if e = tx.QueryRowContext(ctx, "SELECT id FROM moderation_logs WHERE id=? AND chat_id=?", log, chat).Scan(&exists); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO feedback(log_id,actor_id,note) VALUES(?,?,?) ON DUPLICATE KEY UPDATE note=VALUES(note)", log, actor, note)
	if e != nil {
		return e
	}
	if e = audit(ctx, tx, chat, actor, "feedback", nil, map[string]any{"log_id": log, "note": note}); e != nil {
		return e
	}
	return tx.Commit()
}
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// Rows is used only with static queries from the management API; it never interpolates user input.
func (s *Store) Rows(ctx context.Context, q string, args ...any) ([]map[string]any, error) {
	rows, e := s.DB.QueryContext(ctx, q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	cols, e := rows.Columns()
	if e != nil {
		return nil, e
	}
	out := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range values {
			ptr[i] = &values[i]
		}
		if e = rows.Scan(ptr...); e != nil {
			return nil, e
		}
		m := map[string]any{}
		for i, c := range cols {
			v := values[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			m[c] = v
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
