package health

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// Server provides /healthz and /readyz endpoints for Kubernetes probes.
type Server struct {
	server *http.Server
	logger *zap.Logger
	ready  atomic.Bool
}

// NewServer creates a health server on the given port.
func NewServer(port int, logger *zap.Logger) *Server {
	s := &Server{logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)

	s.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return s
}

// SetReady marks the server as ready for readiness probes.
func (s *Server) SetReady() {
	s.ready.Store(true)
}

// Start begins serving health endpoints in a background goroutine.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", s.server.Addr, err)
	}

	s.server.Addr = ln.Addr().String()

	go func() {
		s.logger.Info("Health server started", zap.String("addr", s.server.Addr))
		if err := s.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.logger.Error("Health server error", zap.Error(err))
		}
	}()

	return nil
}

// Addr returns the address the server is listening on.
func (s *Server) Addr() string {
	return s.server.Addr
}

// Stop gracefully shuts down the health server.
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	if s.ready.Load() {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("not ready"))
}
