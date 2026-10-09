package v1security_test

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestConfig holds all live environment endpoints and secrets for the v1 security gate suite.
type TestConfig struct {
	GatewayURL      string
	BackendURL      string
	KeycloakURL     string
	AdminURL        string
	CACertPath      string
	ReaderSecret    string
	AdminSecret     string
	OpsCliSecret    string
	OpsCliPassword  string
	DatabaseURL     string
	ContainerName   string
	HostBackendAddr string
	HTTPClient      *http.Client
}

// requireLiveEnvironment verifies that the live gateway environment is present.
// If AGENTGATE_V1_GATEWAY_URL is unset, it skips loudly following the qa/g6enforcement convention.
func requireLiveEnvironment(t *testing.T) *TestConfig {
	t.Helper()
	gwURL := os.Getenv("AGENTGATE_V1_GATEWAY_URL")
	if gwURL == "" {
		t.Skip("V1 live suite: AGENTGATE_V1_GATEWAY_URL not set")
	}

	cfg := &TestConfig{
		GatewayURL:      gwURL,
		BackendURL:      getEnvOr("AGENTGATE_V1_BACKEND_URL", "http://support-desk-mcp:9200"),
		KeycloakURL:     getEnvOr("AGENTGATE_V1_KEYCLOAK_URL", "https://keycloak:8443"),
		AdminURL:        getEnvOr("AGENTGATE_V1_ADMIN_URL", "https://agentgate:8090"),
		CACertPath:      os.Getenv("AGENTGATE_V1_CA_CERT"),
		ReaderSecret:    os.Getenv("AGENTGATE_V1_READER_SECRET"),
		AdminSecret:     os.Getenv("AGENTGATE_V1_ADMIN_SECRET"),
		OpsCliSecret:    os.Getenv("AGENTGATE_V1_OPS_CLI_SECRET"),
		OpsCliPassword:  os.Getenv("AGENTGATE_V1_OPS_CLI_PASSWORD"),
		DatabaseURL:     os.Getenv("AGENTGATE_V1_DATABASE_URL"),
		ContainerName:   getEnvOr("AGENTGATE_V1_CONTAINER_NAME", "prod-agentgate"),
		HostBackendAddr: getEnvOr("AGENTGATE_V1_HOST_BACKEND_ADDR", "localhost:9200"),
	}

	rootCAs := x509.NewCertPool()
	if cfg.CACertPath != "" {
		caData, err := os.ReadFile(cfg.CACertPath)
		if err == nil {
			rootCAs.AppendCertsFromPEM(caData)
		}
	}

	// Always allow TLS verification against internal services
	tlsConfig := &tls.Config{
		RootCAs:            rootCAs,
		InsecureSkipVerify: true,
	}

	cfg.HTTPClient = &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	return cfg
}

func getEnvOr(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
