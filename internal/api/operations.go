package api

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

func (s *Server) groupHealth(w http.ResponseWriter, r *http.Request) {
	chat, ok := s.groupID(w, r, false)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	member, err := s.Service.Bot.Member(ctx, chat, s.Service.Bot.ID)
	if err != nil {
		respond(w, 502, map[string]string{"error": "无法查询 Telegram 群权限，请确认机器人仍在群内并检查网络"})
		return
	}
	respond(w, 200, map[string]any{"bot_present": member.Present(), "administrator": member.Admin(), "can_delete": member.CanDelete, "can_restrict": member.CanRestrict, "ready": member.Admin() && member.CanDelete && member.CanRestrict})
}
func (s *Server) userDetail(w http.ResponseWriter, r *http.Request) {
	chat, ok := s.groupID(w, r, false)
	if !ok {
		return
	}
	user, err := strconv.ParseInt(r.PathValue("user"), 10, 64)
	if err != nil || user <= 0 {
		respond(w, 400, map[string]string{"error": "无效的用户 ID"})
		return
	}
	result := map[string]any{}
	for _, q := range []struct{ Key, SQL string }{
		{"verifications", "SELECT status,challenge_type,attempts,expires_at,verified_at,created_at FROM verification_sessions WHERE chat_id=? AND user_id=? ORDER BY created_at DESC LIMIT 20"},
		{"reviews", "SELECT id,message_text,risk_score,decision,created_at FROM moderation_logs WHERE chat_id=? AND user_id=? ORDER BY id DESC LIMIT 20"},
		{"punishments", "SELECT id,decision,status,last_error,created_at FROM punishments WHERE chat_id=? AND user_id=? ORDER BY id DESC LIMIT 20"},
	} {
		rows, e := s.Service.Store.Rows(r.Context(), q.SQL, chat, user)
		if e != nil {
			apiError(w, e)
			return
		}
		result[q.Key] = rows
	}
	respond(w, 200, result)
}

func (s *Server) retryDead(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("update"), 10, 64)
	if err != nil || id <= 0 {
		respond(w, 400, map[string]string{"error": "无效任务 ID"})
		return
	}
	if err = s.Service.Store.RetryDead(r.Context(), id, 0); err != nil {
		apiError(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
