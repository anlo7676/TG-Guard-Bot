package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"tgguard/internal/domain"
)

type MenuGroup struct {
	ID    int64
	Title string
}

func (s *Store) MenuGroups(ctx context.Context, before int64) ([]MenuGroup, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT chat_id,title FROM bot_groups WHERE active=TRUE AND authorization='approved' AND chat_id< ? ORDER BY chat_id DESC LIMIT 11", before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MenuGroup{}
	for rows.Next() {
		var g MenuGroup
		if err = rows.Scan(&g.ID, &g.Title); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func (s *Store) MenuGroup(ctx context.Context, chat int64) (MenuGroup, error) {
	var g MenuGroup
	err := s.DB.QueryRowContext(ctx, "SELECT chat_id,title FROM bot_groups WHERE chat_id=? AND active=TRUE AND authorization='approved'", chat).Scan(&g.ID, &g.Title)
	return g, err
}

// ChangeSettings reads the latest settings under lock, preserving unrelated edits.
func (s *Store) ChangeSettings(ctx context.Context, chat, actor int64, change func(*domain.Settings) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	if err = tx.QueryRowContext(ctx, "SELECT chat_id FROM bot_groups WHERE chat_id=? AND active=TRUE AND authorization='approved' FOR UPDATE", chat).Scan(&id); err != nil {
		return err
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, "SELECT settings FROM group_settings WHERE chat_id=? FOR UPDATE", chat).Scan(&raw)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	v := domain.DefaultSettings()
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, &v); err != nil {
			return err
		}
	}
	if v.Rules == nil {
		v.Rules = map[string]domain.RuleSetting{}
	}
	before, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err = change(&v); err != nil {
		return err
	}
	if err = v.Validate(); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO group_settings(chat_id,settings) VALUES(?,?) ON DUPLICATE KEY UPDATE settings=VALUES(settings)", chat, SQLJSON(v)); err != nil {
		return err
	}
	if !v.VerificationEnabled {
		if _, err = tx.ExecContext(ctx, "UPDATE verification_sessions SET status='releasing',next_attempt_at=UTC_TIMESTAMP(6) WHERE chat_id=? AND status IN ('pending','completing','expiring')", chat); err != nil {
			return err
		}
	}
	if err = audit(ctx, tx, chat, actor, "settings.update", json.RawMessage(before), v); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MenuListEntry(ctx context.Context, chat, id int64) (domain.ListEntry, error) {
	l := domain.ListEntry{ChatID: chat}
	err := s.DB.QueryRowContext(ctx, "SELECT user_id,username,kind FROM list_entries WHERE id=? AND chat_id=?", id, chat).Scan(&l.UserID, &l.Username, &l.Kind)
	return l, err
}

func (s *Store) GroupExists(ctx context.Context, chat int64) (bool, error) {
	var found bool
	err := s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM bot_groups WHERE chat_id=?)", chat).Scan(&found)
	return found, err
}

func (s *Store) MemberRole(ctx context.Context, chat int64, u domain.User, role string) error {
	if err := s.User(ctx, u); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, "INSERT INTO group_members(chat_id,user_id,role) VALUES(?,?,?) ON DUPLICATE KEY UPDATE role=VALUES(role)", chat, u.ID, role)
	return err
}
