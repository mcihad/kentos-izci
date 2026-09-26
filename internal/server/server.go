// Package server runs izci's single HTTP listener.
//
// One port serves everything: native gRPC (HTTP/2 over TLS or cleartext h2c),
// gRPC-Web and the Connect protocol (HTTP/1.1 and HTTP/2) through connect-go
// handlers, the Sentry-compatible ingest endpoints, the MCP endpoint, OIDC
// callbacks, health/metrics endpoints and the embedded single-page app.
package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/version"
)

// Options configures a Server.
type Options struct {
	Config *config.Config
	Logger *slog.Logger
	// DB backs /readyz; nil means "no database dependency" (tests).
	DB *db.DB
	// UI is the built single-page app (web/dist); nil serves a placeholder.
	UI fs.FS
}

// Server owns the HTTP listener and routing table.
type Server struct {
	cfg  *config.Config
	log  *slog.Logger
	db   *db.DB
	mux  *http.ServeMux
	http *http.Server

	streamsOnce sync.Once
	streamsDone chan struct{}
}

// New builds a server with the built-in routes registered.
func New(opts Options) *Server {
	s := &Server{
		cfg:         opts.Config,
		log:         opts.Logger,
		db:          opts.DB,
		mux:         http.NewServeMux(),
		streamsDone: make(chan struct{}),
	}
	if s.log == nil {
		s.log = slog.Default()
	}

	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /readyz", s.handleReady)
	s.mux.Handle("/", NewSPAHandler(opts.UI, s.cfg, s.log))

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	if s.cfg.TLSEnabled() {
		protocols.SetHTTP2(true)
	} else {
		// Cleartext HTTP/2 (h2c with prior knowledge) is what native gRPC
		// clients use when TLS terminates at a proxy such as Traefik.
		protocols.SetUnencryptedHTTP2(true)
	}

	s.http = &http.Server{
		Handler:           s.middleware(s.mux),
		Protocols:         protocols,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
		HTTP2: &http.HTTP2Config{
			MaxConcurrentStreams: 250,
		},
		ErrorLog: slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
	}
	if s.cfg.TLSEnabled() {
		s.http.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	s.http.RegisterOnShutdown(s.closeStreams)
	return s
}

// Handle registers a handler on the shared mux. Later packages (Connect
// services, Sentry ingest, MCP, OIDC) mount themselves through this.
func (s *Server) Handle(pattern string, h http.Handler) { s.mux.Handle(pattern, h) }

// StreamsDone is closed when shutdown begins; long-lived streaming handlers
// select on it so that graceful shutdown is not held open by idle streams.
func (s *Server) StreamsDone() <-chan struct{} { return s.streamsDone }

func (s *Server) closeStreams() { s.streamsOnce.Do(func() { close(s.streamsDone) }) }

// Handler returns the fully wrapped handler (useful for httptest).
func (s *Server) Handler() http.Handler { return s.http.Handler }

// Run listens on cfg.HTTP.Addr until ctx is cancelled, then shuts down
// gracefully within cfg.HTTP.ShutdownTimeout.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.HTTP.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.HTTP.Addr, err)
	}
	return s.Serve(ctx, ln)
}

// Serve is Run with a caller-provided listener.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	errCh := make(chan error, 1)
	go func() {
		var err error
		if s.cfg.TLSEnabled() {
			err = s.http.ServeTLS(ln, s.cfg.HTTP.TLSCertFile, s.cfg.HTTP.TLSKeyFile)
		} else {
			err = s.http.Serve(ln)
		}
		if !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	s.log.Info("http server listening",
		slog.String("addr", ln.Addr().String()),
		slog.Bool("tls", s.cfg.TLSEnabled()),
		slog.String("public_url", s.cfg.PublicURL))

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	s.log.Info("shutting down http server", slog.Duration("timeout", s.cfg.HTTP.ShutdownTimeout))
	s.closeStreams()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.HTTP.ShutdownTimeout)
	defer cancel()
	if err := s.http.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	return <-errCh
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	info := version.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": info.Version,
		"commit":  info.ShortCommit(),
	})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if s.db != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.db.Ping(ctx); err != nil {
			s.log.Warn("readiness check failed", slog.Any("err", err))
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unavailable", "reason": "database"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
