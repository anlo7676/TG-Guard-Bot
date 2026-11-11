package api

import (
	"io"
	"log/slog"
	"net/http"
	"time"

	"tgguard/internal/store"
)

func (s *Server) backupDatabase(w http.ResponseWriter, r *http.Request) {
	data, err := s.Service.BackupDatabase(r.Context(), s.Config.SettingsKey)
	if err != nil {
		slog.Error("database backup failed", "error", err)
		respond(w, 500, map[string]string{"error": "备份失败；数据库可能超过 Web 上限，请检查服务日志或使用服务器备份脚本"})
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="tg-guard-`+time.Now().UTC().Format("20060102T150405Z")+`.tggbackup"`)
	w.Write(data)
	slog.Info("database backup downloaded", "actor", actor(r))
}

func (s *Server) restoreDatabase(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Confirm-Restore") != "RESTORE" {
		respond(w, 400, map[string]string{"error": "请确认覆盖数据库并在恢复后重启服务"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, store.MaxBackupBytes+128)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		respond(w, 400, map[string]string{"error": "备份文件无法读取或超过 32 MiB 上限"})
		return
	}
	if err = s.Service.RestoreDatabase(r.Context(), data, s.Config.SettingsKey); err != nil {
		slog.Error("database restore failed", "actor", actor(r), "error", err)
		respond(w, 400, map[string]string{"error": "恢复失败：文件、密钥、机器人身份或表结构不匹配，或数据库暂时不可用。若业务已暂停，请检查日志并重启服务。"})
		return
	}
	slog.Warn("database restored; restart required", "actor", actor(r))
	respond(w, 200, map[string]string{"message": "数据库已恢复，业务已暂停。请重启应用服务；重启时会清理当前 Redis 数据库的会话与临时状态。"})
}
