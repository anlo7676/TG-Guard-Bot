package api

import (
	"encoding/json"
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
		ForwardSource string `json:"forward_source"`
		Quote         string `json:"quote"`
		Text          string `json:"text"`
		NewMember     bool   `json:"new_member"`
	}
	if !decode(w, r, &b, 16384, true) {
		return
	}
	if strings.TrimSpace(b.Text) == "" || len(b.Text)+len(b.ForwardSource)+len(b.Quote) > 8000 {
		respond(w, 400, map[string]string{"error": "请输入测试文字，最多 8000 字节"})
		return
	}
	v, e := s.Service.Store.Settings(r.Context(), chat)
	if e != nil {
		apiError(w, e)
		return
	}
	m := domain.Message{Text: b.Text}
	if b.ForwardSource != "" {
		m.Forward, _ = json.Marshal(domain.MessageOrigin{Type: "hidden_user", HiddenName: b.ForwardSource})
	}
	if b.Quote != "" {
		m.Quote = &domain.TextQuote{Text: b.Quote}
		if b.ForwardSource == "" {
			m.ExternalReply = &domain.ExternalReply{}
		}
	}
	n := rules.Normalize(m)
	n.IsNew = b.NewMember
	n.FirstMessage = b.NewMember
	risk := rules.Evaluate(n, v)
	d := rules.Decide(risk, nil, v, 0, false)
	if !v.ModerationEnabled {
		d = domain.Decision{Action: "allow", Reason: "moderation_disabled"}
	}
	respond(w, 200, map[string]any{"risk": risk, "decision": d, "moderation_enabled": v.ModerationEnabled, "ai_eligible": v.ModerationEnabled && v.AIEnabled && risk.Score >= v.AIThreshold, "ai_threshold": v.AIThreshold, "direct_threshold": v.DirectThreshold})
}
