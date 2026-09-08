package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"tgguard/internal/buildinfo"
	"time"

	"tgguard/internal/config"
	"tgguard/internal/domain"
	"tgguard/internal/service"
	"tgguard/internal/store"
)

type Server struct {
	Service *service.Service
	Config  config.Config
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.panel)
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(panelAssets))))
	mux.HandleFunc("POST /auth/login", s.withAuthTimeout(s.login))
	mux.HandleFunc("POST /auth/ticket", s.withAuthTimeout(s.exchangeTicket))
	mux.HandleFunc("GET /auth/session", s.withAuthTimeout(s.sessionInfo))
	mux.HandleFunc("POST /auth/logout", s.withAuthTimeout(s.logout))
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"status": "ok", "build": buildinfo.Info()})
	})
	mux.HandleFunc("GET /health/ready", s.ready)
	if s.Config.Mode == "webhook" {
		mux.HandleFunc("POST /telegram/webhook", s.webhook)
	}
	admin := http.NewServeMux()
	admin.HandleFunc("GET /api/v1/upgrades", s.upgrades)
	admin.HandleFunc("GET /api/v1/upgrades/check", s.checkUpgrade)
	admin.HandleFunc("POST /api/v1/upgrades", s.upgrades)
	admin.HandleFunc("POST /api/v1/panel-ticket", s.ticket)
	admin.HandleFunc("POST /api/v1/admin-credentials", s.adminCredential)
	admin.HandleFunc("POST /api/v1/sessions/revoke", s.revokeSessions)
	admin.HandleFunc("GET /api/v1/system", s.system)
	admin.HandleFunc("PUT /api/v1/system", s.system)
	admin.HandleFunc("POST /api/v1/system/test-ai", s.testAI)
	admin.HandleFunc("GET /api/v1/dashboard", s.dashboard)
	admin.HandleFunc("GET /api/v1/groups", s.groups)
	admin.HandleFunc("POST /api/v1/groups/{chat}/rules/test", s.testRules)
	admin.HandleFunc("GET /api/v1/rule-catalog", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, domain.BuiltinRules) })
	admin.HandleFunc("PUT /api/v1/groups/{chat}/authorization", s.authorization)
	admin.HandleFunc("GET /api/v1/groups/{chat}/settings", s.settings)
	admin.HandleFunc("PUT /api/v1/groups/{chat}/settings", s.settings)
	admin.HandleFunc("GET /api/v1/groups/{chat}/keywords", s.keywords)
	admin.HandleFunc("POST /api/v1/groups/{chat}/keywords", s.keywords)
	admin.HandleFunc("POST /api/v1/groups/{chat}/keywords/test", s.testKeywords)
	admin.HandleFunc("PUT /api/v1/groups/{chat}/keywords/{id}", s.keywords)
	admin.HandleFunc("DELETE /api/v1/groups/{chat}/keywords/{id}", s.keywords)
	admin.HandleFunc("GET /api/v1/groups/{chat}/lists", s.lists)
	admin.HandleFunc("POST /api/v1/groups/{chat}/lists", s.lists)
	admin.HandleFunc("DELETE /api/v1/groups/{chat}/lists", s.lists)
	admin.HandleFunc("GET /api/v1/groups/{chat}/logs", s.logs)
	admin.HandleFunc("GET /api/v1/groups/{chat}/users", s.users)
	admin.HandleFunc("GET /api/v1/groups/{chat}/users/{user}", s.userDetail)
	admin.HandleFunc("GET /api/v1/groups/{chat}/health", s.groupHealth)
	admin.HandleFunc("GET /api/v1/groups/{chat}/punishments", s.punishments)
	admin.HandleFunc("GET /api/v1/groups/{chat}/verifications", s.verifications)
	admin.HandleFunc("GET /api/v1/groups/{chat}/audits", s.audits)
	admin.HandleFunc("POST /api/v1/groups/{chat}/feedback", s.feedback)
	admin.HandleFunc("GET /api/v1/queue/dead", s.dead)
	admin.HandleFunc("POST /api/v1/queue/{update}/retry", s.retryDead)
	mux.Handle("/api/", s.authenticate(admin))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		mux.ServeHTTP(w, r)
	})
}
func SecretEqual(a, b string) bool {
	x, y := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return a != "" && b != "" && subtle.ConstantTimeCompare(x[:], y[:]) == 1
}
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timeout := 10 * time.Second
		if r.URL.Path == "/api/v1/system/test-ai" {
			timeout = 35 * time.Second
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		r = r.WithContext(ctx)
		if s.Service.State != nil {
			host := s.clientAddress(r)
			ok, e := s.Service.State.Limit(ctx, "api:rate:"+host, 120, time.Minute)
			if e != nil {
				respond(w, 503, map[string]string{"error": "rate limiter unavailable"})
				return
			}
			if !ok {
				respond(w, 429, map[string]string{"error": "rate limited"})
				return
			}
		}
		bearer := len(s.Config.AdminToken) >= 32 && SecretEqual(r.Header.Get("Authorization"), "Bearer "+s.Config.AdminToken)
		session, _, sessionOK := s.session(r)
		if !bearer && !sessionOK {
			respond(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		if r.Method != "GET" && (!s.validOrigin(r) || (!bearer && !SecretEqual(r.Header.Get("X-CSRF-Token"), session.CSRF))) {
			respond(w, 403, map[string]string{"error": "cross-origin writes forbidden"})
			return
		}
		if !bearer {
			ctx := context.WithValue(r.Context(), sessionEpochKey{}, session.Epoch)
			r = r.WithContext(store.WithAuditActor(context.WithValue(ctx, actorKey{}, session.Actor), session.Actor))
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if s.Service.Store.DB.PingContext(ctx) != nil || s.Service.State.R.Ping(ctx).Err() != nil {
		respond(w, 503, map[string]string{"status": "unavailable"})
		return
	}
	health, ok := s.Service.Health.Snapshot()
	queue, e := s.Service.Store.QueueHealth(ctx)
	if e != nil {
		respond(w, 503, map[string]string{"status": "unavailable"})
		return
	}
	code, status := 200, "ready"
	recovery, e := s.Service.Store.RecoveryHealth(ctx)
	if e != nil {
		respond(w, 503, map[string]string{"status": "unavailable"})
		return
	}
	if !ok || queue["oldest_pending_seconds"].(int64) >= 300 {
		code, status = 503, "unavailable"
	}
	respond(w, code, map[string]any{"status": status, "telegram": health, "queue": queue, "recovery": recovery})
}
func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	if !SecretEqual(r.Header.Get("X-Telegram-Bot-Api-Secret-Token"), s.Config.WebhookSecret) {
		respond(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	var u domain.Update
	if !decode(w, r, &u, 1<<20, false) {
		return
	}
	if u.ID <= 0 {
		respond(w, 400, map[string]string{"error": "invalid update_id"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if e := s.Service.Store.Enqueue(ctx, u); e != nil {
		apiError(w, e)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func decode(w http.ResponseWriter, r *http.Request, out any, size int64, strict bool) bool {
	r.Body = http.MaxBytesReader(w, r.Body, size)
	d := json.NewDecoder(r.Body)
	if strict {
		d.DisallowUnknownFields()
	}
	if e := d.Decode(out); e != nil {
		respond(w, 400, map[string]string{"error": "invalid JSON body"})
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		respond(w, 400, map[string]string{"error": "only one JSON object allowed"})
		return false
	}
	return true
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func apiError(w http.ResponseWriter, e error) {
	if errors.Is(e, sql.ErrNoRows) {
		respond(w, 404, map[string]string{"error": "not found"})
		return
	}
	slog.Error("management API error", "error", e)
	respond(w, 500, map[string]string{"error": "internal error"})
}
func chatID(w http.ResponseWriter, r *http.Request, global bool) (int64, bool) {
	id, e := strconv.ParseInt(r.PathValue("chat"), 10, 64)
	if e != nil || id > 0 || id == 0 && !global {
		respond(w, 400, map[string]string{"error": "chat must be a negative Telegram group ID (0 only for global lists)"})
		return 0, false
	}
	return id, true
}
func cursor(r *http.Request) int64 {
	n, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	if n == 0 {
		return 9223372036854775807
	}
	return n
}
func (s *Server) rows(w http.ResponseWriter, r *http.Request, q store.View, args ...any) {
	rows, e := s.Service.Store.View(r.Context(), q, args...)
	if e != nil {
		apiError(w, e)
		return
	}
	respond(w, 200, rows)
}
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	s.rows(w, r, store.ViewDashboard)
}
func (s *Server) groups(w http.ResponseWriter, r *http.Request) {
	s.rows(w, r, store.ViewGroups, cursor(r))
}
func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	chat, ok := s.groupID(w, r, false)
	if !ok {
		return
	}
	settings, e := s.Service.Store.Settings(r.Context(), chat)
	if e != nil {
		apiError(w, e)
		return
	}
	if r.Method == "GET" {
		respond(w, 200, settings)
		return
	}
	var raw json.RawMessage
	if !decode(w, r, &raw, 65536, true) {
		return
	}
	probe := settings
	if e = domain.ApplySettingsPatch(&probe, raw); e != nil {
		respond(w, 400, map[string]string{"error": e.Error()})
		return
	}
	if e = s.Service.Store.ChangeSettings(r.Context(), chat, actor(r), func(v *domain.Settings) error { return domain.ApplySettingsPatch(v, raw) }); e != nil {
		apiError(w, e)
		return
	}
	settings, e = s.Service.Store.Settings(r.Context(), chat)
	if e != nil {
		apiError(w, e)
		return
	}

	respond(w, 200, settings)
}
func (s *Server) keywords(w http.ResponseWriter, r *http.Request) {
	chat, ok := s.groupID(w, r, false)
	if !ok {
		return
	}
	if r.Method == "GET" {
		ks, e := s.Service.Store.Keywords(r.Context(), chat)
		if e != nil {
			apiError(w, e)
			return
		}
		respond(w, 200, ks)
		return
	}
	id := int64(0)
	if r.PathValue("id") != "" {
		var e error
		id, e = strconv.ParseInt(r.PathValue("id"), 10, 64)
		if e != nil || id <= 0 {
			respond(w, 400, map[string]string{"error": "invalid keyword ID"})
			return
		}
	}
	if r.Method == "DELETE" {
		if e := s.Service.Store.DeleteKeyword(r.Context(), chat, id, actor(r)); e != nil {
			apiError(w, e)
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
		return
	}
	var k domain.Keyword
	if !decode(w, r, &k, 65536, true) {
		return
	}
	k.ChatID = chat
	k.ID = id
	if e := k.Validate(); e != nil {
		respond(w, 400, map[string]string{"error": e.Error()})
		return
	}
	id, e := s.Service.Store.SaveKeyword(r.Context(), k, actor(r))
	if e != nil {
		apiError(w, e)
		return
	}
	respond(w, 200, map[string]int64{"id": id})
}
func (s *Server) lists(w http.ResponseWriter, r *http.Request) {
	chat, ok := s.groupID(w, r, true)
	if !ok {
		return
	}
	if r.Method == "GET" {
		s.rows(w, r, store.ViewLists, chat, cursor(r))
		return
	}
	var l domain.ListEntry
	if !decode(w, r, &l, 65536, true) {
		return
	}
	l.ChatID = chat
	l.Username = strings.TrimPrefix(l.Username, "@")
	if r.Method != "DELETE" && l.UserID <= 0 {
		respond(w, 400, map[string]string{"error": "请填写数字用户 ID；用户名不能作为授权身份"})
		return
	}
	if e := l.Validate(); e != nil {
		respond(w, 400, map[string]string{"error": e.Error()})
		return
	}
	if e := s.Service.Store.SaveList(r.Context(), l, actor(r), r.Method == "DELETE"); e != nil {
		apiError(w, e)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	chat, ok := s.groupID(w, r, false)
	if !ok {
		return
	}
	s.rows(w, r, store.ViewLogs, chat, cursor(r))
}
func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	chat, ok := s.groupID(w, r, false)
	if !ok {
		return
	}
	query := r.URL.Query().Get("q")
	if len(query) > 100 {
		respond(w, 400, map[string]string{"error": "query too long"})
		return
	}
	pattern := "%" + query + "%"
	s.rows(w, r, store.ViewUsers, chat, cursor(r), query, pattern, pattern)
}
func (s *Server) punishments(w http.ResponseWriter, r *http.Request) {
	chat, ok := s.groupID(w, r, false)
	if !ok {
		return
	}
	s.rows(w, r, store.ViewPunishments, chat, cursor(r))
}
func (s *Server) verifications(w http.ResponseWriter, r *http.Request) {
	chat, ok := s.groupID(w, r, false)
	if !ok {
		return
	}
	s.rows(w, r, store.ViewVerifications, chat)
}
func (s *Server) audits(w http.ResponseWriter, r *http.Request) {
	chat, ok := s.groupID(w, r, false)
	if !ok {
		return
	}
	s.rows(w, r, store.ViewAudits, chat, cursor(r))
}
func (s *Server) feedback(w http.ResponseWriter, r *http.Request) {
	chat, ok := s.groupID(w, r, false)
	if !ok {
		return
	}
	var body struct {
		LogID int64  `json:"log_id"`
		Note  string `json:"note"`
	}
	if !decode(w, r, &body, 65536, true) {
		return
	}
	if body.LogID <= 0 || strings.TrimSpace(body.Note) == "" || len(body.Note) > 500 {
		respond(w, 400, map[string]string{"error": "invalid feedback"})
		return
	}
	if e := s.Service.Store.Feedback(r.Context(), chat, body.LogID, actor(r), body.Note); e != nil {
		apiError(w, e)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) dead(w http.ResponseWriter, r *http.Request) {
	s.rows(w, r, store.ViewDeadUpdates, cursor(r))
}

func (s *Server) groupID(w http.ResponseWriter, r *http.Request, global bool) (int64, bool) {
	chat, ok := chatID(w, r, global)
	if !ok || chat == 0 {
		return chat, ok
	}
	exists, e := s.Service.Store.GroupExists(r.Context(), chat)
	if e != nil {
		apiError(w, e)
		return 0, false
	}
	if !exists {
		respond(w, 404, map[string]string{"error": "群组尚未接入"})
		return 0, false
	}
	if r.Method != "GET" {
		if e := s.Service.Store.RequireAuthorized(r.Context(), chat); e != nil {
			respond(w, 403, map[string]string{"error": e.Error()})
			return 0, false
		}
	}
	return chat, true
}
