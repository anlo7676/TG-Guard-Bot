package api

import (
	"net/http"
	"tgguard/internal/rules"
)

func (s *Server) testKeywords(w http.ResponseWriter, r *http.Request) {
	chat, ok := s.groupID(w, r, false)
	if !ok {
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &body, 16384, true) {
		return
	}
	if len(body.Text) > 8000 {
		respond(w, 400, map[string]string{"error": "测试文本过长"})
		return
	}
	ks, e := s.Service.Store.Keywords(r.Context(), chat)
	if e != nil {
		apiError(w, e)
		return
	}
	settings, e := s.Service.Store.Settings(r.Context(), chat)
	if e != nil {
		apiError(w, e)
		return
	}
	out := []map[string]any{}
	selected := 0
	for _, k := range ks {
		matched := rules.KeywordMatch(k, body.Text)
		send := matched && settings.KeywordEnabled && (selected == 0 || settings.KeywordAll && selected < 5)
		if send {
			selected++
		}
		out = append(out, map[string]any{"id": k.ID, "keyword": k.Keyword, "matched": matched, "would_reply": send})
	}
	respond(w, 200, map[string]any{"keyword_enabled": settings.KeywordEnabled, "results": out})
}
