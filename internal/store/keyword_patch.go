package store

import (
	"context"
	"encoding/json"

	"tgguard/internal/domain"
)

// ChangeKeyword preserves fields changed by other editors during a private wizard.
func (s *Store) ChangeKeyword(ctx context.Context, chat, id, actor int64, change func(*domain.Keyword) error) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var k domain.Keyword
	var buttons []byte
	if e = tx.QueryRowContext(ctx, "SELECT id,chat_id,keyword,match_type,reply_type,content,priority,enabled,reply,buttons FROM keyword_rules WHERE id=? AND chat_id=? FOR UPDATE", id, chat).Scan(&k.ID, &k.ChatID, &k.Keyword, &k.MatchType, &k.ReplyType, &k.Content, &k.Priority, &k.Enabled, &k.Reply, &buttons); e != nil {
		return e
	}
	if len(buttons) > 0 {
		if e = json.Unmarshal(buttons, &k.Buttons); e != nil {
			return e
		}
	}
	before := JSON(k)
	if e = change(&k); e != nil {
		return e
	}
	if e = k.Validate(); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE keyword_rules SET keyword=?,match_type=?,reply_type=?,content=?,priority=?,enabled=?,reply=?,buttons=? WHERE id=? AND chat_id=?", k.Keyword, k.MatchType, k.ReplyType, k.Content, k.Priority, k.Enabled, k.Reply, JSON(k.Buttons), id, chat); e != nil {
		return e
	}
	if e = audit(ctx, tx, chat, actor, "keyword.update", json.RawMessage(before), k); e != nil {
		return e
	}
	return tx.Commit()
}
