package api

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web/*
var panelFiles embed.FS
var panelAssets = func() fs.FS {
	f, e := fs.Sub(panelFiles, "web")
	if e != nil {
		panic(e)
	}
	return f
}()

func (s *Server) panel(w http.ResponseWriter, r *http.Request) {
	b, e := panelFiles.ReadFile("web/index.html")
	if e != nil {
		apiError(w, e)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b)
}
