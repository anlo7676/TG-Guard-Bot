package api

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"tgguard/internal/state"
	"time"
)

const sessionCookie = "tg_guard_session"

type webSession struct {
	CSRF string `json:"csrf"`
}

func (s *Server) validOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, e := url.Parse(origin)
	if e != nil || u.User != nil || u.Host != r.Host || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if s.Service.Runtime != nil {
		p, _ := url.Parse(s.Service.Runtime.Snapshot().PanelURL)
		if p != nil && p.Host == r.Host && p.Scheme == "https" {
			scheme = "https"
		}
	}
	return u.Scheme == scheme
}
func (s *Server) session(r *http.Request) (webSession, string, bool) {
	var session webSession
	c, e := r.Cookie(sessionCookie)
	if e != nil || len(c.Value) != 32 || s.Service.State == nil {
		return session, "", false
	}
	key := "web:session:" + state.Hash(c.Value)
	if e = s.Service.State.Get(r.Context(), key, &session); e != nil || session.CSRF == "" {
		return session, "", false
	}
	return session, key, true
}
func (s *Server) loginLimit(w http.ResponseWriter, r *http.Request) bool {
	if !s.validOrigin(r) {
		respond(w, 403, map[string]string{"error": "不允许跨站登录"})
		return false
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	ok, e := s.Service.State.Limit(r.Context(), "web:login:"+host, 10, 5*time.Minute)
	if e != nil {
		respond(w, 503, map[string]string{"error": "登录服务不可用"})
		return false
	}
	if !ok {
		respond(w, 429, map[string]string{"error": "登录尝试过多，请稍后重试"})
		return false
	}
	return true
}
func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	id, e := state.Token()
	if e != nil {
		apiError(w, e)
		return
	}
	csrf, e := state.Token()
	if e != nil {
		apiError(w, e)
		return
	}
	if e = s.Service.State.Put(r.Context(), "web:session:"+state.Hash(id), webSession{CSRF: csrf}, 8*time.Hour); e != nil {
		apiError(w, e)
		return
	}
	secure := r.TLS != nil
	if s.Service.Runtime != nil {
		p, _ := url.Parse(s.Service.Runtime.Snapshot().PanelURL)
		secure = secure || (p != nil && p.Scheme == "https" && p.Host == r.Host)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: id, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: 8 * 3600})
	respond(w, 200, map[string]string{"csrf": csrf})
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimit(w, r) {
		return
	}
	var b struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &b, 8192, true) {
		return
	}
	if !SecretEqual(b.Token, s.Config.AdminToken) {
		respond(w, 401, map[string]string{"error": "管理密钥不正确"})
		return
	}
	s.createSession(w, r)
}
func (s *Server) ticket(w http.ResponseWriter, r *http.Request) {
	token, e := state.Token()
	if e != nil {
		apiError(w, e)
		return
	}
	if e = s.Service.State.R.Set(r.Context(), "web:ticket:"+state.Hash(token), "1", time.Minute).Err(); e != nil {
		apiError(w, e)
		return
	}
	respond(w, 200, map[string]string{"ticket": token})
}
func (s *Server) exchangeTicket(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimit(w, r) {
		return
	}
	var b struct {
		Ticket string `json:"ticket"`
	}
	if !decode(w, r, &b, 4096, true) {
		return
	}
	if len(b.Ticket) != 32 {
		respond(w, 401, map[string]string{"error": "登录链接无效"})
		return
	}
	v, e := s.Service.State.R.GetDel(r.Context(), "web:ticket:"+state.Hash(b.Ticket)).Result()
	if e != nil || v != "1" {
		respond(w, 401, map[string]string{"error": "登录链接已使用或过期，请重新打开"})
		return
	}
	s.createSession(w, r)
}
func (s *Server) sessionInfo(w http.ResponseWriter, r *http.Request) {
	v, _, ok := s.session(r)
	if !ok {
		respond(w, 401, map[string]string{"error": "请先登录"})
		return
	}
	respond(w, 200, map[string]string{"csrf": v.CSRF})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	v, key, ok := s.session(r)
	if !s.validOrigin(r) || !ok || !SecretEqual(r.Header.Get("X-CSRF-Token"), v.CSRF) {
		respond(w, 403, map[string]string{"error": "无效的登录会话"})
		return
	}
	if e := s.Service.State.R.Del(r.Context(), key).Err(); e != nil {
		apiError(w, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1, SameSite: http.SameSiteStrictMode})
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) withAuthTimeout(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		next(w, r.WithContext(ctx))
	}
}
