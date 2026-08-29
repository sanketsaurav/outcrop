package limit

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiterWindow(t *testing.T) {
	l := New(2, 50*time.Millisecond)
	if !l.Allow("k") || !l.Allow("k") {
		t.Fatal("first two events must pass")
	}
	if l.Allow("k") {
		t.Fatal("third event within the window must be limited")
	}
	if !l.Allow("other") {
		t.Fatal("keys are independent")
	}
	time.Sleep(60 * time.Millisecond)
	if !l.Allow("k") {
		t.Fatal("a fresh window must allow again")
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.9:4444"
	if got := ClientIP(r, false); got != "203.0.113.9" {
		t.Errorf("direct: %q", got)
	}

	r.Header.Set("X-Forwarded-For", "198.51.100.1, 203.0.113.50")
	if got := ClientIP(r, false); got != "203.0.113.9" {
		t.Errorf("untrusted proxy must ignore XFF, got %q", got)
	}
	if got := ClientIP(r, true); got != "203.0.113.50" {
		t.Errorf("trusted proxy takes the last XFF entry, got %q", got)
	}

	r.Header.Del("X-Forwarded-For")
	if got := ClientIP(r, true); got != "203.0.113.9" {
		t.Errorf("trusted proxy without XFF falls back to RemoteAddr, got %q", got)
	}
}
