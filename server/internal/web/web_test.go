package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanketsaurav/outcrop/server/internal/config"
	"github.com/sanketsaurav/outcrop/server/internal/store"
)

type testEnv struct {
	mux   *http.ServeMux
	st    *store.Store
	blobs *store.Blobs
}

func newTestWeb(t *testing.T) *testEnv {
	t.Helper()
	base, _ := url.Parse("http://notes.test")
	dir := t.TempDir()
	cfg := &config.Config{
		APIKey: "test-api-key-0123456789", BaseURL: base, DataDir: dir,
		SiteName: "Test Notes", SiteAuthor: "Test Author", OGAccent: "#6c5ce7",
		MaxNoteBytes: 2 << 20, MaxAssetBytes: 1 << 20, Version: "test",
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
	srv, err := New(cfg, st, blobs)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv.Register(mux)
	return &testEnv{mux: mux, st: st, blobs: blobs}
}

func (e *testEnv) get(t *testing.T, path string, mod func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if mod != nil {
		mod(req)
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec
}

func TestNotePage(t *testing.T) {
	e := newTestWeb(t)
	n, err := e.st.CreateNote(store.NoteInput{
		Title: "Hello World", Description: "A test note", HTML: "<p>body-content-here</p>",
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	rec := e.get(t, "/"+n.Slug, nil)
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Hello World", "body-content-here", `property="og:image"`,
		"application/ld+json", "Test Author", `rel="canonical"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
		t.Errorf("missing CSP, got %q", csp)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}
	rec = e.get(t, "/"+n.Slug, func(r *http.Request) { r.Header.Set("If-None-Match", etag) })
	if rec.Code != 304 {
		t.Fatalf("want 304, got %d", rec.Code)
	}
}

func TestNotFound(t *testing.T) {
	e := newTestWeb(t)
	for _, path := range []string{"/", "/does-not-exist", "/deep/path/here"} {
		rec := e.get(t, path, nil)
		if rec.Code != 404 {
			t.Errorf("%s: want 404, got %d", path, rec.Code)
		}
	}
	if !strings.Contains(e.get(t, "/gone", nil).Body.String(), "404") {
		t.Error("404 page should be styled")
	}
}

func TestUnlockFlow(t *testing.T) {
	e := newTestWeb(t)
	n, err := e.st.CreateNote(store.NoteInput{Title: "Locked Note", HTML: "<p>secret-content</p>"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetPasscode(n.ID, "amber-falcon-42"); err != nil {
		t.Fatal(err)
	}

	// Without a cookie: unlock form, not content.
	rec := e.get(t, "/"+n.Slug, nil)
	body := rec.Body.String()
	if !strings.Contains(body, "protected") || strings.Contains(body, "secret-content") {
		t.Fatalf("expected unlock page without content, got: %.200s", body)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("unlock page must be no-store, got %q", cc)
	}

	post := func(pass string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/"+n.Slug+"/unlock", strings.NewReader("passcode="+url.QueryEscape(pass)))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := post("wrong-pass-11"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "isn&#39;t right") {
		t.Fatalf("wrong passcode: %d %.200s", rec.Code, rec.Body.String())
	}

	rec2 := post("Amber-Falcon-42") // normalization-insensitive
	if rec2.Code != 303 {
		t.Fatalf("right passcode: want 303, got %d", rec2.Code)
	}
	cookies := rec2.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].Path != "/"+n.Slug {
		t.Fatalf("bad cookie: %+v", cookies)
	}

	rec = e.get(t, "/"+n.Slug, func(r *http.Request) { r.AddCookie(cookies[0]) })
	if !strings.Contains(rec.Body.String(), "secret-content") {
		t.Fatal("valid cookie should reveal content")
	}

	// Changing the passcode invalidates the cookie.
	if err := e.st.SetPasscode(n.ID, "new-pass-77"); err != nil {
		t.Fatal(err)
	}
	rec = e.get(t, "/"+n.Slug, func(r *http.Request) { r.AddCookie(cookies[0]) })
	if strings.Contains(rec.Body.String(), "secret-content") {
		t.Fatal("stale cookie must not reveal content")
	}
}

func TestSitemapAndRobots(t *testing.T) {
	e := newTestWeb(t)
	pub, _ := e.st.CreateNote(store.NoteInput{Title: "Public", HTML: "p"}, "")
	hidden, _ := e.st.CreateNote(store.NoteInput{Title: "Hidden", HTML: "h", Noindex: true}, "")
	locked, _ := e.st.CreateNote(store.NoteInput{Title: "Locked", HTML: "l"}, "")
	e.st.SetPasscode(locked.ID, "pass-code-11")

	body := e.get(t, "/sitemap.xml", nil).Body.String()
	if !strings.Contains(body, pub.Slug) {
		t.Error("sitemap should include the public note")
	}
	if strings.Contains(body, hidden.Slug) || strings.Contains(body, locked.Slug) {
		t.Error("sitemap must exclude noindex and protected notes")
	}

	robots := e.get(t, "/robots.txt", nil).Body.String()
	if !strings.Contains(robots, "Sitemap: http://notes.test/sitemap.xml") {
		t.Errorf("robots missing sitemap line: %q", robots)
	}
}

func TestOGImage(t *testing.T) {
	e := newTestWeb(t)
	n, _ := e.st.CreateNote(store.NoteInput{
		Title: "A fairly long note title that will need to wrap across several lines in the image", HTML: "x",
	}, "")

	rec := e.get(t, "/og/"+n.Slug+".png", nil)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("og: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("decoding png: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 1200 || b.Dy() != 630 {
		t.Fatalf("want 1200x630, got %dx%d", b.Dx(), b.Dy())
	}
	if e.get(t, "/og/nonexistent.png", nil).Code != 404 {
		t.Error("og for unknown slug should 404")
	}
}

func TestThemeServing(t *testing.T) {
	e := newTestWeb(t)
	if body := e.get(t, "/t/site.css", nil).Body.String(); !strings.Contains(body, ":root") {
		t.Error("fallback CSS should be served before a theme is pushed")
	}
	e.st.SetTheme(store.Theme{CSS: "body{color:tomato}", JS: "console.log(1)"})
	if body := e.get(t, "/t/site.css", nil).Body.String(); body != "body{color:tomato}" {
		t.Errorf("theme css: %q", body)
	}
	if body := e.get(t, "/t/site.js", nil).Body.String(); body != "console.log(1)" {
		t.Errorf("theme js: %q", body)
	}
}

func TestAssetServing(t *testing.T) {
	e := newTestWeb(t)
	blob := []byte("image-bytes")
	sum := sha256.Sum256(blob)
	hash := hex.EncodeToString(sum[:])
	if _, err := e.blobs.Save(hash, bytes.NewReader(blob)); err != nil {
		t.Fatal(err)
	}
	if err := e.st.InsertAsset(hash, "png", "image/png", int64(len(blob))); err != nil {
		t.Fatal(err)
	}

	rec := e.get(t, "/a/"+hash+".png", nil)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("asset: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !bytes.Equal(rec.Body.Bytes(), blob) {
		t.Fatal("asset bytes mismatch")
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("asset should be immutable, got %q", cc)
	}
	if e.get(t, "/a/"+hash+".jpg", nil).Code != 404 {
		t.Error("wrong extension should 404")
	}
	if e.get(t, "/a/nothash.png", nil).Code != 404 {
		t.Error("bad hash should 404")
	}
}
