// Package limit provides a small fixed-window rate limiter and client IP extraction.
package limit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type bucket struct {
	count int
	start time.Time
}

// Limiter allows at most max events per key per window.
type Limiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	buckets map[string]*bucket
}

func New(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, buckets: make(map[string]*bucket)}
}

// Allow records an event for key and reports whether it is within the limit.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()

	// Opportunistic cleanup so the map can't grow unbounded.
	if len(l.buckets) > 4096 {
		for k, b := range l.buckets {
			if now.Sub(b.start) > l.window {
				delete(l.buckets, k)
			}
		}
	}

	b := l.buckets[key]
	if b == nil || now.Sub(b.start) > l.window {
		l.buckets[key] = &bucket{count: 1, start: now}
		return true
	}
	b.count++
	return b.count <= l.max
}

// ClientIP extracts the caller's IP for rate-limit bucketing. With trustProxy,
// the last X-Forwarded-For entry (appended by the proxy in front) wins.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
