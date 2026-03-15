package health

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestHealthzReturnsOK(t *testing.T) {
	srv := NewServer(0, zap.NewNop())
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop(context.Background())

	resp, err := http.Get(fmt.Sprintf("http://%s/healthz", srv.Addr()))
	if err != nil {
		t.Fatalf("healthz request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestReadyzBeforeReady(t *testing.T) {
	srv := NewServer(0, zap.NewNop())
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop(context.Background())

	resp, err := http.Get(fmt.Sprintf("http://%s/readyz", srv.Addr()))
	if err != nil {
		t.Fatalf("readyz request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 before ready, got %d", resp.StatusCode)
	}
}

func TestReadyzAfterReady(t *testing.T) {
	srv := NewServer(0, zap.NewNop())
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop(context.Background())

	srv.SetReady()

	resp, err := http.Get(fmt.Sprintf("http://%s/readyz", srv.Addr()))
	if err != nil {
		t.Fatalf("readyz request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after ready, got %d", resp.StatusCode)
	}
}

func TestGracefulShutdown(t *testing.T) {
	srv := NewServer(0, zap.NewNop())
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	addr := srv.Addr()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := srv.Stop(ctx); err != nil {
		t.Errorf("graceful shutdown failed: %v", err)
	}

	resp, err := http.Get(fmt.Sprintf("http://%s/healthz", addr))
	if err == nil {
		resp.Body.Close()
		t.Error("expected error after shutdown, got nil")
	}
}
