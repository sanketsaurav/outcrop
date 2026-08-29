// Package api implements the bearer-authenticated /api/v1 surface the plugin talks to.
package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/sanketsaurav/outcrop/server/internal/config"
	"github.com/sanketsaurav/outcrop/server/internal/limit"
	"github.com/sanketsaurav/outcrop/server/internal/store"
)

type Server struct {
	cfg       *config.Config
	st        *store.Store
	blobs     *store.Blobs
	authLimit *limit.Limiter
	keyDigest [32]byte
}

func New(cfg *config.Config, st *store.Store, blobs *store.Blobs) *Server {
	return &Server{
		cfg:       cfg,
		st:        st,
		blobs:     blobs,
		authLimit: limit.New(10, time.Minute),
		keyDigest: sha256.Sum256([]byte(cfg.APIKey)),
	}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/ping", s.auth(s.ping))
	mux.HandleFunc("POST /api/v1/notes", s.auth(s.createNote))
	mux.HandleFunc("GET /api/v1/notes", s.auth(s.listNotes))
	mux.HandleFunc("PUT /api/v1/notes/{id}", s.auth(s.updateNote))
	mux.HandleFunc("DELETE /api/v1/notes/{id}", s.auth(s.deleteNote))
	mux.HandleFunc("POST /api/v1/notes/{id}/rotate", s.auth(s.rotateNote))
	mux.HandleFunc("PUT /api/v1/notes/{id}/passcode", s.auth(s.setPasscode))
	mux.HandleFunc("DELETE /api/v1/notes/{id}/passcode", s.auth(s.clearPasscode))
	mux.HandleFunc("GET /api/v1/assets/{hash}", s.auth(s.assetExists)) // also serves HEAD
	mux.HandleFunc("POST /api/v1/assets/{hash}", s.auth(s.uploadAsset))
	mux.HandleFunc("PUT /api/v1/theme", s.auth(s.putTheme))
	mux.HandleFunc("GET /api/v1/theme", s.auth(s.getTheme))
	// Unknown /api/* paths fall through to the web catch-all, which answers
	// them with a JSON 404 (a "/api/" pattern here would conflict with the
	// web mux's "POST /{slug}/unlock" — both match /api/unlock).
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if len(tok) > len(prefix) && tok[:len(prefix)] == prefix {
			tok = tok[len(prefix):]
		}
		got := sha256.Sum256([]byte(tok))
		if subtle.ConstantTimeCompare(got[:], s.keyDigest[:]) != 1 {
			if !s.authLimit.Allow("auth:" + limit.ClientIP(r, s.cfg.TrustProxy)) {
				apiError(w, http.StatusTooManyRequests, "too many failed auth attempts, slow down")
				return
			}
			apiError(w, http.StatusUnauthorized, "invalid or missing API key")
			return
		}
		next(w, r)
	}
}

func (s *Server) ping(w http.ResponseWriter, r *http.Request) {
	count, err := s.st.CountNotes()
	if err != nil {
		s.internalError(w, "counting notes", err)
		return
	}
	theme, err := s.st.GetTheme()
	if err != nil {
		s.internalError(w, "loading theme", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":          s.cfg.Version,
		"notes":            count,
		"theme_updated_at": theme.UpdatedAt,
	})
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func apiError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) internalError(w http.ResponseWriter, what string, err error) {
	slog.Error("api: "+what, "err", err)
	apiError(w, http.StatusInternalServerError, "internal error")
}

var errBodyTooLarge = errors.New("request body too large")

// readJSON decodes the request body into v, enforcing a byte cap. An empty
// body decodes to the zero value (used by rotate).
func readJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	err := json.NewDecoder(r.Body).Decode(v)
	if err == nil || errors.Is(err, io.EOF) {
		return nil
	}
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return errBodyTooLarge
	}
	return fmt.Errorf("invalid JSON: %w", err)
}

func (s *Server) handleReadErr(w http.ResponseWriter, err error) {
	if errors.Is(err, errBodyTooLarge) {
		apiError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("note payload exceeds the %d MB limit (MAX_NOTE_MB)", s.cfg.MaxNoteBytes>>20))
		return
	}
	apiError(w, http.StatusUnprocessableEntity, err.Error())
}

func (s *Server) noteURL(slug string) string {
	return s.cfg.AbsoluteURL("/" + slug)
}

func (s *Server) gcBlobs(hashes []string) {
	for _, h := range hashes {
		if err := s.blobs.Remove(h); err != nil {
			slog.Warn("api: removing garbage-collected blob", "hash", h, "err", err)
		}
	}
}
