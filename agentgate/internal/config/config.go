// Package config provides AgentGate's typed, validated startup
// configuration, read from the process environment.
//
// Only fields actually consumed by the current scaffold are present here.
// Later tasks (Cedar policy loading, Postgres audit/policy storage, the
// ext_authz gRPC listener, claims mapping, etc. — see
// docs/PHASES/PHASE-01-MINIMAL-E2E-ENFORCEMENT.md) will extend this struct
// as those packages are implemented. It is deliberately not pre-populated
// with fields nothing reads yet.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

const envPrefix = "AGENTGATE_"

const (
	defaultEnvironment     = "development"
	defaultHTTPAddr        = ":8090"
	defaultAuthzGRPCAddr   = ":9001"
	defaultLogLevel        = "info"
	defaultShutdownTimeout = 10 * time.Second
	defaultAdminToken      = "agentgate-admin-secret-dev"
)

// Config holds AgentGate's startup configuration.
type Config struct {
	// Environment is a free-form deployment label (e.g. "development",
	// "staging", "production"). It is not validated against a fixed set;
	// it exists for logging/observability context only.
	Environment string

	// HTTPAddr is the listen address for AgentGate's HTTP surface.
	// Per docs/PROJECT_DEFINITION.md §2 this is the :8090 HTTP UI + API
	// port; today it only serves health/readiness.
	HTTPAddr string

	// AuthzGRPCAddr is the listen address for AgentGate's Envoy v3 ext_authz gRPC service.
	AuthzGRPCAddr string

	// LogLevel controls the minimum severity written by the structured
	// logger (see internal/logging).
	LogLevel slog.Level

	// ShutdownTimeout bounds how long graceful shutdown waits for
	// in-flight requests to finish before the process exits anyway.
	ShutdownTimeout time.Duration

	// AdminToken is the secret required for authenticated policy governance mutations.
	AdminToken string

	// DatabaseURL is the PostgreSQL connection string for persistent policy storage.
	// If empty, AgentGate runs with an in-memory policy store.
	DatabaseURL string

	// ToolBootstrapFile is an optional JSON file that seeds the tool catalog when it is empty.
	ToolBootstrapFile string

	// CredentialSigningKeyFile is the PEM RSA private key used to sign downstream credentials.
	CredentialSigningKeyFile string
	CredentialIssuer         string
	CredentialKeyID          string
	CredentialLifetime       time.Duration
	// CredentialAudiences maps backendID to the audience of credentials minted for it.
	CredentialAudiences         map[string]string
	RequireDownstreamCredential bool
}

// Load reads configuration from the process environment, applies defaults
// for anything unset, and validates the result. It returns a descriptive
// error rather than a partially-valid Config if validation fails, so
// AgentGate fails clearly at startup instead of running with bad config.
func Load() (Config, error) {
	var cfg Config

	cfg.Environment = normalizeEnvironment(getEnv(envPrefix+"ENV", defaultEnvironment))
	cfg.HTTPAddr = getEnv(envPrefix+"HTTP_ADDR", defaultHTTPAddr)

	grpcAddr := getEnv(envPrefix+"AUTHZ_GRPC_ADDR", "")
	if grpcAddr == "" {
		if port := getEnv(envPrefix+"AUTHZ_GRPC_PORT", ""); port != "" {
			if strings.HasPrefix(port, ":") {
				grpcAddr = port
			} else {
				grpcAddr = ":" + port
			}
		} else {
			grpcAddr = defaultAuthzGRPCAddr
		}
	}
	cfg.AuthzGRPCAddr = grpcAddr

	level, err := parseLogLevel(getEnv(envPrefix+"LOG_LEVEL", defaultLogLevel))
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	cfg.LogLevel = level

	timeoutStr := getEnv(envPrefix+"SHUTDOWN_TIMEOUT", defaultShutdownTimeout.String())
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid %sSHUTDOWN_TIMEOUT %q: %w", envPrefix, timeoutStr, err)
	}
	cfg.ShutdownTimeout = timeout

	cfg.AdminToken = getEnv(envPrefix+"ADMIN_TOKEN", defaultAdminToken)
	cfg.DatabaseURL = getEnv(envPrefix+"DATABASE_URL", "")
	cfg.ToolBootstrapFile = strings.TrimSpace(getEnv(envPrefix+"TOOL_BOOTSTRAP_FILE", ""))

	cfg.CredentialSigningKeyFile = getEnv(envPrefix+"CREDENTIAL_SIGNING_KEY_FILE", "")
	cfg.CredentialIssuer = getEnv(envPrefix+"CREDENTIAL_ISSUER", "agentgate")
	cfg.CredentialKeyID = getEnv(envPrefix+"CREDENTIAL_KEY_ID", "agentgate-cred-1")

	lifetimeStr := getEnv(envPrefix+"CREDENTIAL_LIFETIME", (60 * time.Second).String())
	lifetime, err := time.ParseDuration(lifetimeStr)
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid %sCREDENTIAL_LIFETIME %q: %w", envPrefix, lifetimeStr, err)
	}
	cfg.CredentialLifetime = lifetime

	audiences, err := parseAudiences(getEnv(envPrefix+"CREDENTIAL_AUDIENCES", ""))
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	cfg.CredentialAudiences = audiences

	requireStr := getEnv(envPrefix+"REQUIRE_DOWNSTREAM_CREDENTIAL", "true")
	require, err := strconv.ParseBool(requireStr)
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid %sREQUIRE_DOWNSTREAM_CREDENTIAL %q: %w", envPrefix, requireStr, err)
	}
	cfg.RequireDownstreamCredential = require

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// Validate fails clearly when required configuration is missing or
// nonsensical, rather than letting AgentGate start in an undefined state.
func (c Config) Validate() error {
	if strings.TrimSpace(c.HTTPAddr) == "" {
		return fmt.Errorf("config: %sHTTP_ADDR must not be empty", envPrefix)
	}
	if strings.TrimSpace(c.AuthzGRPCAddr) == "" {
		return fmt.Errorf("config: %sAUTHZ_GRPC_ADDR must not be empty", envPrefix)
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("config: %sSHUTDOWN_TIMEOUT must be positive, got %s", envPrefix, c.ShutdownTimeout)
	}
	if strings.TrimSpace(c.AdminToken) == "" {
		return fmt.Errorf("config: %sADMIN_TOKEN must not be empty", envPrefix)
	}
	return nil
}

func normalizeEnvironment(s string) string {
	env := strings.ToLower(strings.TrimSpace(s))
	switch env {
	case "":
		return defaultEnvironment
	case "prod":
		return "production"
	}
	return env
}

func parseAudiences(s string) (map[string]string, error) {
	out := map[string]string{}
	if strings.TrimSpace(s) == "" {
		return out, nil
	}
	for _, entry := range strings.Split(s, ",") {
		backend, aud, ok := strings.Cut(strings.TrimSpace(entry), "=")
		backend, aud = strings.TrimSpace(backend), strings.TrimSpace(aud)
		if !ok || backend == "" || aud == "" {
			return nil, fmt.Errorf("invalid %sCREDENTIAL_AUDIENCES entry %q (want backendID=audience)", envPrefix, entry)
		}
		out[backend] = aud
	}
	return out, nil
}

func parseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid %sLOG_LEVEL %q (want debug|info|warn|error)", envPrefix, s)
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
