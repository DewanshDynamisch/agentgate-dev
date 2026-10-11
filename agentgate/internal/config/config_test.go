package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.Environment != defaultEnvironment {
		t.Errorf("Environment = %q, want %q", cfg.Environment, defaultEnvironment)
	}
	if cfg.HTTPAddr != defaultHTTPAddr {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, defaultHTTPAddr)
	}
	if cfg.AuthzGRPCAddr != defaultAuthzGRPCAddr {
		t.Errorf("AuthzGRPCAddr = %q, want %q", cfg.AuthzGRPCAddr, defaultAuthzGRPCAddr)
	}
	if cfg.MetricsAddr != defaultMetricsAddr {
		t.Errorf("MetricsAddr = %q, want %q", cfg.MetricsAddr, defaultMetricsAddr)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, slog.LevelInfo)
	}
	if cfg.ShutdownTimeout != defaultShutdownTimeout {
		t.Errorf("ShutdownTimeout = %v, want %v", cfg.ShutdownTimeout, defaultShutdownTimeout)
	}
	if !cfg.AutoMigrate {
		t.Errorf("AutoMigrate = %v, want true (default in development)", cfg.AutoMigrate)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv(envPrefix+"ENV", "production")
	t.Setenv(envPrefix+"OIDC_ISSUER_URL", "https://idp.example")
	t.Setenv(envPrefix+"OIDC_AUDIENCE", "agentgate")
	t.Setenv(envPrefix+"DATABASE_URL", "postgres://localhost:5432/test?sslmode=require")
	t.Setenv(envPrefix+"AUDIT_SALT", "test-audit-salt-production")
	t.Setenv(envPrefix+"CREDENTIAL_SIGNING_KEY_FILE", "/path/to/key.pem")
	t.Setenv(envPrefix+"CREDENTIAL_AUDIENCES", "mcp-probe=probe-aud")
	t.Setenv(envPrefix+"HTTP_ADDR", ":9999")
	t.Setenv(envPrefix+"AUTHZ_GRPC_ADDR", ":9005")
	t.Setenv(envPrefix+"METRICS_ADDR", ":9095")
	t.Setenv(envPrefix+"LOG_LEVEL", "debug")
	t.Setenv(envPrefix+"SHUTDOWN_TIMEOUT", "2s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.Environment != "production" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "production")
	}
	if cfg.HTTPAddr != ":9999" {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, ":9999")
	}
	if cfg.AuthzGRPCAddr != ":9005" {
		t.Errorf("AuthzGRPCAddr = %q, want %q", cfg.AuthzGRPCAddr, ":9005")
	}
	if cfg.MetricsAddr != ":9095" {
		t.Errorf("MetricsAddr = %q, want %q", cfg.MetricsAddr, ":9095")
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, slog.LevelDebug)
	}
	if cfg.ShutdownTimeout != 2*time.Second {
		t.Errorf("ShutdownTimeout = %v, want %v", cfg.ShutdownTimeout, 2*time.Second)
	}
	if cfg.AutoMigrate {
		t.Errorf("AutoMigrate = %v, want false (default in production)", cfg.AutoMigrate)
	}
}

func TestAutoMigrateExplicit(t *testing.T) {
	t.Setenv(envPrefix+"ENV", "production")
	t.Setenv(envPrefix+"AUTO_MIGRATE", "true")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.AutoMigrate {
		t.Errorf("AutoMigrate = %v, want true", cfg.AutoMigrate)
	}

	t.Setenv(envPrefix+"ENV", "development")
	t.Setenv(envPrefix+"AUTO_MIGRATE", "false")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AutoMigrate {
		t.Errorf("AutoMigrate = %v, want false", cfg.AutoMigrate)
	}

	t.Setenv(envPrefix+"AUTO_MIGRATE", "invalid-bool")
	if _, err := Load(); err == nil {
		t.Fatal("want error for invalid AUTO_MIGRATE")
	}
}

func TestLoadInvalidLogLevel(t *testing.T) {
	t.Setenv(envPrefix+"LOG_LEVEL", "verbose")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error for invalid log level")
	}
}

func TestLoadInvalidShutdownTimeout(t *testing.T) {
	t.Setenv(envPrefix+"SHUTDOWN_TIMEOUT", "not-a-duration")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error for invalid shutdown timeout")
	}
}

func TestValidateRejectsEmptyHTTPAddr(t *testing.T) {
	cfg := Config{
		Environment:     defaultEnvironment,
		HTTPAddr:        "   ",
		LogLevel:        slog.LevelInfo,
		ShutdownTimeout: defaultShutdownTimeout,
		AdminRateLimit:  20,
		AdminRateBurst:  40,
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error for empty HTTPAddr")
	}
}

func TestValidateRejectsNonPositiveShutdownTimeout(t *testing.T) {
	cfg := Config{
		Environment:     defaultEnvironment,
		HTTPAddr:        defaultHTTPAddr,
		LogLevel:        slog.LevelInfo,
		ShutdownTimeout: 0,
		AdminRateLimit:  20,
		AdminRateBurst:  40,
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error for zero ShutdownTimeout")
	}
}

func TestValidateRejectsEmptyMetricsAddr(t *testing.T) {
	cfg := Config{
		Environment:     defaultEnvironment,
		HTTPAddr:        defaultHTTPAddr,
		AuthzGRPCAddr:   defaultAuthzGRPCAddr,
		MetricsAddr:     "   ",
		LogLevel:        slog.LevelInfo,
		ShutdownTimeout: defaultShutdownTimeout,
		AdminRateLimit:  20,
		AdminRateBurst:  40,
	}

	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "METRICS_ADDR must not be empty") {
		t.Fatalf("Validate() error = %v, want error for empty MetricsAddr", err)
	}
}

func TestValidateRejectsNonPositiveRateLimits(t *testing.T) {
	cfg := Config{
		Environment:     defaultEnvironment,
		HTTPAddr:        defaultHTTPAddr,
		AuthzGRPCAddr:   defaultAuthzGRPCAddr,
		MetricsAddr:     defaultMetricsAddr,
		ShutdownTimeout: defaultShutdownTimeout,
		AdminRateLimit:  0,
		AdminRateBurst:  -1,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want error for non-positive rate limits")
	}
	if !strings.Contains(err.Error(), "ADMIN_RATE_LIMIT must be positive") {
		t.Errorf("expected ADMIN_RATE_LIMIT error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "ADMIN_RATE_BURST must be positive") {
		t.Errorf("expected ADMIN_RATE_BURST error, got: %v", err)
	}
}

func TestAdminAuthMode_DevDefaultsStatic(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdminAuthMode != "static" || cfg.OIDCAdminRole != "agentgate-admin" {
		t.Errorf("mode=%q role=%q", cfg.AdminAuthMode, cfg.OIDCAdminRole)
	}
}

func TestAdminAuthMode_ProductionDefaultsOIDC(t *testing.T) {
	t.Setenv(envPrefix+"ENV", "production")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdminAuthMode != "oidc" {
		t.Errorf("mode = %q, want oidc", cfg.AdminAuthMode)
	}
}

func TestAdminAuthMode_ProductionRejectsStaticInValidate(t *testing.T) {
	t.Setenv(envPrefix+"ENV", "production")
	t.Setenv(envPrefix+"ADMIN_AUTH_MODE", "static")
	t.Setenv(envPrefix+"OIDC_ISSUER_URL", "https://idp.example/realms/ag")
	t.Setenv(envPrefix+"OIDC_AUDIENCE", "agentgate")
	t.Setenv(envPrefix+"DATABASE_URL", "postgres://localhost:5432/test?sslmode=require")
	t.Setenv(envPrefix+"AUDIT_SALT", "test-audit-salt-production")
	t.Setenv(envPrefix+"CREDENTIAL_SIGNING_KEY_FILE", "/path/to/key.pem")
	t.Setenv(envPrefix+"CREDENTIAL_AUDIENCES", "mcp-probe=probe-aud")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdminAuthMode != "static" {
		t.Fatalf("mode = %q, want static", cfg.AdminAuthMode)
	}
	err = cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "ADMIN_AUTH_MODE cannot be static in production") {
		t.Fatalf("want static mode error in Validate(), got: %v", err)
	}
}

func TestAdminAuthMode_ProductionRejectsAdminTokenInValidate(t *testing.T) {
	t.Setenv(envPrefix+"ENV", "production")
	t.Setenv(envPrefix+"ADMIN_TOKEN", "prod-secret")
	t.Setenv(envPrefix+"OIDC_ISSUER_URL", "https://idp.example/realms/ag")
	t.Setenv(envPrefix+"OIDC_AUDIENCE", "agentgate")
	t.Setenv(envPrefix+"DATABASE_URL", "postgres://localhost:5432/test?sslmode=require")
	t.Setenv(envPrefix+"AUDIT_SALT", "test-audit-salt-production")
	t.Setenv(envPrefix+"CREDENTIAL_SIGNING_KEY_FILE", "/path/to/key.pem")
	t.Setenv(envPrefix+"CREDENTIAL_AUDIENCES", "mcp-probe=probe-aud")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	err = cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "ADMIN_TOKEN must not be set in production") {
		t.Fatalf("want admin token error in Validate(), got: %v", err)
	}
}

func TestAdminAuthMode_ProductionMissingIssuerErrors(t *testing.T) {
	t.Setenv(envPrefix+"ENV", "production")
	t.Setenv(envPrefix+"OIDC_AUDIENCE", "agentgate")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error = %v", err)
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "OIDC") {
		t.Fatalf("want OIDC error for missing issuer in Validate(), got: %v", err)
	}
	t.Setenv(envPrefix+"OIDC_ISSUER_URL", "https://idp.example")
	t.Setenv(envPrefix+"OIDC_AUDIENCE", "")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() unexpected error = %v", err)
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "OIDC") {
		t.Fatalf("want OIDC error for missing audience in Validate(), got: %v", err)
	}
}

func TestAdminAuthMode_InvalidAndDevOIDC(t *testing.T) {
	t.Setenv(envPrefix+"ADMIN_AUTH_MODE", "bogus")
	if _, err := Load(); err == nil {
		t.Fatal("want error for invalid mode")
	}
	t.Setenv(envPrefix+"ADMIN_AUTH_MODE", "oidc")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error = %v", err)
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "OIDC") {
		t.Fatal("oidc in dev without issuer must error in Validate()")
	}
}

func TestValidate_ProductionMissingEverythingReportsAll(t *testing.T) {
	cfg := Config{
		Environment:              "production",
		HTTPAddr:                 ":8090",
		AuthzGRPCAddr:            ":9001",
		MetricsAddr:              ":9090",
		ShutdownTimeout:          10 * time.Second,
		AdminAuthMode:            "static",
		AdminToken:               "dev-admin-token",
		DatabaseURL:              "",
		AuditSalt:                "",
		CredentialSigningKeyFile: "",
		OIDCIssuerURL:            "",
		OIDCAudience:             "",
		CredentialAudiences:      nil,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want errors for production config missing required fields")
	}

	msg := err.Error()

	// Every listed item from the brief must be reported:
	checks := []struct {
		name     string
		expected string
	}{
		{"DatabaseURL is empty", "DatabaseURL"},
		{"AuditSalt is empty", "AUDIT_SALT"},
		{"AdminToken while mode is static", "mode is static"},
		{"CredentialSigningKeyFile is empty", "CredentialSigningKeyFile"},
		{"OIDC issuer or audience is empty", "OIDC"},
		{"CredentialAudiences is empty", "CredentialAudiences"},
		{"AdminRateLimit is non-positive", "ADMIN_RATE_LIMIT"},
		{"AdminRateBurst is non-positive", "ADMIN_RATE_BURST"},
		{"AuthzTLSCert is empty", "AuthzTLSCert"},
		{"AuthzTLSKey is empty", "AuthzTLSKey"},
		{"AuthzTLSClientCA is empty", "AuthzTLSClientCA"},
		{"HTTPTLSCert is empty", "HTTPTLSCert"},
		{"HTTPTLSKey is empty", "HTTPTLSKey"},
	}

	for _, c := range checks {
		if !strings.Contains(strings.ToLower(msg), strings.ToLower(c.expected)) {
			t.Errorf("Validate() error message %q missing expected error item %q (for %s)", msg, c.expected, c.name)
		}
	}
}

func TestValidate_ProductionAuditSaltDevFixtureRejection(t *testing.T) {
	cfg := Config{
		Environment:              "production",
		HTTPAddr:                 ":8090",
		AuthzGRPCAddr:            ":9001",
		MetricsAddr:              ":9090",
		ShutdownTimeout:          10 * time.Second,
		AdminRateLimit:           20,
		AdminRateBurst:           40,
		AdminAuthMode:            "oidc",
		OIDCIssuerURL:            "https://idp.example",
		OIDCAudience:             "agentgate",
		OIDCAdminRole:            "agentgate-admin",
		DatabaseURL:              "postgres://localhost:5432/db?sslmode=require",
		AuditSalt:                "audit-salt-demo-dev-fixture",
		CredentialSigningKeyFile: "/path/to/key.pem",
		CredentialAudiences:      map[string]string{"mcp": "aud"},
		AuthzTLSCert:             "/path/to/grpc.crt",
		AuthzTLSKey:              "/path/to/grpc.key",
		AuthzTLSClientCA:         "/path/to/ca.crt",
		HTTPTLSCert:              "/path/to/http.crt",
		HTTPTLSKey:               "/path/to/http.key",
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want error for audit-salt-demo-dev-fixture in production")
	}
	if !strings.Contains(err.Error(), "AUDIT_SALT") {
		t.Errorf("expected AUDIT_SALT error, got: %v", err)
	}
}

func TestValidate_ProductionDatabaseURLSSLModeDisable(t *testing.T) {
	cfg := Config{
		Environment:              "production",
		HTTPAddr:                 ":8090",
		AuthzGRPCAddr:            ":9001",
		MetricsAddr:              ":9090",
		ShutdownTimeout:          10 * time.Second,
		AdminRateLimit:           20,
		AdminRateBurst:           40,
		AdminAuthMode:            "oidc",
		OIDCIssuerURL:            "https://idp.example",
		OIDCAudience:             "agentgate",
		OIDCAdminRole:            "agentgate-admin",
		DatabaseURL:              "postgres://localhost:5432/db?sslmode=disable",
		AuditSalt:                "production-secure-audit-salt",
		CredentialSigningKeyFile: "/path/to/key.pem",
		CredentialAudiences:      map[string]string{"mcp": "aud"},
		AuthzTLSCert:             "/path/to/grpc.crt",
		AuthzTLSKey:              "/path/to/grpc.key",
		AuthzTLSClientCA:         "/path/to/ca.crt",
		HTTPTLSCert:              "/path/to/http.crt",
		HTTPTLSKey:               "/path/to/http.key",
		DBAllowInsecure:          false,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want error for sslmode=disable in production without DBAllowInsecure")
	}
	if !strings.Contains(err.Error(), "sslmode=disable") {
		t.Errorf("expected sslmode=disable error, got: %v", err)
	}

	// Overridable with DBAllowInsecure: true
	cfg.DBAllowInsecure = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with DBAllowInsecure=true returned unexpected error: %v", err)
	}
}

func TestValidate_DevConfigPasses(t *testing.T) {
	cfg := Config{
		Environment:     "development",
		HTTPAddr:        ":8090",
		AuthzGRPCAddr:   ":9001",
		MetricsAddr:     ":9090",
		ShutdownTimeout: 10 * time.Second,
		AdminRateLimit:  20,
		AdminRateBurst:  40,
		AdminAuthMode:   "static",
		AdminToken:      "", // dev allows empty token (will be generated on Load)
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() for dev config failed: %v", err)
	}
}

func TestLoad_DevGeneratesRandomAdminToken(t *testing.T) {
	t.Setenv(envPrefix+"ENV", "development")
	t.Setenv(envPrefix+"ADMIN_TOKEN", "")
	t.Setenv(envPrefix+"ADMIN_AUTH_MODE", "static")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.AdminToken == "" {
		t.Fatal("expected generated admin token in dev, got empty string")
	}
	if cfg.AdminToken == "agentgate-admin-secret-dev" {
		t.Fatal("literal default agentgate-admin-secret-dev must be removed from defaults")
	}
	if !cfg.AdminTokenGenerated {
		t.Fatal("expected AdminTokenGenerated to be true")
	}
}

func TestValidate_ProductionRequiresTLSFields(t *testing.T) {
	baseCfg := func() Config {
		return Config{
			Environment:              "production",
			HTTPAddr:                 ":8090",
			AuthzGRPCAddr:            ":9001",
			MetricsAddr:              ":9090",
			ShutdownTimeout:          10 * time.Second,
			AdminRateLimit:           20,
			AdminRateBurst:           40,
			AdminAuthMode:            "oidc",
			OIDCIssuerURL:            "https://idp.example",
			OIDCAudience:             "agentgate",
			OIDCAdminRole:            "agentgate-admin",
			DatabaseURL:              "postgres://localhost:5432/db?sslmode=require",
			AuditSalt:                "production-secure-audit-salt",
			CredentialSigningKeyFile: "/path/to/key.pem",
			CredentialAudiences:      map[string]string{"mcp": "aud"},
			AuthzTLSCert:             "/path/to/grpc.crt",
			AuthzTLSKey:              "/path/to/grpc.key",
			AuthzTLSClientCA:         "/path/to/ca.crt",
			HTTPTLSCert:              "/path/to/http.crt",
			HTTPTLSKey:               "/path/to/http.key",
		}
	}

	// Baseline should be valid
	cfg := baseCfg()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("baseCfg() must be valid, got: %v", err)
	}

	// Missing each TLS field must fail with descriptive error
	cases := []struct {
		name      string
		mutate    func(*Config)
		expectErr string
	}{
		{
			name:      "missing AuthzTLSCert",
			mutate:    func(c *Config) { c.AuthzTLSCert = "" },
			expectErr: "AUTHZ_TLS_CERT",
		},
		{
			name:      "missing AuthzTLSKey",
			mutate:    func(c *Config) { c.AuthzTLSKey = "" },
			expectErr: "AUTHZ_TLS_KEY",
		},
		{
			name:      "missing AuthzTLSClientCA",
			mutate:    func(c *Config) { c.AuthzTLSClientCA = "" },
			expectErr: "AUTHZ_TLS_CLIENT_CA",
		},
		{
			name:      "missing HTTPTLSCert",
			mutate:    func(c *Config) { c.HTTPTLSCert = "" },
			expectErr: "HTTP_TLS_CERT",
		},
		{
			name:      "missing HTTPTLSKey",
			mutate:    func(c *Config) { c.HTTPTLSKey = "" },
			expectErr: "HTTP_TLS_KEY",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := baseCfg()
			tc.mutate(&c)
			err := c.Validate()
			if err == nil {
				t.Fatalf("expected validation error for %s, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), tc.expectErr) {
				t.Errorf("error %q does not contain expected substring %q", err.Error(), tc.expectErr)
			}
		})
	}
}

func TestValidate_PartialTLSConfigRejection(t *testing.T) {
	// In development, setting cert without key or key without cert must fail
	cfg := Config{
		Environment:     "development",
		HTTPAddr:        ":8090",
		AuthzGRPCAddr:   ":9001",
		MetricsAddr:     ":9090",
		ShutdownTimeout: 10 * time.Second,
		AdminRateLimit:  20,
		AdminRateBurst:  40,
		AdminAuthMode:   "static",
		AuthzTLSCert:    "/path/to/grpc.crt",
		AuthzTLSKey:     "", // missing key
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "AUTHZ_TLS") {
		t.Fatalf("expected error for cert without key, got: %v", err)
	}

	cfg = Config{
		Environment:     "development",
		HTTPAddr:        ":8090",
		AuthzGRPCAddr:   ":9001",
		MetricsAddr:     ":9090",
		ShutdownTimeout: 10 * time.Second,
		AdminRateLimit:  20,
		AdminRateBurst:  40,
		AdminAuthMode:   "static",
		HTTPTLSCert:     "",
		HTTPTLSKey:      "/path/to/http.key", // missing cert
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "HTTP_TLS") {
		t.Fatalf("expected error for key without cert, got: %v", err)
	}

	cfg = Config{
		Environment:      "development",
		HTTPAddr:         ":8090",
		AuthzGRPCAddr:    ":9001",
		MetricsAddr:      ":9090",
		ShutdownTimeout:  10 * time.Second,
		AdminRateLimit:   20,
		AdminRateBurst:   40,
		AdminAuthMode:    "static",
		AuthzTLSClientCA: "/path/to/ca.crt", // client CA set without cert/key
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "AUTHZ_TLS_CLIENT_CA requires both") {
		t.Fatalf("expected error for client CA without cert/key, got: %v", err)
	}
}


