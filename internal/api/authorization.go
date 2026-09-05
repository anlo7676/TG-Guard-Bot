package api

import (
	"net/http"
)

func (s *Server) authorization(w http.ResponseWriter, r *http.Request) {
	chat, ok := chatID(w, r, false)
	if !ok {
		return
	}
	var in struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if !decode(w, r, &in, 4096, true) {
		return
	}
	if e := s.Service.Store.AuthorizeGroup(r.Context(), chat, 0, in.Status, in.Reason); e != nil {
		respond(w, 400, map[string]string{"error": e.Error()})
		return
	}
	respond(w, 200, map[string]string{"status": in.Status})
}
