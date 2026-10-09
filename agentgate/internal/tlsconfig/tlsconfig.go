// Package tlsconfig constructs TLS configurations for AgentGate's
// listeners (gRPC ext_authz and admin HTTP), supporting optional mTLS
// client certificate verification and dynamic server certificate rotation
// without process restarts.
package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// ServerTLS constructs a *tls.Config for server listeners (gRPC or HTTP) with
// TLS 1.2 minimum version, optional mTLS client verification, and 1-minute
// cached certificate reloading via GetCertificate.
func ServerTLS(certFile, keyFile, clientCAFile string) (*tls.Config, error) {
	return ServerTLSWithClock(certFile, keyFile, clientCAFile, time.Minute, time.Now)
}

// ServerTLSWithClock is identical to ServerTLS but allows injecting a custom
// cache TTL and clock function for deterministic testing of certificate rotation.
func ServerTLSWithClock(certFile, keyFile, clientCAFile string, cacheTTL time.Duration, clock func() time.Time) (*tls.Config, error) {
	if certFile == "" {
		return nil, errors.New("tlsconfig: certFile must not be empty")
	}
	if keyFile == "" {
		return nil, errors.New("tlsconfig: keyFile must not be empty")
	}
	if clock == nil {
		clock = time.Now
	}
	if cacheTTL <= 0 {
		cacheTTL = time.Minute
	}

	// Verify that the initial key pair can be loaded fail-closed at startup.
	initCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("tlsconfig: load server key pair: %w", err)
	}

	var clientAuth tls.ClientAuthType = tls.NoClientCert
	var clientCAs *x509.CertPool
	if clientCAFile != "" {
		caPEM, err := os.ReadFile(clientCAFile)
		if err != nil {
			return nil, fmt.Errorf("tlsconfig: read client CA file %s: %w", clientCAFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("tlsconfig: failed to parse any certificates from client CA file %s", clientCAFile)
		}
		clientCAs = pool
		clientAuth = tls.RequireAndVerifyClientCert
	}

	reloader := &certReloader{
		certFile: certFile,
		keyFile:  keyFile,
		cacheTTL: cacheTTL,
		clock:    clock,
		cert:     &initCert,
		loadedAt: clock(),
	}

	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: reloader.GetCertificate,
		ClientAuth:     clientAuth,
		ClientCAs:      clientCAs,
	}, nil
}

type certReloader struct {
	certFile string
	keyFile  string
	cacheTTL time.Duration
	clock    func() time.Time
	mu       sync.RWMutex
	cert     *tls.Certificate
	loadedAt time.Time
}

func (r *certReloader) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	now := r.clock()

	r.mu.RLock()
	if r.cert != nil && now.Sub(r.loadedAt) < r.cacheTTL {
		c := r.cert
		r.mu.RUnlock()
		return c, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Re-check under write lock in case another goroutine just refreshed it
	now = r.clock()
	if r.cert != nil && now.Sub(r.loadedAt) < r.cacheTTL {
		return r.cert, nil
	}

	cert, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		return nil, fmt.Errorf("tlsconfig: reload certificate: %w", err)
	}

	r.cert = &cert
	r.loadedAt = now
	return r.cert, nil
}
