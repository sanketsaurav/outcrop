package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/sanketsaurav/outcrop/server/internal/store"
)

type noteBody struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	HTML        string   `json:"html"`
	Noindex     bool     `json:"noindex"`
	Slug        string   `json:"slug"`     // create only
	Passcode    string   `json:"passcode"` // create only
	Assets      []string `json:"assets"`
}

func (b *noteBody) validate() (store.NoteInput, string) {
	b.Title = strings.TrimSpace(b.Title)
	if b.Title == "" {
		return store.NoteInput{}, "title is required"
	}
	if len(b.Title) > 500 {
		return store.NoteInput{}, "title is too long"
	}
	for _, h := range b.Assets {
		if !store.ValidAssetHash(h) {
			return store.NoteInput{}, "assets must be lowercase hex sha256 hashes"
		}
	}
	return store.NoteInput{
		Title:       b.Title,
		Description: strings.TrimSpace(b.Description),
		HTML:        b.HTML,
		Noindex:     b.Noindex,
		Assets:      b.Assets,
	}, ""
}

type noteResp struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	Protected bool   `json:"protected"`
	Noindex   bool   `json:"noindex"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

func (s *Server) noteResp(n *store.Note) noteResp {
	return noteResp{
		ID:        n.ID,
		Slug:      n.Slug,
		URL:       s.noteURL(n.Slug),
		Title:     n.Title,
		Protected: n.Protected(),
		Noindex:   n.Noindex,
		SizeBytes: n.SizeBytes,
		CreatedAt: n.CreatedAt,
		UpdatedAt: n.UpdatedAt,
	}
}

func (s *Server) createNote(w http.ResponseWriter, r *http.Request) {
	var body noteBody
	if err := readJSON(w, r, s.cfg.MaxNoteBytes, &body); err != nil {
		s.handleReadErr(w, err)
		return
	}
	in, problem := body.validate()
	if problem != "" {
		apiError(w, http.StatusUnprocessableEntity, problem)
		return
	}
	if body.Slug != "" && !store.ValidCustomSlug(body.Slug) {
		apiError(w, http.StatusUnprocessableEntity,
			"custom slug must be lowercase letters/digits/hyphens (max 80 chars) and not a reserved path")
		return
	}
	note, err := s.st.CreateNote(in, body.Slug)
	switch {
	case errors.Is(err, store.ErrSlugTaken):
		apiError(w, http.StatusConflict, "slug is already taken")
		return
	case errors.Is(err, store.ErrUnknownAsset):
		apiError(w, http.StatusUnprocessableEntity, "note references an asset that was never uploaded")
		return
	case err != nil:
		s.internalError(w, "creating note", err)
		return
	}
	if pc := store.NormalizePasscode(body.Passcode); pc != "" {
		if err := s.st.SetPasscode(note.ID, pc); err != nil {
			s.internalError(w, "setting passcode", err)
			return
		}
		note, err = s.st.GetNote(note.ID)
		if err != nil {
			s.internalError(w, "reloading note", err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, s.noteResp(note))
}

func (s *Server) updateNote(w http.ResponseWriter, r *http.Request) {
	var body noteBody
	if err := readJSON(w, r, s.cfg.MaxNoteBytes, &body); err != nil {
		s.handleReadErr(w, err)
		return
	}
	in, problem := body.validate()
	if problem != "" {
		apiError(w, http.StatusUnprocessableEntity, problem)
		return
	}
	note, gc, err := s.st.UpdateNote(r.PathValue("id"), in)
	switch {
	case errors.Is(err, store.ErrNotFound):
		apiError(w, http.StatusNotFound, "no note with that id")
		return
	case errors.Is(err, store.ErrUnknownAsset):
		apiError(w, http.StatusUnprocessableEntity, "note references an asset that was never uploaded")
		return
	case err != nil:
		s.internalError(w, "updating note", err)
		return
	}
	s.gcBlobs(gc)
	writeJSON(w, http.StatusOK, s.noteResp(note))
}

func (s *Server) deleteNote(w http.ResponseWriter, r *http.Request) {
	gc, err := s.st.DeleteNote(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		apiError(w, http.StatusNotFound, "no note with that id")
		return
	}
	if err != nil {
		s.internalError(w, "deleting note", err)
		return
	}
	s.gcBlobs(gc)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) rotateNote(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Slug string `json:"slug"`
	}
	if err := readJSON(w, r, 4096, &body); err != nil {
		s.handleReadErr(w, err)
		return
	}
	if body.Slug != "" && !store.ValidCustomSlug(body.Slug) {
		apiError(w, http.StatusUnprocessableEntity,
			"custom slug must be lowercase letters/digits/hyphens (max 80 chars) and not a reserved path")
		return
	}
	note, err := s.st.RotateSlug(r.PathValue("id"), body.Slug)
	switch {
	case errors.Is(err, store.ErrNotFound):
		apiError(w, http.StatusNotFound, "no note with that id")
		return
	case errors.Is(err, store.ErrSlugTaken):
		apiError(w, http.StatusConflict, "slug is already taken")
		return
	case err != nil:
		s.internalError(w, "rotating slug", err)
		return
	}
	writeJSON(w, http.StatusOK, s.noteResp(note))
}

func (s *Server) setPasscode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Passcode string `json:"passcode"`
	}
	if err := readJSON(w, r, 4096, &body); err != nil {
		s.handleReadErr(w, err)
		return
	}
	pc := store.NormalizePasscode(body.Passcode)
	if len(pc) < 4 {
		apiError(w, http.StatusUnprocessableEntity, "passcode must be at least 4 characters")
		return
	}
	err := s.st.SetPasscode(r.PathValue("id"), pc)
	if errors.Is(err, store.ErrNotFound) {
		apiError(w, http.StatusNotFound, "no note with that id")
		return
	}
	if err != nil {
		s.internalError(w, "setting passcode", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) clearPasscode(w http.ResponseWriter, r *http.Request) {
	err := s.st.ClearPasscode(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		apiError(w, http.StatusNotFound, "no note with that id")
		return
	}
	if err != nil {
		s.internalError(w, "clearing passcode", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listNotes(w http.ResponseWriter, r *http.Request) {
	notes, err := s.st.ListNotes()
	if err != nil {
		s.internalError(w, "listing notes", err)
		return
	}
	out := make([]noteResp, 0, len(notes))
	for _, n := range notes {
		out = append(out, s.noteResp(n))
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": out})
}
