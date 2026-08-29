// Outcrop server: self-hosted public note sharing for Obsidian.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sanketsaurav/outcrop/server/internal/api"
	"github.com/sanketsaurav/outcrop/server/internal/config"
	"github.com/sanketsaurav/outcrop/server/internal/store"
	"github.com/sanketsaurav/outcrop/server/internal/web"
)

// version is stamped at build time via -ldflags "-X main.version=…".
var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("outcrop failed to start", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(version)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "outcrop.db"))
	if err != nil {
		return err
	}
	defer st.Close()
	blobs, err := store.NewBlobs(filepath.Join(cfg.DataDir, "blobs"))
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	api.New(cfg, st, blobs).Register(mux)
	websrv, err := web.New(cfg, st, blobs)
	if err != nil {
		return err
	}
	websrv.Register(mux)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           requestLog(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute, // asset uploads on slow links
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	slog.Info("outcrop listening", "addr", cfg.ListenAddr, "base_url", cfg.BaseURL.String(), "version", version)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutCtx)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		slog.Info("req", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"dur", time.Since(start).Round(time.Millisecond).String())
	})
}
