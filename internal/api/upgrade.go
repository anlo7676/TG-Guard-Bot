package api

import (
	"context"
	"net/http"
	"tgguard/internal/buildinfo"
	"tgguard/internal/upgrade"
	"time"
)

func (s *Server) upgrades(w http.ResponseWriter, r *http.Request) {
	q := upgrade.Queue{Dir: s.Config.UpdateDir}
	if r.Method == "GET" {
		respond(w, 200, map[string]any{"current": buildinfo.Version, "enabled": q.Ready(), "can_upgrade": actor(r) == 0, "job": q.Status()})
		return
	}
	if actor(r) != 0 {
		respond(w, 403, map[string]string{"error": "请使用后台恢复密钥登录后执行系统升级"})
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	if !decode(w, r, &body, 1024, true) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	release, e := upgrade.Latest(ctx, &http.Client{Timeout: 8 * time.Second})
	if e != nil {
		respond(w, 502, map[string]string{"error": e.Error()})
		return
	}
	if body.Version != release.Version || !upgrade.Newer(release.Version, buildinfo.Version) {
		respond(w, 409, map[string]string{"error": "目标版本已变化或无需升级，请重新检查更新"})
		return
	}
	if !q.Ready() {
		respond(w, 503, map[string]string{"error": "服务器升级服务未启用，请在服务器管理菜单选择启用网页升级"})
		return
	}
	unlock, e := s.Service.State.Lock(ctx, "web:upgrade", 15*time.Second)
	if e != nil {
		respond(w, 409, map[string]string{"error": "已有升级请求正在处理"})
		return
	}
	defer unlock()
	if e = s.Service.Store.RecordUpgrade(ctx, release.Version); e != nil {
		apiError(w, e)
		return
	}
	if e = q.Request(release.Version); e != nil {
		respond(w, 409, map[string]string{"error": e.Error()})
		return
	}
	respond(w, 202, q.Status())
}
func (s *Server) checkUpgrade(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	v, e := upgrade.Latest(ctx, &http.Client{Timeout: 8 * time.Second})
	if e != nil {
		respond(w, 502, map[string]string{"error": e.Error()})
		return
	}
	respond(w, 200, map[string]any{"release": v, "available": upgrade.Newer(v.Version, buildinfo.Version), "current": buildinfo.Version})
}
