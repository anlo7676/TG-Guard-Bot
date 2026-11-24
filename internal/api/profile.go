package api

import "net/http"

func (s *Server) profileCheck(w http.ResponseWriter, r *http.Request) {
	chat, ok := s.groupID(w, r, false)
	if !ok {
		return
	}
	var body struct {
		UserID int64 `json:"user_id"`
		Apply  bool  `json:"apply"`
		Learn  bool  `json:"learn"`
	}
	if !decode(w, r, &body, 1024, true) {
		return
	}
	if body.UserID <= 0 {
		respond(w, 400, map[string]string{"error": "invalid user_id"})
		return
	}
	result, err := s.Service.ProfileCheck(r.Context(), chat, body.UserID, body.Apply, body.Learn, actor(r))
	if err != nil {
		apiError(w, err)
		return
	}
	respond(w, 200, result)
}
