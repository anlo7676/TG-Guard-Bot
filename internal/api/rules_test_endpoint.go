package api

import (
	"net/http"
	"strings"
	"tgguard/internal/domain"
	"tgguard/internal/rules"
)

// Uses saved settings without sending messages, invoking AI, or changing counters.
func (s *Server) testRules(w http.ResponseWriter, r *http.Request) {
	chat, ok := s.groupID(w, r, false)
	if !ok {
		return
	}
	var b struct {
		Text      string `json:"text"`
		NewMember bool   `json:"new_member"`
	}
	if !decode(w, r, &b, 16384, true) {
		return
	}
	if strings.TrimSpace(b.Text) == "" || len(b.Text) > 8000 {
		respond(w, 400, map[string]string{"error": "请输入测试文字，最多 8000 字节"})
		return
	}
	v, e := s.Service.Store.Settings(r.Context(), chat)
	if e != nil {
		apiError(w, e)
		return
	}
	n := rules.Normalize(domain.Message{Text: b.Text})
	n.IsNew = b.NewMember
	n.FirstMessage = b.NewMember
	risk := rules.Evaluate(n, v)
	d := rules.Decide(risk, nil, v, 0, false)
	if !v.ModerationEnabled {
		d = domain.Decision{Action: "allow", Reason: "moderation_disabled"}
	}
	respond(w, 200, map[string]any{"risk": risk, "decision": d, "moderation_enabled": v.ModerationEnabled, "ai_eligible": v.ModerationEnabled && v.AIEnabled && risk.Score >= v.AIThreshold && risk.Score < v.DirectThreshold, "ai_threshold": v.AIThreshold, "direct_threshold": v.DirectThreshold})
}
