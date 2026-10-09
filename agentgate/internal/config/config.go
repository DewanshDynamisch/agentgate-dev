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
	"crypto/rand"
	"encoding/hex"
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
	defaultOIDCAdminRole   = "agentgate-admin"
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

	// AdminAuthMode is "static" (shared token) or "oidc". Forced to "oidc" in production.
	AdminAuthMode string
	// OIDCIssuerURL, OIDCAudience, OIDCAdminRole and OIDCRoleClaimPath configure
	// OIDC admin authentication.
	OIDCIssuerURL     string
	OIDCAudience      string
	OIDCAdminRole     string
	OIDCRoleClaimPath string

	// DatabaseURL is the PostgreSQL connection string for persistent policy storage.
	// If empty, AgentGate runs with an in-memory policy store.
	DatabaseURL     string
	DBAllowInsecure bool

	// AuditSalt is the deployment salt for argument hashing.
	AuditSalt string

	// AdminRateLimit and AdminRateBurst configure the admin API token bucket rate limiter.
	AdminRateLimit float64
	AdminRateBurst int

	// AdminTokenGenerated indicates whether a random admin token was generated for development.
	AdminTokenGenerated bool

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

	cfg.AdminToken = getEnv(envPrefix+"ADMIN_TOKEN", "")
	cfg.AuditSalt = getEnv(envPrefix+"AUDIT_SALT", "")

	dbAllowInsecureStr := getEnv(envPrefix+"DB_ALLOW_INSECURE", "false")
	dbAllowInsecure, err := strconv.ParseBool(dbAllowInsecureStr)
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid %sDB_ALLOW_INSECURE %q: %w", envPrefix, dbAllowInsecureStr, err)
	}
	cfg.DBAllowInsecure = dbAllowInsecure

	rateLimitStr := getEnv(envPrefix+"ADMIN_RATE_LIMIT", "20")
	rateLimit, err := strconv.ParseFloat(rateLimitStr, 64)
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid %sADMIN_RATE_LIMIT %q: %w", envPrefix, rateLimitStr, err)
	}
	cfg.AdminRateLimit = rateLimit

	rateBurstStr := getEnv(envPrefix+"ADMIN_RATE_BURST", "40")
	rateBurst, err := strconv.Atoi(rateBurstStr)
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid %sADMIN_RATE_BURST %q: %w", envPrefix, rateBurstStr, err)
	}
	cfg.AdminRateBurst = rateBurst

	cfg.OIDCIssuerURL = strings.TrimSpace(getEnv(envPrefix+"OIDC_ISSUER_URL", ""))
	cfg.OIDCAudience = strings.TrimSpace(getEnv(envPrefix+"OIDC_AUDIENCE", ""))
	cfg.OIDCAdminRole = strings.TrimSpace(getEnv(envPrefix+"OIDC_ADMIN_ROLE", defaultOIDCAdminRole))
	cfg.OIDCRoleClaimPath = strings.TrimSpace(getEnv(envPrefix+"OIDC_ROLE_CLAIM_PATH", ""))
	mode := strings.ToLower(strings.TrimSpace(getEnv(envPrefix+"ADMIN_AUTH_MODE", "static")))
	if mode != "static" && mode != "oidc" {
		return Config{}, fmt.Errorf("config: invalid %sADMIN_AUTH_MODE %q (want oidc|static)", envPrefix, mode)
	}
	if cfg.Environment == "production" {
		mode = "oidc" // production never uses the shared static admin token
	}
	cfg.AdminAuthMode = mode

	if cfg.Environment != "production" && cfg.AdminAuthMode == "static" && strings.TrimSpace(cfg.AdminToken) == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return Config{}, fmt.Errorf("config: generate random admin token: %w", err)
		}
		cfg.AdminToken = hex.EncodeToString(b)
		cfg.AdminTokenGenerated = true
	}

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

	if cfg.AdminAuthMode == "oidc" {
		if cfg.OIDCIssuerURL == "" {
			return Config{}, fmt.Errorf("config: %sOIDC_ISSUER_URL is required when admin auth mode is oidc", envPrefix)
		}
		if cfg.OIDCAudience == "" {
			return Config{}, fmt.Errorf("config: %sOIDC_AUDIENCE is required when admin auth mode is oidc", envPrefix)
		}
		if cfg.OIDCAdminRole == "" {
			return Config{}, fmt.Errorf("config: %sOIDC_ADMIN_ROLE must not be empty", envPrefix)
		}
	}

	return cfg, nil
}

// Validate fails clearly when required configuration is missing or
// nonsensical, rather than letting AgentGate start in an undefined state.
func (c Config) Validate() error {
	var errs []string

	if strings.TrimSpace(c.HTTPAddr) == "" {
		errs = append(errs, fmt.Sprintf("%sHTTP_ADDR must not be empty", envPrefix))
	}
	if strings.TrimSpace(c.AuthzGRPCAddr) == "" {
		errs = append(errs, fmt.Sprintf("%sAUTHZ_GRPC_ADDR must not be empty", envPrefix))
	}
	if c.ShutdownTimeout <= 0 {
		errs = append(errs, fmt.Sprintf("%sSHUTDOWN_TIMEOUT must be positive, got %s", envPrefix, c.ShutdownTimeout))
	}

	if c.Environment == "production" {
		if strings.TrimSpace(c.DatabaseURL) == "" {
			errs = append(errs, fmt.Sprintf("%sDATABASE_URL must not be empty in production (DatabaseURL is empty)", envPrefix))
		} else if strings.Contains(c.DatabaseURL, "sslmode=disable") && !c.DBAllowInsecure {
			errs = append(errs, fmt.Sprintf("%sDATABASE_URL contains sslmode=disable without %sDB_ALLOW_INSECURE=true", envPrefix, envPrefix))
		}

		if strings.TrimSpace(c.AuditSalt) == "" || c.AuditSalt == "audit-salt-demo-dev-fixture" {
			errs = append(errs, fmt.Sprintf("%sAUDIT_SALT is empty or equals audit-salt-demo-dev-fixture", envPrefix))
		}

		if c.AdminAuthMode == "static" {
			if strings.TrimSpace(c.AdminToken) != "" {
				errs = append(errs, "AdminToken is non-empty while mode is static (admin auth mode cannot be static in production)")
			} else {
				errs = append(errs, "ADMIN_AUTH_MODE cannot be static in production")
			}
		}

		if strings.TrimSpace(c.CredentialSigningKeyFile) == "" {
			errs = append(errs, fmt.Sprintf("%sCREDENTIAL_SIGNING_KEY_FILE is empty (CredentialSigningKeyFile is empty)", envPrefix))
		}

		if strings.TrimSpace(c.OIDCIssuerURL) == "" || strings.TrimSpace(c.OIDCAudience) == "" {
			errs = append(errs, fmt.Sprintf("OIDC issuer or audience is empty (%sOIDC_ISSUER_URL or %sOIDC_AUDIENCE)", envPrefix, envPrefix))
		}
		if strings.TrimSpace(c.OIDCAdminRole) == "" {
			errs = append(errs, fmt.Sprintf("%sOIDC_ADMIN_ROLE must not be empty", envPrefix))
		}

		if len(c.CredentialAudiences) == 0 {
			errs = append(errs, fmt.Sprintf("%sCREDENTIAL_AUDIENCES is empty (CredentialAudiences is empty)", envPrefix))
		}

		// TLS is not configured (Task 18 fields; add the check in Task 18).
	} else {
		if c.AdminAuthMode == "oidc" {
			if strings.TrimSpace(c.OIDCIssuerURL) == "" {
				errs = append(errs, fmt.Sprintf("%sOIDC_ISSUER_URL is required when admin auth mode is oidc", envPrefix))
			}
			if strings.TrimSpace(c.OIDCAudience) == "" {
				errs = append(errs, fmt.Sprintf("%sOIDC_AUDIENCE is required when admin auth mode is oidc", envPrefix))
			}
			if strings.TrimSpace(c.OIDCAdminRole) == "" {
				errs = append(errs, fmt.Sprintf("%sOIDC_ADMIN_ROLE must not be empty", envPrefix))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("config: %s", strings.Join(errs, "; "))
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
