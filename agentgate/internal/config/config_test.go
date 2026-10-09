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
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, slog.LevelInfo)
	}
	if cfg.ShutdownTimeout != defaultShutdownTimeout {
		t.Errorf("ShutdownTimeout = %v, want %v", cfg.ShutdownTimeout, defaultShutdownTimeout)
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
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, slog.LevelDebug)
	}
	if cfg.ShutdownTimeout != 2*time.Second {
		t.Errorf("ShutdownTimeout = %v, want %v", cfg.ShutdownTimeout, 2*time.Second)
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
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error for zero ShutdownTimeout")
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

func TestAdminAuthMode_ProductionForcesOIDC(t *testing.T) {
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
	if cfg.AdminAuthMode != "oidc" {
		t.Errorf("mode = %q, want oidc", cfg.AdminAuthMode)
	}
}

func TestAdminAuthMode_ProductionMissingIssuerErrors(t *testing.T) {
	t.Setenv(envPrefix+"ENV", "production")
	t.Setenv(envPrefix+"OIDC_AUDIENCE", "agentgate")
	if _, err := Load(); err == nil {
		t.Fatal("want error for missing issuer")
	}
	t.Setenv(envPrefix+"OIDC_ISSUER_URL", "https://idp.example")
	t.Setenv(envPrefix+"OIDC_AUDIENCE", "")
	if _, err := Load(); err == nil {
		t.Fatal("want error for missing audience")
	}
}

func TestAdminAuthMode_InvalidAndDevOIDC(t *testing.T) {
	t.Setenv(envPrefix+"ADMIN_AUTH_MODE", "bogus")
	if _, err := Load(); err == nil {
		t.Fatal("want error for invalid mode")
	}
	t.Setenv(envPrefix+"ADMIN_AUTH_MODE", "oidc")
	if _, err := Load(); err == nil {
		t.Fatal("oidc in dev without issuer must error")
	}
}

func TestValidate_ProductionMissingEverythingReportsAll(t *testing.T) {
	cfg := Config{
		Environment:              "production",
		HTTPAddr:                 ":8090",
		AuthzGRPCAddr:            ":9001",
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
		ShutdownTimeout:          10 * time.Second,
		AdminAuthMode:            "oidc",
		OIDCIssuerURL:            "https://idp.example",
		OIDCAudience:             "agentgate",
		OIDCAdminRole:            "agentgate-admin",
		DatabaseURL:              "postgres://localhost:5432/db?sslmode=require",
		AuditSalt:                "audit-salt-demo-dev-fixture",
		CredentialSigningKeyFile: "/path/to/key.pem",
		CredentialAudiences:      map[string]string{"mcp": "aud"},
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
		ShutdownTimeout:          10 * time.Second,
		AdminAuthMode:            "oidc",
		OIDCIssuerURL:            "https://idp.example",
		OIDCAudience:             "agentgate",
		OIDCAdminRole:            "agentgate-admin",
		DatabaseURL:              "postgres://localhost:5432/db?sslmode=disable",
		AuditSalt:                "production-secure-audit-salt",
		CredentialSigningKeyFile: "/path/to/key.pem",
		CredentialAudiences:      map[string]string{"mcp": "aud"},
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
		ShutdownTimeout: 10 * time.Second,
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

