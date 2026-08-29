// Package config loads server configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type Config struct {
	APIKey        string
	BaseURL       *url.URL
	ListenAddr    string
	DataDir       string
	SiteName      string
	SiteAuthor    string
	OGAccent      string
	MaxNoteBytes  int64
	MaxAssetBytes int64
	TrustProxy    bool
	Version       string
}

var hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func Load(version string) (*Config, error) {
	cfg := &Config{Version: version}

	cfg.APIKey = strings.TrimSpace(os.Getenv("API_KEY"))
	if f := os.Getenv("API_KEY_FILE"); cfg.APIKey == "" && f != "" {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("reading API_KEY_FILE: %w", err)
		}
		cfg.APIKey = strings.TrimSpace(string(b))
	}
	if cfg.APIKey == "" {
		return nil, errors.New("API_KEY (or API_KEY_FILE) is required; generate one with: openssl rand -hex 32")
	}
	if len(cfg.APIKey) < 16 {
		return nil, errors.New("API_KEY must be at least 16 characters")
	}

	raw := strings.TrimRight(strings.TrimSpace(os.Getenv("BASE_URL")), "/")
	if raw == "" {
		return nil, errors.New("BASE_URL is required (e.g. https://notes.example.com)")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("BASE_URL must be an absolute http(s) URL, got %q", raw)
	}
	if u.Path != "" {
		return nil, errors.New("BASE_URL must not contain a path; Outcrop serves from the domain root")
	}
	cfg.BaseURL = u

	cfg.ListenAddr = envDefault("LISTEN_ADDR", ":8080")
	cfg.DataDir = envDefault("DATA_DIR", "/data")
	cfg.SiteName = envDefault("SITE_NAME", u.Host)
	cfg.SiteAuthor = strings.TrimSpace(os.Getenv("SITE_AUTHOR"))

	cfg.OGAccent = envDefault("OG_ACCENT", "#6c5ce7")
	if !hexColorRe.MatchString(cfg.OGAccent) {
		return nil, fmt.Errorf("OG_ACCENT must be a #rrggbb hex color, got %q", cfg.OGAccent)
	}

	if cfg.MaxNoteBytes, err = envMB("MAX_NOTE_MB", 2); err != nil {
		return nil, err
	}
	if cfg.MaxAssetBytes, err = envMB("MAX_ASSET_MB", 25); err != nil {
		return nil, err
	}
	cfg.TrustProxy = os.Getenv("TRUST_PROXY") == "1"
	return cfg, nil
}

// AbsoluteURL joins an absolute path like "/slug" onto BASE_URL.
func (c *Config) AbsoluteURL(path string) string {
	return c.BaseURL.String() + path
}

// SecureCookies reports whether unlock cookies should carry the Secure flag.
func (c *Config) SecureCookies() bool {
	return c.BaseURL.Scheme == "https"
}

func envDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envMB(key string, def int64) (int64, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def << 20, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 || n > 1024 {
		return 0, fmt.Errorf("%s must be a positive number of megabytes, got %q", key, v)
	}
	return n << 20, nil
}
