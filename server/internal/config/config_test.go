package config

import (
	"strings"
	"testing"
)

func setValid(t *testing.T) {
	t.Helper()
	t.Setenv("API_KEY", "a-long-enough-test-key")
	t.Setenv("BASE_URL", "https://notes.example.com")
	// Neutralize anything from the developer's shell.
	for _, k := range []string{"API_KEY_FILE", "LISTEN_ADDR", "DATA_DIR", "SITE_NAME",
		"SITE_AUTHOR", "OG_ACCENT", "MAX_NOTE_MB", "MAX_ASSET_MB", "TRUST_PROXY"} {
		t.Setenv(k, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	setValid(t)
	cfg, err := Load("test")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SiteName != "notes.example.com" {
		t.Errorf("SiteName should default to the host, got %q", cfg.SiteName)
	}
	if cfg.ListenAddr != ":8080" || cfg.DataDir != "/data" {
		t.Errorf("bad defaults: %q %q", cfg.ListenAddr, cfg.DataDir)
	}
	if cfg.MaxNoteBytes != 2<<20 || cfg.MaxAssetBytes != 25<<20 {
		t.Errorf("bad size defaults: %d %d", cfg.MaxNoteBytes, cfg.MaxAssetBytes)
	}
	if !cfg.SecureCookies() {
		t.Error("https BASE_URL should mean secure cookies")
	}
	if got := cfg.AbsoluteURL("/x"); got != "https://notes.example.com/x" {
		t.Errorf("AbsoluteURL: %q", got)
	}
}

func TestLoadValidation(t *testing.T) {
	cases := []struct {
		name string
		mod  func(t *testing.T)
		want string
	}{
		{"missing api key", func(t *testing.T) { t.Setenv("API_KEY", "") }, "API_KEY"},
		{"short api key", func(t *testing.T) { t.Setenv("API_KEY", "tiny") }, "at least 16"},
		{"missing base url", func(t *testing.T) { t.Setenv("BASE_URL", "") }, "BASE_URL"},
		{"relative base url", func(t *testing.T) { t.Setenv("BASE_URL", "notes.example.com") }, "absolute"},
		{"base url with path", func(t *testing.T) { t.Setenv("BASE_URL", "https://x.com/notes") }, "path"},
		{"bad accent", func(t *testing.T) { t.Setenv("OG_ACCENT", "green") }, "OG_ACCENT"},
		{"bad size", func(t *testing.T) { t.Setenv("MAX_NOTE_MB", "-3") }, "MAX_NOTE_MB"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setValid(t)
			tc.mod(t)
			_, err := Load("test")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestTrailingSlashAndHTTP(t *testing.T) {
	setValid(t)
	t.Setenv("BASE_URL", "http://localhost:8080/")
	cfg, err := Load("test")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL.String() != "http://localhost:8080" {
		t.Errorf("trailing slash should be stripped, got %q", cfg.BaseURL)
	}
	if cfg.SecureCookies() {
		t.Error("http BASE_URL must not set secure cookies")
	}
}
