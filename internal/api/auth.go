package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"tgguard/internal/state"
	"time"
)

const sessionCookie = "tg_guard_session"

type webSession struct {
	CSRF  string `json:"csrf"`
	Actor int64  `json:"actor"`
	Epoch string `json:"epoch"`
}

type actorKey struct{}
type sessionEpochKey struct{}

func actor(r *http.Request) int64 { id, _ := r.Context().Value(actorKey{}).(int64); return id }

// Epoch binds sessions AND tickets to the master credential, individual credential
// and explicit global revocation. Only hashes of high-entropy credentials are used.
func (s *Server) authEpoch(ctx context.Context, id int64) (string, error) {
	credential := s.Config.AdminToken
	if id != 0 {
		if s.Service.Runtime == nil || !s.Service.Runtime.IsAdmin(id) || s.Service.Store == nil {
			return "", fmt.Errorf("administrator revoked")
		}
		hash, err := s.Service.Store.WebCredential(ctx, id)
		if err != nil {
			return "", err
		}
		credential += ":" + hash
	}
	return s.credentialEpoch(ctx, credential)
}

func (s *Server) credentialEpoch(ctx context.Context, credential string) (string, error) {
	generation, err := s.Service.State.R.Get(ctx, "web:auth_epoch").Result()
	if err != nil && err != redis.Nil {
		return "", err
	}
	return state.Hash(credential + ":" + generation), nil
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
	epoch, err := s.authEpoch(r.Context(), session.Actor)
	if err != nil || !SecretEqual(epoch, session.Epoch) {
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
func (s *Server) createSession(w http.ResponseWriter, r *http.Request, actor int64, epoch string) {
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
	if e = s.Service.State.Put(r.Context(), "web:session:"+state.Hash(id), webSession{CSRF: csrf, Actor: actor, Epoch: epoch}, 8*time.Hour); e != nil {
		apiError(w, e)
		return
	}
	s.finishSession(w, r, id, csrf)
}
func (s *Server) finishSession(w http.ResponseWriter, r *http.Request, id, csrf string) {
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
	user := int64(0)
	credential := s.Config.AdminToken
	valid := SecretEqual(b.Token, s.Config.AdminToken)
	if !valid && s.Service.Runtime != nil && s.Service.Store != nil {
		parts := strings.SplitN(b.Token, ".", 2)
		if len(parts) == 2 {
			id, err := strconv.ParseInt(parts[0], 10, 64)
			if err == nil && id > 0 && s.Service.Runtime.IsAdmin(id) {
				hash, err := s.Service.Store.WebCredential(r.Context(), id)
				valid = err == nil && SecretEqual(hash, state.Hash(b.Token))
				if valid {
					user = id
					credential += ":" + hash
				}
			}
		}
	}
	if !valid {
		respond(w, 401, map[string]string{"error": "管理密钥不正确"})
		return
	}
	// Bind to the credential actually checked, not a later replacement credential.
	epoch, err := s.credentialEpoch(r.Context(), credential)
	if err != nil {
		apiError(w, err)
		return
	}
	s.createSession(w, r, user, epoch)
}
func (s *Server) ticket(w http.ResponseWriter, r *http.Request) {
	token, e := state.Token()
	if e != nil {
		apiError(w, e)
		return
	}
	epoch, e := s.authEpoch(r.Context(), actor(r))
	if e != nil {
		apiError(w, e)
		return
	}
	if authenticatedEpoch, ok := r.Context().Value(sessionEpochKey{}).(string); ok {
		// An in-flight request must not renew a session revoked after authentication.
		epoch = authenticatedEpoch
	}
	if e = s.Service.State.Put(r.Context(), "web:ticket:"+state.Hash(token), webSession{Actor: actor(r), Epoch: epoch}, time.Minute); e != nil {
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
	var ticket webSession
	key := "web:ticket:" + state.Hash(b.Ticket)
	raw, e := s.Service.State.R.Get(r.Context(), key).Result()
	if e != nil && e != redis.Nil {
		respond(w, 503, map[string]string{"error": "登录服务暂时不可用，请重试"})
		return
	}
	if e != nil || json.Unmarshal([]byte(raw), &ticket) != nil {
		respond(w, 401, map[string]string{"error": "登录链接已使用或过期"})
		return
	}
	epoch, e := s.authEpoch(r.Context(), ticket.Actor)
	if e != nil || !SecretEqual(epoch, ticket.Epoch) {
		respond(w, 401, map[string]string{"error": "登录授权已撤销，请重新登录"})
		return
	}
	ticket.CSRF = csrf
	payload, _ := json.Marshal(ticket)
	// Create session before consuming the one-use ticket, in the same Redis operation.
	ok, e := s.Service.State.R.Eval(r.Context(), `if redis.call('GET',KEYS[1])~=ARGV[2] then return 0 end
 redis.call('SET',KEYS[2],ARGV[1],'EX',28800)
 redis.call('DEL',KEYS[1]);return 1`, []string{key, "web:session:" + state.Hash(id)}, string(payload), raw).Int()
	if e != nil {
		respond(w, 503, map[string]string{"error": "登录服务暂时不可用，请重试"})
		return
	}
	if ok != 1 {
		respond(w, 401, map[string]string{"error": "登录链接已使用或过期，请重新打开"})
		return
	}
	s.finishSession(w, r, id, csrf)

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
