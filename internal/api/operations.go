package api

import (
	"context"
	"net/http"
	"strconv"
	"tgguard/internal/store"
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
	for _, q := range []struct {
		Key string
		SQL store.View
	}{
		{"verifications", store.ViewUserVerifications},
		{"reviews", store.ViewUserReviews},
		{"punishments", store.ViewUserPunishments},
	} {
		rows, e := s.Service.Store.View(r.Context(), q.SQL, chat, user)
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
	if err = s.Service.Store.RetryDead(r.Context(), id, actor(r)); err != nil {
		apiError(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
