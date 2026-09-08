package api

import (
	"fmt"
	"net/http"
	"tgguard/internal/state"
)

func (s *Server) adminCredential(w http.ResponseWriter, r *http.Request) {
	if actor(r) != 0 {
		respond(w, 403, map[string]string{"error": "请使用后台恢复密钥登录后管理独立凭据"})
		return
	}
	var b struct {
		User   int64 `json:"user_id"`
		Revoke bool  `json:"revoke"`
	}
	if !decode(w, r, &b, 4096, true) {
		return
	}
	if b.User <= 0 || s.Service.Runtime == nil || (!b.Revoke && !s.Service.Runtime.IsAdmin(b.User)) {
		respond(w, 400, map[string]string{"error": "请先添加对应机器人管理员"})
		return
	}
	token := ""
	hash := ""
	if !b.Revoke {
		secret, err := state.Token()
		if err != nil {
			apiError(w, err)
			return
		}
		token = fmt.Sprintf("%d.%s", b.User, secret)
		hash = state.Hash(token)
	}
	write := func() error { return s.Service.Store.SetWebCredential(r.Context(), b.User, hash) }
	var err error
	if b.Revoke {
		err = write()
	} else {
		err = s.Service.Runtime.WithAdmin(b.User, write)
	}
	if err != nil {
		apiError(w, err)
		return
	}
	respond(w, 200, map[string]any{"user_id": b.User, "token": token, "revoked": b.Revoke})
}
func (s *Server) revokeSessions(w http.ResponseWriter, r *http.Request) {
	if actor(r) != 0 {
		respond(w, 403, map[string]string{"error": "只有后台恢复凭据可撤销全部会话"})
		return
	}
	epoch, err := state.Token()
	if err != nil {
		apiError(w, err)
		return
	}
	if err = s.Service.State.R.Set(r.Context(), "web:auth_epoch", epoch, 0).Err(); err != nil {
		apiError(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
