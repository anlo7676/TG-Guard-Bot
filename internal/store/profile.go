package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
)

func (s *Store) LearnProfileAvatar(ctx context.Context, chat, user int64, hash, domain string) error {
	b, err := hex.DecodeString(hash)
	if err != nil || len(b) != 32 || chat >= 0 || user <= 0 || domain == "" {
		return errors.New("invalid avatar sample")
	}
	_, err = s.DB.ExecContext(ctx, `INSERT IGNORE INTO profile_avatar_samples(chat_id,avatar_hash,source_user_id,matched_domain) VALUES(?,?,?,?)`, chat, hash, user, domain)
	return err
}

func (s *Store) ProfileAvatarDomain(ctx context.Context, chat int64, hash string) (string, error) {
	var domain string
	err := s.DB.QueryRowContext(ctx, `SELECT matched_domain FROM profile_avatar_samples WHERE chat_id=? AND avatar_hash=?`, chat, hash).Scan(&domain)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return domain, err
}

func (s *Store) ProfilePunishmentStatus(ctx context.Context, key string) (string, error) {
	var status string
	err := s.DB.QueryRowContext(ctx, `SELECT status FROM punishments WHERE event_key=?`, key).Scan(&status)
	return status, err
}
