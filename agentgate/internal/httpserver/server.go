// Package httpserver provides AgentGate's HTTP surface.
//
// Today it only exposes liveness and readiness endpoints. Per
// docs/PROJECT_DEFINITION.md §2 (:8090 HTTP UI + API), later phases add the
// operator API/UI on the same listener — this package is the boundary
// where that will be wired in, not a placeholder for it.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// Server serves AgentGate's HTTP endpoints.
type Server struct {
	addr           string
	logger         *slog.Logger
	srv            *http.Server
	ln             net.Listener
	mux            *http.ServeMux
	ready          atomic.Bool
	readinessCheck func(context.Context) error
}

// New constructs a Server bound to addr. It does not open a listener until
// Start is called.
func New(addr string, logger *slog.Logger) *Server {
	mux := http.NewServeMux()
	s := &Server{
		addr:   addr,
		logger: logger,
		mux:    mux,
		srv: &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       120 * time.Second,
			MaxHeaderBytes:    1 << 20,
		},
	}

	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)

	return s
}

// Mux returns the server's HTTP ServeMux to register additional routes.
func (s *Server) Mux() *http.ServeMux {
	return s.mux
}

// HTTPServer returns the underlying http.Server instance.
func (s *Server) HTTPServer() *http.Server {
	return s.srv
}

// SetReadinessCheck sets an optional check (e.g. DB ping) for the /readyz endpoint.
func (s *Server) SetReadinessCheck(check func(context.Context) error) {
	s.readinessCheck = check
}

// Start binds the listener and begins serving in the background. It
// returns as soon as the listener is bound, so a caller learns immediately
// whether the configured address is usable, rather than blocking for the
// server's entire lifetime.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("httpserver: listen on %s: %w", s.addr, err)
	}
	s.ln = ln
	s.ready.Store(true)

	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("http server exited unexpectedly", "error", err)
		}
	}()

	return nil
}

// Addr returns the actual bound address (useful when the configured
// address uses an ephemeral port, e.g. in tests). It is empty until Start
// succeeds.
func (s *Server) Addr() string {
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Shutdown gracefully stops the server, waiting for in-flight requests to
// finish or for ctx to be done, whichever comes first.
func (s *Server) Shutdown(ctx context.Context) error {
	s.ready.Store(false)
	return s.srv.Shutdown(ctx)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	// Liveness: the process is up and able to handle a request at all.
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	// Readiness: the server has bound its listener and has not begun
	// shutting down. Checks dependent health (such as PostgreSQL) if configured.
	if !s.ready.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not ready"))
		return
	}

	if s.readinessCheck != nil {
		if err := s.readinessCheck(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("dependency not ready: " + err.Error()))
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}
