package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/sanketsaurav/outcrop/server/internal/store"
)

// assetExists answers HEAD/GET /api/v1/assets/{hash} with 204 or 404, no body.
func (s *Server) assetExists(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !store.ValidAssetHash(hash) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	exists, err := s.st.AssetExists(hash)
	if err != nil {
		s.internalError(w, "checking asset", err)
		return
	}
	if exists {
		w.WriteHeader(http.StatusNoContent)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *Server) uploadAsset(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !store.ValidAssetHash(hash) {
		apiError(w, http.StatusUnprocessableEntity, "asset path must be a lowercase hex sha256")
		return
	}
	ext := r.URL.Query().Get("ext")
	mime, ok := store.AssetMIME(ext)
	if !ok {
		apiError(w, http.StatusUnprocessableEntity, fmt.Sprintf("file extension %q is not allowed", ext))
		return
	}

	if exists, err := s.st.AssetExists(hash); err != nil {
		s.internalError(w, "checking asset", err)
		return
	} else if exists {
		io.Copy(io.Discard, r.Body)
		writeJSON(w, http.StatusOK, map[string]string{"url": s.assetURL(hash, ext)})
		return
	}

	body := http.MaxBytesReader(w, r.Body, s.cfg.MaxAssetBytes)
	size, err := s.blobs.Save(hash, body)
	if err != nil {
		var mbe *http.MaxBytesError
		switch {
		case errors.As(err, &mbe):
			apiError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("asset exceeds the %d MB limit (MAX_ASSET_MB)", s.cfg.MaxAssetBytes>>20))
		case errors.Is(err, store.ErrHashMismatch):
			apiError(w, http.StatusUnprocessableEntity, "uploaded bytes do not match the declared hash")
		default:
			s.internalError(w, "saving blob", err)
		}
		return
	}
	if err := s.st.InsertAsset(hash, ext, mime, size); err != nil {
		s.internalError(w, "recording asset", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"url": s.assetURL(hash, ext)})
}

func (s *Server) assetURL(hash, ext string) string {
	return s.cfg.AbsoluteURL("/a/" + hash + "." + ext)
}
