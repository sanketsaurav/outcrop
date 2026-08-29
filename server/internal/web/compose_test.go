package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanketsaurav/outcrop/server/internal/api"
	"github.com/sanketsaurav/outcrop/server/internal/config"
	"github.com/sanketsaurav/outcrop/server/internal/store"
	"github.com/sanketsaurav/outcrop/server/internal/web"
)

// TestMuxComposition registers the API and web routers on one ServeMux exactly
// as main() does — a pattern conflict panics at registration time, so this is
// the regression test for route overlap between the two.
func TestMuxComposition(t *testing.T) {
	base, _ := url.Parse("http://notes.test")
	dir := t.TempDir()
	cfg := &config.Config{
		APIKey: "test-api-key-0123456789", BaseURL: base, DataDir: dir,
		SiteName: "Test", OGAccent: "#6c5ce7", MaxNoteBytes: 2 << 20, MaxAssetBytes: 1 << 20,
	}
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	blobs, err := store.NewBlobs(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	api.New(cfg, st, blobs).Register(mux)
	websrv, err := web.New(cfg, st, blobs)
	if err != nil {
		t.Fatal(err)
	}
	websrv.Register(mux) // panics here if any pattern conflicts

	// Unknown API routes answer JSON, not the styled 404 page.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v2/nope", nil))
	if rec.Code != 404 || !strings.Contains(rec.Header().Get("Content-Type"), "json") {
		t.Fatalf("api 404: got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}

	// Public 404 stays styled HTML.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/nope", nil))
	if rec.Code != 404 || !strings.Contains(rec.Header().Get("Content-Type"), "html") {
		t.Fatalf("web 404: got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}
