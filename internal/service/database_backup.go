package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"tgguard/internal/store"
)

const backupHeader = "TG-GUARD-BACKUP-1\n"

func backupCipher(key string) (cipher.AEAD, error) {
	if len(key) < 32 {
		return nil, errors.New("备份需要有效的配置加密密钥")
	}
	h := sha256.Sum256([]byte("tg-guard-database-backup-v1:" + key))
	block, err := aes.NewCipher(h[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func sealBackup(b store.DatabaseBackup, key string) ([]byte, error) {
	aead, err := backupCipher(key)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	if len(raw) > store.MaxBackupBytes {
		return nil, errors.New("数据库超过 Web 备份上限，请使用服务器备份脚本")
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte(backupHeader), nonce...)
	return aead.Seal(out, nonce, raw, []byte(backupHeader)), nil
}

func openBackup(data []byte, key string) (store.DatabaseBackup, error) {
	var b store.DatabaseBackup
	aead, err := backupCipher(key)
	if err != nil {
		return b, err
	}
	n := len(backupHeader)
	if len(data) < n+aead.NonceSize()+aead.Overhead() || len(data) > store.MaxBackupBytes+128 || string(data[:n]) != backupHeader {
		return b, errors.New("无效的 TG Guard 数据库备份文件")
	}
	raw, err := aead.Open(nil, data[n:n+aead.NonceSize()], data[n+aead.NonceSize():], []byte(backupHeader))
	if err != nil {
		return b, errors.New("备份已损坏或配置加密密钥不匹配")
	}
	if err = json.Unmarshal(raw, &b); err != nil {
		return b, errors.New("备份内容格式无效")
	}
	return b, nil
}

func (s *Service) BackupDatabase(ctx context.Context, key string) ([]byte, error) {
	b, err := s.Store.BackupDatabase(ctx, s.Bot.ID)
	if err != nil {
		return nil, err
	}
	return sealBackup(b, key)
}

func (s *Service) RestoreDatabase(ctx context.Context, data []byte, key string) error {
	b, err := openBackup(data, key)
	if err != nil {
		return err
	}
	if b.BotID != s.Bot.ID || b.Version != 1 {
		return errors.New("备份版本或机器人身份不匹配")
	}
	if s.Maintenance == nil {
		return errors.New("维护模式不可用")
	}
	if err = s.Maintenance.Pause(ctx); err != nil {
		return err
	}
	if err = s.Store.RestoreDatabase(ctx, b, s.Bot.ID); err != nil {
		// Even a commit transport error has an uncertain outcome. Keep all
		// business processing paused until startup reconciles the durable marker.
		return fmt.Errorf("恢复未确认完成，业务已暂停；请检查日志并重启服务: %w", err)
	}
	return nil
}
