package httpserver_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/Dynamisch-LLC/agentgate/internal/httpserver"
	"github.com/Dynamisch-LLC/agentgate/internal/logging"
)

func newTestLogger() *slog.Logger {
	return logging.New(slog.LevelError, nil)
}

func TestServerHealthAndReadyWhileServing(t *testing.T) {
	s := httpserver.New("127.0.0.1:0", newTestLogger())

	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	})

	base := "http://" + s.Addr()

	resp, err := http.Get(base + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	resp2, err := http.Get(base + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("/readyz status = %d, want %d while serving", resp2.StatusCode, http.StatusOK)
	}
}

func TestServerUnreachableAfterShutdown(t *testing.T) {
	s := httpserver.New("127.0.0.1:0", newTestLogger())

	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	addr := s.Addr()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if _, err := http.Get("http://" + addr + "/healthz"); err == nil {
		t.Error("expected request after Shutdown to fail to connect, got nil error")
	}
}

func TestAddrEmptyBeforeStart(t *testing.T) {
	s := httpserver.New("127.0.0.1:0", newTestLogger())
	if addr := s.Addr(); addr != "" {
		t.Errorf("Addr() before Start() = %q, want empty", addr)
	}
}

func TestServerReadinessCheckWithDependency(t *testing.T) {
	s := httpserver.New("127.0.0.1:0", newTestLogger())

	var depHealthy bool
	s.SetReadinessCheck(func(ctx context.Context) error {
		if !depHealthy {
			return errors.New("db disconnected")
		}
		return nil
	})

	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	})

	base := "http://" + s.Addr()

	// 1. Dependency unhealthy -> 503
	resp, err := http.Get(base + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when dependency unhealthy, got %d", resp.StatusCode)
	}

	// 2. Dependency healthy -> 200
	depHealthy = true
	resp2, err := http.Get(base + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected 200 when dependency healthy, got %d", resp2.StatusCode)
	}
}

func TestServerHardenedTimeouts(t *testing.T) {
	s := httpserver.New("127.0.0.1:0", newTestLogger())
	hs := s.HTTPServer()

	if hs.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 5s", hs.ReadHeaderTimeout)
	}
	if hs.ReadTimeout != 15*time.Second {
		t.Errorf("ReadTimeout = %v, want 15s", hs.ReadTimeout)
	}
	if hs.WriteTimeout != 30*time.Second {
		t.Errorf("WriteTimeout = %v, want 30s", hs.WriteTimeout)
	}
	if hs.IdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout = %v, want 120s", hs.IdleTimeout)
	}
	if hs.MaxHeaderBytes != 1<<20 {
		t.Errorf("MaxHeaderBytes = %v, want 1 MiB", hs.MaxHeaderBytes)
	}
}

