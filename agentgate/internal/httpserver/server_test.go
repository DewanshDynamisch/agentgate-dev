package httpserver_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
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

func generateSelfSignedCert(t *testing.T, dir string) (certFile, keyFile string, certPEM []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	certFile = filepath.Join(dir, "server.crt")
	keyFile = filepath.Join(dir, "server.key")
	if err := os.WriteFile(certFile, pemBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile, pemBytes
}

func TestServerTLS_HTTPServing(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile, certPEM := generateSelfSignedCert(t, dir)

	s := httpserver.New("127.0.0.1:0", newTestLogger())
	s.SetTLS(certFile, keyFile)
	if !s.IsTLS() {
		t.Fatal("expected IsTLS to be true")
	}

	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	})

	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(certPEM)
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: caPool,
			},
		},
		Timeout: 3 * time.Second,
	}

	resp, err := client.Get("https://" + s.Addr() + "/healthz")
	if err != nil {
		t.Fatalf("GET https://healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

