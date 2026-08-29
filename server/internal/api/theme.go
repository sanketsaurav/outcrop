package api

import (
	"net/http"

	"github.com/sanketsaurav/outcrop/server/internal/store"
)

func (s *Server) putTheme(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CSS  string `json:"css"`
		JS   string `json:"js"`
		Head string `json:"head"`
	}
	if err := readJSON(w, r, s.cfg.MaxNoteBytes, &body); err != nil {
		s.handleReadErr(w, err)
		return
	}
	if err := s.st.SetTheme(store.Theme{CSS: body.CSS, JS: body.JS, Head: body.Head}); err != nil {
		s.internalError(w, "saving theme", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getTheme(w http.ResponseWriter, r *http.Request) {
	t, err := s.st.GetTheme()
	if err != nil {
		s.internalError(w, "loading theme", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"css":        t.CSS,
		"js":         t.JS,
		"head":       t.Head,
		"updated_at": t.UpdatedAt,
	})
}
