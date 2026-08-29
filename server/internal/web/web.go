// Package web serves the public site: note pages, unlock flow, assets, theme,
// OG images, sitemap, and robots.
package web

import (
	"bytes"
	"compress/gzip"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sanketsaurav/outcrop/server/internal/config"
	"github.com/sanketsaurav/outcrop/server/internal/limit"
	"github.com/sanketsaurav/outcrop/server/internal/store"
)

//go:embed templates/*.html fallback.css
var content embed.FS

const csp = "default-src 'none'; img-src 'self' data: https:; media-src 'self'; " +
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
	"font-src 'self' data: https://fonts.gstatic.com; script-src 'self'; " +
	"base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

type Server struct {
	cfg         *config.Config
	st          *store.Store
	blobs       *store.Blobs
	page        *template.Template
	unlock      *template.Template
	nfPage      *template.Template
	fallbackCSS []byte
	og          *ogRenderer
	unlockLimit *limit.Limiter
	secret      []byte
}

func New(cfg *config.Config, st *store.Store, blobs *store.Blobs) (*Server, error) {
	s := &Server{
		cfg:         cfg,
		st:          st,
		blobs:       blobs,
		unlockLimit: limit.New(5, time.Minute),
	}
	var err error
	if s.secret, err = st.CookieSecret(); err != nil {
		return nil, fmt.Errorf("loading cookie secret: %w", err)
	}
	if s.og, err = newOGRenderer(cfg.SiteName, cfg.OGAccent); err != nil {
		return nil, fmt.Errorf("initializing OG renderer: %w", err)
	}
	if s.fallbackCSS, err = content.ReadFile("fallback.css"); err != nil {
		return nil, err
	}

	// The page template can be overridden by dropping template.html in DATA_DIR.
	pageSrc, err := content.ReadFile("templates/page.html")
	if err != nil {
		return nil, err
	}
	if custom, err := os.ReadFile(filepath.Join(cfg.DataDir, "template.html")); err == nil {
		slog.Info("web: using custom page template from DATA_DIR/template.html")
		pageSrc = custom
	}
	if s.page, err = template.New("page").Parse(string(pageSrc)); err != nil {
		return nil, fmt.Errorf("parsing page template: %w", err)
	}
	if s.unlock, err = template.ParseFS(content, "templates/unlock.html"); err != nil {
		return nil, err
	}
	if s.nfPage, err = template.ParseFS(content, "templates/notfound.html"); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/", s.notFound) // catch-all, including GET /
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("GET /robots.txt", s.robots)
	mux.HandleFunc("GET /sitemap.xml", s.sitemap)
	mux.HandleFunc("GET /t/site.css", s.themeCSS)
	mux.HandleFunc("GET /t/site.js", s.themeJS)
	mux.HandleFunc("GET /a/{file}", s.asset)
	mux.HandleFunc("GET /og/{file}", s.ogImage)
	mux.HandleFunc("GET /{slug}", s.notePage)
	mux.HandleFunc("POST /{slug}/unlock", s.unlockPost)
}

// ---- note pages ----

type pageData struct {
	SiteName     string
	Title        string
	Description  string
	URL          string
	OGImageURL   string
	CreatedISO   string
	CreatedHuman string
	UpdatedISO   string
	UpdatedHuman string
	Noindex      bool
	ThemeVersion int64
	HeadExtra    template.HTML
	JSONLD       template.HTML
	Body         template.HTML
	Slug         string
	UnlockError  string
}

func (s *Server) notePage(w http.ResponseWriter, r *http.Request) {
	note, err := s.st.GetNoteBySlug(r.PathValue("slug"))
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.internalError(w, "loading note", err)
		return
	}
	theme, err := s.st.GetTheme()
	if err != nil {
		s.internalError(w, "loading theme", err)
		return
	}

	if note.Protected() && !s.hasUnlockCookie(r, note) {
		s.renderUnlock(w, r, note, theme, "", http.StatusOK)
		return
	}

	h := w.Header()
	if note.Protected() {
		h.Set("Cache-Control", "private, no-store")
	} else {
		etag := fmt.Sprintf(`W/"%d-%d"`, note.UpdatedAt, theme.UpdatedAt)
		h.Set("ETag", etag)
		h.Set("Cache-Control", "no-cache")
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	if note.Noindex {
		h.Set("X-Robots-Tag", "noindex")
	}
	s.renderHTML(w, r, http.StatusOK, s.page, s.buildPageData(note, theme))
}

func (s *Server) buildPageData(note *store.Note, theme store.Theme) pageData {
	url := s.cfg.AbsoluteURL("/" + note.Slug)
	d := pageData{
		SiteName:     s.cfg.SiteName,
		Title:        note.Title,
		Description:  note.Description,
		URL:          url,
		OGImageURL:   s.cfg.AbsoluteURL(fmt.Sprintf("/og/%s.png?v=%d", note.Slug, note.UpdatedAt)),
		CreatedISO:   time.Unix(note.CreatedAt, 0).UTC().Format(time.RFC3339),
		CreatedHuman: time.Unix(note.CreatedAt, 0).UTC().Format("January 2, 2006"),
		UpdatedISO:   time.Unix(note.UpdatedAt, 0).UTC().Format(time.RFC3339),
		UpdatedHuman: time.Unix(note.UpdatedAt, 0).UTC().Format("January 2, 2006"),
		Noindex:      note.Noindex,
		ThemeVersion: theme.UpdatedAt,
		HeadExtra:    template.HTML(theme.Head),
		Body:         template.HTML(note.HTML),
		Slug:         note.Slug,
	}
	if !note.Protected() {
		ld := map[string]any{
			"@context":         "https://schema.org",
			"@type":            "Article",
			"headline":         note.Title,
			"datePublished":    d.CreatedISO,
			"dateModified":     d.UpdatedISO,
			"mainEntityOfPage": url,
		}
		if s.cfg.SiteAuthor != "" {
			ld["author"] = map[string]any{"@type": "Person", "name": s.cfg.SiteAuthor}
		}
		// json.Marshal escapes <, >, & — safe to embed in a script element.
		if b, err := json.Marshal(ld); err == nil {
			d.JSONLD = template.HTML(`<script type="application/ld+json">` + string(b) + `</script>`)
		}
	}
	return d
}

// ---- unlock flow ----

func (s *Server) unlockPost(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	note, err := s.st.GetNoteBySlug(slug)
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.internalError(w, "loading note", err)
		return
	}
	if !note.Protected() {
		http.Redirect(w, r, "/"+slug, http.StatusSeeOther)
		return
	}
	theme, err := s.st.GetTheme()
	if err != nil {
		s.internalError(w, "loading theme", err)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		s.renderUnlock(w, r, note, theme, "Something went wrong — try again.", http.StatusBadRequest)
		return
	}
	ip := limit.ClientIP(r, s.cfg.TrustProxy)
	if !s.unlockLimit.Allow("unlock:" + ip + ":" + note.ID) {
		s.renderUnlock(w, r, note, theme, "Too many attempts — wait a minute and try again.", http.StatusTooManyRequests)
		return
	}
	if !store.VerifyPasscode(note, r.PostFormValue("passcode")) {
		time.Sleep(300 * time.Millisecond)
		s.renderUnlock(w, r, note, theme, "That passcode isn't right.", http.StatusOK)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "oc_" + note.ID,
		Value:    store.UnlockToken(s.secret, note),
		Path:     "/" + note.Slug,
		MaxAge:   30 * 24 * 3600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.SecureCookies(),
	})
	http.Redirect(w, r, "/"+note.Slug, http.StatusSeeOther)
}

func (s *Server) hasUnlockCookie(r *http.Request, note *store.Note) bool {
	c, err := r.Cookie("oc_" + note.ID)
	if err != nil {
		return false
	}
	want := store.UnlockToken(s.secret, note)
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(want)) == 1
}

func (s *Server) renderUnlock(w http.ResponseWriter, r *http.Request, note *store.Note, theme store.Theme, errMsg string, status int) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	d := s.buildPageData(note, theme)
	d.Noindex = true
	d.UnlockError = errMsg
	d.Body = ""
	s.renderHTML(w, r, status, s.unlock, d)
}

// ---- theme ----

func (s *Server) themeCSS(w http.ResponseWriter, r *http.Request) {
	theme, err := s.st.GetTheme()
	if err != nil {
		s.internalError(w, "loading theme", err)
		return
	}
	body := []byte(theme.CSS)
	if len(bytes.TrimSpace(body)) == 0 {
		body = s.fallbackCSS
	}
	h := w.Header()
	h.Set("Content-Type", "text/css; charset=utf-8")
	h.Set("Cache-Control", "public, max-age=31536000, immutable") // busted via ?v=
	h.Set("X-Content-Type-Options", "nosniff")
	writeMaybeGzip(w, r, http.StatusOK, body)
}

func (s *Server) themeJS(w http.ResponseWriter, r *http.Request) {
	theme, err := s.st.GetTheme()
	if err != nil {
		s.internalError(w, "loading theme", err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/javascript; charset=utf-8")
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	writeMaybeGzip(w, r, http.StatusOK, []byte(theme.JS))
}

// ---- assets ----

func (s *Server) asset(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	hash, ext, ok := strings.Cut(file, ".")
	if !ok || !store.ValidAssetHash(hash) {
		s.notFound(w, r)
		return
	}
	a, err := s.st.GetAsset(hash)
	if errors.Is(err, store.ErrNotFound) || (err == nil && a.Ext != ext) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.internalError(w, "loading asset", err)
		return
	}
	f, err := s.blobs.Open(hash)
	if err != nil {
		s.internalError(w, "opening blob", err)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", a.MIME)
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("ETag", `"`+hash+`"`)
	h.Set("X-Content-Type-Options", "nosniff")
	// Hardens SVG (and anything else viewed as a document): no scripts, no framing.
	h.Set("Content-Security-Policy", "script-src 'none'; frame-ancestors 'none'")
	h.Set("Content-Disposition", "inline")
	http.ServeContent(w, r, "", time.Unix(a.CreatedAt, 0), f)
}

// ---- og image ----

func (s *Server) ogImage(w http.ResponseWriter, r *http.Request) {
	slug, ok := strings.CutSuffix(r.PathValue("file"), ".png")
	if !ok {
		s.notFound(w, r)
		return
	}
	note, err := s.st.GetNoteBySlug(slug)
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.internalError(w, "loading note", err)
		return
	}
	png, etag, err := s.og.render(note.Title)
	if err != nil {
		s.internalError(w, "rendering og image", err)
		return
	}
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", "public, max-age=86400")
	h.Set("Content-Type", "image/png")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Write(png)
}

// ---- sitemap & robots ----

func (s *Server) sitemap(w http.ResponseWriter, r *http.Request) {
	notes, err := s.st.ListSitemapNotes()
	if err != nil {
		s.internalError(w, "listing sitemap notes", err)
		return
	}
	type urlEntry struct {
		Loc     string `xml:"loc"`
		LastMod string `xml:"lastmod"`
	}
	type urlset struct {
		XMLName xml.Name   `xml:"urlset"`
		XMLNS   string     `xml:"xmlns,attr"`
		URLs    []urlEntry `xml:"url"`
	}
	set := urlset{XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	for _, n := range notes {
		set.URLs = append(set.URLs, urlEntry{
			Loc:     s.cfg.AbsoluteURL("/" + n.Slug),
			LastMod: time.Unix(n.UpdatedAt, 0).UTC().Format(time.RFC3339),
		})
	}
	b, err := xml.MarshalIndent(set, "", "  ")
	if err != nil {
		s.internalError(w, "marshaling sitemap", err)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	writeMaybeGzip(w, r, http.StatusOK, append([]byte(xml.Header), b...))
}

func (s *Server) robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "User-agent: *\nAllow: /\n\nSitemap: %s\n", s.cfg.AbsoluteURL("/sitemap.xml"))
}

// ---- shared rendering ----

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	// API callers get a JSON 404, not the styled page (see api.Server.Register).
	if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"unknown API route"}`))
		return
	}
	theme, _ := s.st.GetTheme()
	d := pageData{SiteName: s.cfg.SiteName, Title: "Not found", ThemeVersion: theme.UpdatedAt,
		HeadExtra: template.HTML(theme.Head), Noindex: true}
	s.renderHTML(w, r, http.StatusNotFound, s.nfPage, d)
}

func (s *Server) internalError(w http.ResponseWriter, what string, err error) {
	slog.Error("web: "+what, "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func (s *Server) renderHTML(w http.ResponseWriter, r *http.Request, status int, t *template.Template, data pageData) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		s.internalError(w, "executing template", err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", csp)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	writeMaybeGzip(w, r, status, buf.Bytes())
}

func writeMaybeGzip(w http.ResponseWriter, r *http.Request, status int, body []byte) {
	if len(body) >= 860 && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		w.WriteHeader(status)
		gz := gzip.NewWriter(w)
		gz.Write(body)
		gz.Close()
		return
	}
	w.WriteHeader(status)
	w.Write(body)
}
