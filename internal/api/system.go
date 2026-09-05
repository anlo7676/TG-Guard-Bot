package api

import (
	"context"
	"net/http"
	"tgguard/internal/domain"
	"tgguard/internal/settings"
	"time"
)

func (s *Server) system(w http.ResponseWriter, r *http.Request) {
	if s.Service.Runtime == nil {
		respond(w, 503, map[string]string{"error": "系统配置尚未初始化"})
		return
	}
	if r.Method == "GET" {
		respond(w, 200, map[string]any{"settings": s.Service.Runtime.Snapshot().Public(), "bot": map[string]any{"id": s.Service.Bot.ID, "username": s.Service.Bot.Username}})
		return
	}
	var body struct {
		settings.Config
		ClearKey bool `json:"clear_key"`
	}
	if !decode(w, r, &body, 65536, true) {
		return
	}
	// Validate errors are exposed without logging or returning the supplied secret.
	probe := body.Config
	if !body.ClearKey && probe.AI.APIKey == "" {
		probe.AI.APIKey = s.Service.Runtime.Snapshot().AI.APIKey
	}
	if body.ClearKey {
		probe.AI.APIKey = ""
	}
	if e := probe.Validate(); e != nil {
		respond(w, 400, map[string]string{"error": e.Error()})
		return
	}
	if e := s.Service.Runtime.Save(r.Context(), body.Config, body.ClearKey); e != nil {
		apiError(w, e)
		return
	}
	respond(w, 200, s.Service.Runtime.Snapshot().Public())
}
func (s *Server) testAI(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if s.Service.AI == nil {
		respond(w, 400, map[string]string{"error": "AI 未配置"})
		return
	}
	tester, ok := s.Service.AI.(interface {
		Test(context.Context, domain.Normalized, domain.Risk) (domain.AIResult, error)
	})
	if !ok {
		respond(w, 503, map[string]string{"error": "当前 Provider 不支持连接测试"})
		return
	}
	result, e := tester.Test(ctx, domain.Normalized{ChatID: 0, Text: "大家好，请问 Go 如何创建一个 HTTP 服务？", URLs: []string{}, Mentions: []string{}}, domain.Risk{Matches: []domain.Match{}})
	if e != nil {
		respond(w, 400, map[string]string{"error": e.Error()})
		return
	}
	respond(w, 200, result)
}
