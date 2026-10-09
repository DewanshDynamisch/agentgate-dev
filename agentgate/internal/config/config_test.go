package config

import (
	"log/slog"
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
