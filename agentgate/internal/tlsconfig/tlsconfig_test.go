package tlsconfig_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Dynamisch-LLC/agentgate/internal/tlsconfig"
	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type pkiFixtures struct {
	caCertFile     string
	serverCertFile string
	serverKeyFile  string
	clientCertFile string
	clientKeyFile  string
	rogueCertFile  string
	rogueKeyFile   string
	caCertPEM      []byte
	serverCertPEM  []byte
	clientCertPEM  []byte
	caPriv         *ecdsa.PrivateKey
	caTemplate     *x509.Certificate
}

func generateCertPEM(priv *ecdsa.PrivateKey, template, parent *x509.Certificate, parentPriv *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.CreateCertificate(rand.Reader, template, parent, &priv.PublicKey, parentPriv)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

func encodeKeyPEM(priv *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), nil
}

func setupPKI(t *testing.T, dir string) *pkiFixtures {
	t.Helper()

	// 1. Root CA
	caPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ca key: %v", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"AgentGate Test CA"},
			CommonName:   "AgentGate Test CA",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caPEM, err := generateCertPEM(caPriv, caTemplate, caTemplate, caPriv)
	if err != nil {
		t.Fatalf("generate ca cert: %v", err)
	}
	caCertFile := filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(caCertFile, caPEM, 0600); err != nil {
		t.Fatalf("write ca cert: %v", err)
	}

	// 2. Server Cert
	serverPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			CommonName: "localhost",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
		BasicConstraintsValid: true,
	}
	serverPEM, err := generateCertPEM(serverPriv, serverTemplate, caTemplate, caPriv)
	if err != nil {
		t.Fatalf("generate server cert: %v", err)
	}
	serverKeyPEM, err := encodeKeyPEM(serverPriv)
	if err != nil {
		t.Fatalf("encode server key: %v", err)
	}
	serverCertFile := filepath.Join(dir, "server.crt")
	serverKeyFile := filepath.Join(dir, "server.key")
	if err := os.WriteFile(serverCertFile, serverPEM, 0600); err != nil {
		t.Fatalf("write server cert: %v", err)
	}
	if err := os.WriteFile(serverKeyFile, serverKeyPEM, 0600); err != nil {
		t.Fatalf("write server key: %v", err)
	}

	// 3. Client Cert (signed by Root CA)
	clientPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}
	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject: pkix.Name{
			CommonName: "client-agent",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	clientPEM, err := generateCertPEM(clientPriv, clientTemplate, caTemplate, caPriv)
	if err != nil {
		t.Fatalf("generate client cert: %v", err)
	}
	clientKeyPEM, err := encodeKeyPEM(clientPriv)
	if err != nil {
		t.Fatalf("encode client key: %v", err)
	}
	clientCertFile := filepath.Join(dir, "client.crt")
	clientKeyFile := filepath.Join(dir, "client.key")
	if err := os.WriteFile(clientCertFile, clientPEM, 0600); err != nil {
		t.Fatalf("write client cert: %v", err)
	}
	if err := os.WriteFile(clientKeyFile, clientKeyPEM, 0600); err != nil {
		t.Fatalf("write client key: %v", err)
	}

	// 4. Rogue CA and Rogue Client Cert
	roguePriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate rogue key: %v", err)
	}
	rogueCATemplate := &x509.Certificate{
		SerialNumber: big.NewInt(99),
		Subject: pkix.Name{
			CommonName: "Rogue CA",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	rogueClientPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate rogue client key: %v", err)
	}
	rogueClientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(100),
		Subject: pkix.Name{
			CommonName: "rogue-agent",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	rogueClientPEM, err := generateCertPEM(rogueClientPriv, rogueClientTemplate, rogueCATemplate, roguePriv)
	if err != nil {
		t.Fatalf("generate rogue client cert: %v", err)
	}
	rogueClientKeyPEM, err := encodeKeyPEM(rogueClientPriv)
	if err != nil {
		t.Fatalf("encode rogue client key: %v", err)
	}
	rogueCertFile := filepath.Join(dir, "rogue.crt")
	rogueKeyFile := filepath.Join(dir, "rogue.key")
	if err := os.WriteFile(rogueCertFile, rogueClientPEM, 0600); err != nil {
		t.Fatalf("write rogue cert: %v", err)
	}
	if err := os.WriteFile(rogueKeyFile, rogueClientKeyPEM, 0600); err != nil {
		t.Fatalf("write rogue key: %v", err)
	}

	return &pkiFixtures{
		caCertFile:     caCertFile,
		serverCertFile: serverCertFile,
		serverKeyFile:  serverKeyFile,
		clientCertFile: clientCertFile,
		clientKeyFile:  clientKeyFile,
		rogueCertFile:  rogueCertFile,
		rogueKeyFile:   rogueKeyFile,
		caCertPEM:      caPEM,
		serverCertPEM:  serverPEM,
		clientCertPEM:  clientPEM,
		caPriv:         caPriv,
		caTemplate:     caTemplate,
	}
}

type mockAuthzServer struct {
	authv3.UnimplementedAuthorizationServer
}

func (m *mockAuthzServer) Check(_ context.Context, _ *authv3.CheckRequest) (*authv3.CheckResponse, error) {
	return &authv3.CheckResponse{}, nil
}

func TestServerTLS_GRPCMValidClientCertCanCallCheck(t *testing.T) {
	tmp := t.TempDir()
	fix := setupPKI(t, tmp)

	srvTLS, err := tlsconfig.ServerTLS(fix.serverCertFile, fix.serverKeyFile, fix.caCertFile)
	if err != nil {
		t.Fatalf("ServerTLS failed: %v", err)
	}

	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(srvTLS)))
	authv3.RegisterAuthorizationServer(grpcServer, &mockAuthzServer{})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		_ = grpcServer.Serve(ln)
	}()
	defer grpcServer.Stop()

	// Client with valid client cert
	clientCert, err := tls.LoadX509KeyPair(fix.clientCertFile, fix.clientKeyFile)
	if err != nil {
		t.Fatalf("load client cert: %v", err)
	}
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(fix.caCertPEM)

	clientTLS := &tls.Config{
		RootCAs:      caPool,
		Certificates: []tls.Certificate{clientCert},
		ServerName:   "localhost",
		MinVersion:   tls.VersionTLS12,
	}

	conn, err := grpc.NewClient(ln.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	client := authv3.NewAuthorizationClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := client.Check(ctx, &authv3.CheckRequest{})
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil CheckResponse")
	}
}

func TestServerTLS_ClientWithoutCertFailsHandshake(t *testing.T) {
	tmp := t.TempDir()
	fix := setupPKI(t, tmp)

	srvTLS, err := tlsconfig.ServerTLS(fix.serverCertFile, fix.serverKeyFile, fix.caCertFile)
	if err != nil {
		t.Fatalf("ServerTLS failed: %v", err)
	}

	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(srvTLS)))
	authv3.RegisterAuthorizationServer(grpcServer, &mockAuthzServer{})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		_ = grpcServer.Serve(ln)
	}()
	defer grpcServer.Stop()

	// Client without cert
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(fix.caCertPEM)

	clientTLS := &tls.Config{
		RootCAs:    caPool,
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	}

	conn, err := grpc.NewClient(ln.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	client := authv3.NewAuthorizationClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = client.Check(ctx, &authv3.CheckRequest{})
	if err == nil {
		t.Fatal("expected Check to fail when client provides no certificate, but it succeeded")
	}
}

func TestServerTLS_ClientCertFromAnotherCAFailsHandshake(t *testing.T) {
	tmp := t.TempDir()
	fix := setupPKI(t, tmp)

	srvTLS, err := tlsconfig.ServerTLS(fix.serverCertFile, fix.serverKeyFile, fix.caCertFile)
	if err != nil {
		t.Fatalf("ServerTLS failed: %v", err)
	}

	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(srvTLS)))
	authv3.RegisterAuthorizationServer(grpcServer, &mockAuthzServer{})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		_ = grpcServer.Serve(ln)
	}()
	defer grpcServer.Stop()

	// Client with rogue cert
	rogueCert, err := tls.LoadX509KeyPair(fix.rogueCertFile, fix.rogueKeyFile)
	if err != nil {
		t.Fatalf("load rogue cert: %v", err)
	}
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(fix.caCertPEM)

	clientTLS := &tls.Config{
		RootCAs:      caPool,
		Certificates: []tls.Certificate{rogueCert},
		ServerName:   "localhost",
		MinVersion:   tls.VersionTLS12,
	}

	conn, err := grpc.NewClient(ln.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	client := authv3.NewAuthorizationClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = client.Check(ctx, &authv3.CheckRequest{})
	if err == nil {
		t.Fatal("expected Check to fail with rogue client certificate, but it succeeded")
	}
}

func TestServerTLS_RotatingServerCertFilePickedUpAfterCacheExpiry(t *testing.T) {
	tmp := t.TempDir()
	fix := setupPKI(t, tmp)

	var mu sync.Mutex
	fakeTime := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return fakeTime
	}

	cacheTTL := 1 * time.Minute
	srvTLS, err := tlsconfig.ServerTLSWithClock(fix.serverCertFile, fix.serverKeyFile, fix.caCertFile, cacheTTL, clock)
	if err != nil {
		t.Fatalf("ServerTLSWithClock failed: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	tlsLn := tls.NewListener(ln, srvTLS)
	defer tlsLn.Close()

	go func() {
		for {
			conn, err := tlsLn.Accept()
			if err != nil {
				return
			}
			if tc, ok := conn.(*tls.Conn); ok {
				_ = tc.Handshake()
			}
			_ = conn.Close()
		}
	}()

	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(fix.caCertPEM)
	clientCert, err := tls.LoadX509KeyPair(fix.clientCertFile, fix.clientKeyFile)
	if err != nil {
		t.Fatalf("load client cert: %v", err)
	}

	clientTLS := &tls.Config{
		RootCAs:      caPool,
		Certificates: []tls.Certificate{clientCert},
		ServerName:   "localhost",
		MinVersion:   tls.VersionTLS12,
	}

	dialServer := func() *big.Int {
		conn, err := tls.Dial("tcp", ln.Addr().String(), clientTLS)
		if err != nil {
			t.Fatalf("dial TLS: %v", err)
		}
		defer conn.Close()
		state := conn.ConnectionState()
		if len(state.PeerCertificates) == 0 {
			t.Fatal("no peer certificates returned")
		}
		return state.PeerCertificates[0].SerialNumber
	}

	// 1. Initial handshake returns server cert 1 (serial = 2)
	sn1 := dialServer()
	if sn1.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("initial cert serial = %v, want 2", sn1)
	}

	// 2. Generate a new server certificate (serial = 2026) and overwrite the files on disk
	server2Priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate server2 key: %v", err)
	}
	server2Template := &x509.Certificate{
		SerialNumber: big.NewInt(2026),
		Subject: pkix.Name{
			CommonName: "localhost",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
		BasicConstraintsValid: true,
	}
	server2PEM, err := generateCertPEM(server2Priv, server2Template, fix.caTemplate, fix.caPriv)
	if err != nil {
		t.Fatalf("generate server2 cert: %v", err)
	}
	server2KeyPEM, err := encodeKeyPEM(server2Priv)
	if err != nil {
		t.Fatalf("encode server2 key: %v", err)
	}
	if err := os.WriteFile(fix.serverCertFile, server2PEM, 0600); err != nil {
		t.Fatalf("write rotated server cert: %v", err)
	}
	if err := os.WriteFile(fix.serverKeyFile, server2KeyPEM, 0600); err != nil {
		t.Fatalf("write rotated server key: %v", err)
	}

	// 3. Before cache expiry, handshake should still return cached cert (serial = 2)
	mu.Lock()
	fakeTime = fakeTime.Add(20 * time.Second) // within 1 minute TTL
	mu.Unlock()

	snCached := dialServer()
	if snCached.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("cached cert serial = %v, want 2 before cache expiry", snCached)
	}

	// 4. Advance clock past cache TTL (e.g. 70 seconds total elapsed)
	mu.Lock()
	fakeTime = fakeTime.Add(50 * time.Second) // total +70s > 1m TTL
	mu.Unlock()

	// 5. Handshake should reload files from disk and return cert 2 (serial = 2026)
	snRotated := dialServer()
	if snRotated.Cmp(big.NewInt(2026)) != 0 {
		t.Fatalf("rotated cert serial = %v, want 2026 after cache expiry", snRotated)
	}
}

func TestServerTLS_NoClientCAAllowsClientWithoutCert(t *testing.T) {
	tmp := t.TempDir()
	fix := setupPKI(t, tmp)

	// clientCAFile is empty -> 1-way TLS (no client cert required)
	srvTLS, err := tlsconfig.ServerTLS(fix.serverCertFile, fix.serverKeyFile, "")
	if err != nil {
		t.Fatalf("ServerTLS failed: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	tlsLn := tls.NewListener(ln, srvTLS)
	defer tlsLn.Close()

	go func() {
		for {
			conn, err := tlsLn.Accept()
			if err != nil {
				return
			}
			if tc, ok := conn.(*tls.Conn); ok {
				_ = tc.Handshake()
			}
			_ = conn.Close()
		}
	}()

	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(fix.caCertPEM)

	clientTLS := &tls.Config{
		RootCAs:    caPool,
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	}

	conn, err := tls.Dial("tcp", ln.Addr().String(), clientTLS)
	if err != nil {
		t.Fatalf("dial TLS failed: %v", err)
	}
	conn.Close()
}

func TestServerTLS_InvalidFilesFailImmediately(t *testing.T) {
	tmp := t.TempDir()
	fix := setupPKI(t, tmp)

	if _, err := tlsconfig.ServerTLS("", fix.serverKeyFile, ""); err == nil {
		t.Fatal("expected error with empty certFile")
	}
	if _, err := tlsconfig.ServerTLS(fix.serverCertFile, "", ""); err == nil {
		t.Fatal("expected error with empty keyFile")
	}
	if _, err := tlsconfig.ServerTLS("/nonexistent/cert", fix.serverKeyFile, ""); err == nil {
		t.Fatal("expected error with nonexistent certFile")
	}
	if _, err := tlsconfig.ServerTLS(fix.serverCertFile, fix.serverKeyFile, "/nonexistent/ca"); err == nil {
		t.Fatal("expected error with nonexistent clientCAFile")
	}

	// CA file with garbage data
	badCA := filepath.Join(tmp, "bad-ca.crt")
	if err := os.WriteFile(badCA, []byte("not-a-certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := tlsconfig.ServerTLS(fix.serverCertFile, fix.serverKeyFile, badCA); err == nil {
		t.Fatal("expected error with invalid client CA file")
	}
}
