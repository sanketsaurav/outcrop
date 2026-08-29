package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanketsaurav/outcrop/server/internal/config"
	"github.com/sanketsaurav/outcrop/server/internal/store"
)

const testKey = "test-api-key-0123456789abcdef"

func newTestAPI(t *testing.T) *http.ServeMux {
	t.Helper()
	base, _ := url.Parse("http://notes.test")
	dir := t.TempDir()
	cfg := &config.Config{
		APIKey: testKey, BaseURL: base, DataDir: dir, SiteName: "Test Notes",
		OGAccent: "#6c5ce7", MaxNoteBytes: 2 << 20, MaxAssetBytes: 1 << 20, Version: "test",
	}
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	blobs, err := store.NewBlobs(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(cfg, st, blobs).Register(mux)
	return mux
}

func do(t *testing.T, mux *http.ServeMux, method, path string, body any, authed bool) *httptest.ResponseRecorder {
	t.Helper()
	var rd *bytes.Reader
	switch b := body.(type) {
	case nil:
		rd = bytes.NewReader(nil)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		j, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(j)
	}
	req := httptest.NewRequest(method, path, rd)
	if authed {
		req.Header.Set("Authorization", "Bearer "+testKey)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestAuth(t *testing.T) {
	mux := newTestAPI(t)
	if rec := do(t, mux, "GET", "/api/v1/ping", nil, false); rec.Code != 401 {
		t.Fatalf("no auth: want 401, got %d", rec.Code)
	}
	req := httptest.NewRequest("GET", "/api/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("wrong key: want 401, got %d", rec.Code)
	}
	if rec := do(t, mux, "GET", "/api/v1/ping", nil, true); rec.Code != 200 {
		t.Fatalf("good key: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestNoteLifecycle(t *testing.T) {
	mux := newTestAPI(t)

	// Upload an asset, then reference it.
	blob := []byte("fake image bytes")
	sum := sha256.Sum256(blob)
	hash := hex.EncodeToString(sum[:])

	if rec := do(t, mux, "HEAD", "/api/v1/assets/"+hash, nil, true); rec.Code != 404 {
		t.Fatalf("HEAD missing asset: want 404, got %d", rec.Code)
	}
	if rec := do(t, mux, "POST", "/api/v1/assets/"+hash+"?ext=png", blob, true); rec.Code != 201 {
		t.Fatalf("upload: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, mux, "HEAD", "/api/v1/assets/"+hash, nil, true); rec.Code != 204 {
		t.Fatalf("HEAD after upload: want 204, got %d", rec.Code)
	}

	// Create.
	rec := do(t, mux, "POST", "/api/v1/notes", map[string]any{
		"title": "My Note", "html": "<p>hello</p>", "description": "d", "assets": []string{hash},
	}, true)
	if rec.Code != 201 {
		t.Fatalf("create: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	note := decode[noteResp](t, rec)
	if note.ID == "" || len(note.Slug) != 10 || !strings.HasPrefix(note.URL, "http://notes.test/") {
		t.Fatalf("bad create response: %+v", note)
	}

	// List.
	list := decode[struct {
		Notes []noteResp `json:"notes"`
	}](t, do(t, mux, "GET", "/api/v1/notes", nil, true))
	if len(list.Notes) != 1 || list.Notes[0].ID != note.ID {
		t.Fatalf("list: %+v", list)
	}

	// Update.
	rec = do(t, mux, "PUT", "/api/v1/notes/"+note.ID, map[string]any{
		"title": "Renamed", "html": "<p>v2</p>",
	}, true)
	if rec.Code != 200 || decode[noteResp](t, rec).Title != "Renamed" {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}

	// Rotate (random, then custom).
	rot := decode[noteResp](t, do(t, mux, "POST", "/api/v1/notes/"+note.ID+"/rotate", nil, true))
	if rot.Slug == note.Slug {
		t.Fatal("rotate should change slug")
	}
	rot = decode[noteResp](t, do(t, mux, "POST", "/api/v1/notes/"+note.ID+"/rotate", map[string]string{"slug": "pretty-name"}, true))
	if rot.Slug != "pretty-name" {
		t.Fatalf("custom rotate: %+v", rot)
	}

	// Passcode set + clear.
	if rec := do(t, mux, "PUT", "/api/v1/notes/"+note.ID+"/passcode", map[string]string{"passcode": "amber-falcon-42"}, true); rec.Code != 204 {
		t.Fatalf("set passcode: %d", rec.Code)
	}
	list = decode[struct {
		Notes []noteResp `json:"notes"`
	}](t, do(t, mux, "GET", "/api/v1/notes", nil, true))
	if !list.Notes[0].Protected {
		t.Fatal("note should be protected")
	}
	if rec := do(t, mux, "DELETE", "/api/v1/notes/"+note.ID+"/passcode", nil, true); rec.Code != 204 {
		t.Fatalf("clear passcode: %d", rec.Code)
	}

	// Delete.
	if rec := do(t, mux, "DELETE", "/api/v1/notes/"+note.ID, nil, true); rec.Code != 204 {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec := do(t, mux, "PUT", "/api/v1/notes/"+note.ID, map[string]any{"title": "x", "html": "y"}, true); rec.Code != 404 {
		t.Fatalf("update after delete: want 404, got %d", rec.Code)
	}
}

func TestValidation(t *testing.T) {
	mux := newTestAPI(t)

	if rec := do(t, mux, "POST", "/api/v1/notes", map[string]any{"title": "  ", "html": "x"}, true); rec.Code != 422 {
		t.Fatalf("empty title: want 422, got %d", rec.Code)
	}
	if rec := do(t, mux, "POST", "/api/v1/notes", map[string]any{"title": "x", "html": "y", "slug": "Bad Slug"}, true); rec.Code != 422 {
		t.Fatalf("bad slug: want 422, got %d", rec.Code)
	}
	if rec := do(t, mux, "POST", "/api/v1/notes", map[string]any{"title": "x", "html": "y", "assets": []string{"nothex"}}, true); rec.Code != 422 {
		t.Fatalf("bad asset hash: want 422, got %d", rec.Code)
	}
	if rec := do(t, mux, "POST", "/api/v1/notes", map[string]any{
		"title": "x", "html": "y", "assets": []string{strings.Repeat("d", 64)},
	}, true); rec.Code != 422 {
		t.Fatalf("unknown asset: want 422, got %d: %s", rec.Code, rec.Body.String())
	}

	// Slug conflicts.
	if rec := do(t, mux, "POST", "/api/v1/notes", map[string]any{"title": "a", "html": "a", "slug": "mine"}, true); rec.Code != 201 {
		t.Fatalf("create: %d", rec.Code)
	}
	if rec := do(t, mux, "POST", "/api/v1/notes", map[string]any{"title": "b", "html": "b", "slug": "mine"}, true); rec.Code != 409 {
		t.Fatalf("dup slug: want 409, got %d", rec.Code)
	}

	// Asset hash mismatch.
	wrong := hex.EncodeToString(bytes.Repeat([]byte{0xab}, 32))
	if rec := do(t, mux, "POST", "/api/v1/assets/"+wrong+"?ext=png", []byte("actual bytes"), true); rec.Code != 422 {
		t.Fatalf("hash mismatch: want 422, got %d: %s", rec.Code, rec.Body.String())
	}
	sum := sha256.Sum256([]byte("z"))
	if rec := do(t, mux, "POST", fmt.Sprintf("/api/v1/assets/%x?ext=exe", sum), []byte("z"), true); rec.Code != 422 {
		t.Fatalf("bad ext: want 422, got %d", rec.Code)
	}
}

func TestTheme(t *testing.T) {
	mux := newTestAPI(t)
	ping := decode[map[string]any](t, do(t, mux, "GET", "/api/v1/ping", nil, true))
	if ping["theme_updated_at"].(float64) != 0 {
		t.Fatal("fresh server should report theme_updated_at=0")
	}
	if rec := do(t, mux, "PUT", "/api/v1/theme", map[string]string{"css": "body{}", "js": "x", "head": "<link>"}, true); rec.Code != 204 {
		t.Fatalf("put theme: %d", rec.Code)
	}
	got := decode[map[string]any](t, do(t, mux, "GET", "/api/v1/theme", nil, true))
	if got["css"] != "body{}" || got["head"] != "<link>" || got["updated_at"].(float64) == 0 {
		t.Fatalf("theme roundtrip: %v", got)
	}
}
