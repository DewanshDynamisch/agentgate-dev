// Command agentgate is AgentGate's executable entry point.
//
// Wires together typed configuration, structured logging, governance HTTP API,
// durable audit persistence, and the Envoy v3 ext_authz gRPC server for real MCP enforcement.
package main

import (
	"context"
	"crypto/rsa"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Dynamisch-LLC/agentgate/internal/apidocs"
	"github.com/Dynamisch-LLC/agentgate/internal/audit"
	"github.com/Dynamisch-LLC/agentgate/internal/authz"
	"github.com/Dynamisch-LLC/agentgate/internal/config"
	"github.com/Dynamisch-LLC/agentgate/internal/credential"
	"github.com/Dynamisch-LLC/agentgate/internal/fixturepolicy"
	"github.com/Dynamisch-LLC/agentgate/internal/govapi"
	"github.com/Dynamisch-LLC/agentgate/internal/governanceintegration"
	"github.com/Dynamisch-LLC/agentgate/internal/httpserver"
	"github.com/Dynamisch-LLC/agentgate/internal/identity"
	"github.com/Dynamisch-LLC/agentgate/internal/logging"
	"github.com/Dynamisch-LLC/agentgate/internal/metrics"
	"github.com/Dynamisch-LLC/agentgate/internal/oidcverify"
	"github.com/Dynamisch-LLC/agentgate/internal/policymanager"
	"github.com/Dynamisch-LLC/agentgate/internal/policystore"
	"github.com/Dynamisch-LLC/agentgate/internal/ratelimit"
	"github.com/Dynamisch-LLC/agentgate/internal/tlsconfig"
	"github.com/Dynamisch-LLC/agentgate/internal/toolcatalog"
	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

var version = "dev"

var requiredMigrations = []string{
	"001_create_policies",
	"002_create_audit_events",
	"003_add_downstream_credential_ref",
	"004_create_tool_catalog",
}

// MigrationChecker verifies that required database migrations have been applied.
type MigrationChecker interface {
	VerifyMigrations(ctx context.Context, databaseURL string) error
}

// PostgresMigrationChecker checks the schema_migrations table in PostgreSQL.
type PostgresMigrationChecker struct{}

// VerifyMigrations ensures migrations 001–004 exist in schema_migrations.
func (PostgresMigrationChecker) VerifyMigrations(ctx context.Context, databaseURL string) error {
	poolCfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse database url: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	var exists bool
	err = pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables 
			WHERE table_schema = CURRENT_SCHEMA() AND table_name = 'schema_migrations'
		)
	`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("query schema_migrations table existence: %w", err)
	}
	if !exists {
		return errors.New("unmigrated database: schema_migrations table does not exist")
	}

	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return fmt.Errorf("scan migration version: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate migration rows: %w", err)
	}

	var missing []string
	for _, req := range requiredMigrations {
		if !applied[req] {
			missing = append(missing, req)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("unmigrated database: missing migrations %v", missing)
	}
	return nil
}

type commandDeps struct {
	ctx              context.Context
	loadConfig       func() (config.Config, error)
	migrationChecker MigrationChecker
	auditStore       audit.Store
	policyStore      policystore.Store
	toolStore        toolcatalog.Store
	migrateFunc      func(ctx context.Context) error
}

func defaultDeps() commandDeps {
	return commandDeps{
		loadConfig:       config.Load,
		migrationChecker: PostgresMigrationChecker{},
	}
}

func main() {
	os.Exit(runCommand(os.Args[1:], os.Stdout, os.Stderr))
}

func runCommand(args []string, stdout, stderr io.Writer) int {
	return runCommandWithDeps(args, stdout, stderr, defaultDeps())
}

func runCommandWithDeps(args []string, stdout, stderr io.Writer, deps commandDeps) int {
	subcmd := "serve"
	var subcmdArgs []string
	if len(args) > 0 {
		subcmd = args[0]
		subcmdArgs = args[1:]
	}

	ctx := deps.ctx
	if ctx == nil {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
	}

	switch subcmd {
	case "serve":
		return runServe(ctx, subcmdArgs, stdout, stderr, deps)
	case "migrate":
		return runMigrate(ctx, subcmdArgs, stdout, stderr, deps)
	case "verify-audit":
		return runVerifyAudit(ctx, subcmdArgs, stdout, stderr, deps)
	case "version":
		return runVersion(stdout)
	case "healthcheck":
		return runHealthcheck(subcmdArgs, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown subcommand: %s\n", subcmd)
		printUsage(stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: agentgate <command> [arguments]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  serve         Start the AgentGate governance gateway (default)")
	fmt.Fprintln(w, "  migrate       Run database migrations and exit")
	fmt.Fprintln(w, "  verify-audit  Verify cryptographic integrity of audit records for a workspace")
	fmt.Fprintln(w, "  healthcheck   Check gateway health status via /healthz")
	fmt.Fprintln(w, "  version       Print AgentGate version")
}

func runVersion(stdout io.Writer) int {
	fmt.Fprintln(stdout, version)
	return 0
}

func runHealthcheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", "", "HTTP listen address or port (defaults to AGENTGATE_HTTP_ADDR or 8090)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	port := "8090"
	targetAddr := *addr
	if targetAddr == "" {
		targetAddr = os.Getenv("AGENTGATE_HTTP_ADDR")
	}
	if targetAddr != "" {
		if _, p, err := net.SplitHostPort(targetAddr); err == nil && p != "" {
			port = p
		} else if !strings.Contains(targetAddr, ":") {
			port = targetAddr
		} else {
			trimmed := strings.TrimPrefix(targetAddr, ":")
			if trimmed != "" {
				port = trimmed
			}
		}
	}

	url := fmt.Sprintf("http://127.0.0.1:%s/healthz", port)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(stderr, "healthcheck failed: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(stderr, "healthcheck failed with HTTP status %d\n", resp.StatusCode)
		return 1
	}
	fmt.Fprintln(stdout, "OK")
	return 0
}

func runVerifyAudit(ctx context.Context, args []string, stdout, stderr io.Writer, deps commandDeps) int {
	fs := flag.NewFlagSet("verify-audit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var workspaceID string
	fs.StringVar(&workspaceID, "workspace", "", "workspace ID to verify audit records for")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if workspaceID == "" {
		fmt.Fprintln(stderr, "error: --workspace is required")
		return 2
	}

	store := deps.auditStore
	if store == nil {
		cfgLoader := deps.loadConfig
		if cfgLoader == nil {
			cfgLoader = config.Load
		}
		cfg, err := cfgLoader()
		if err != nil {
			fmt.Fprintf(stderr, "load config: %v\n", err)
			return 1
		}
		if cfg.DatabaseURL != "" {
			pgStore, err := audit.NewPostgresStore(ctx, cfg.DatabaseURL)
			if err != nil {
				fmt.Fprintf(stderr, "connect audit store: %v\n", err)
				return 1
			}
			defer pgStore.Close()
			store = pgStore
		} else {
			memStore := audit.NewMemoryStore()
			defer memStore.Close()
			store = memStore
		}
	}

	verifier := audit.NewChainVerifier()
	result, err := verifier.VerifyWorkspace(ctx, store, workspaceID)
	if err != nil {
		fmt.Fprintf(stderr, "verify audit chain: %v\n", err)
		return 1
	}

	if !result.Valid {
		fmt.Fprintf(stdout, "BROKEN at sequence %d: %s\n", result.BrokenSequence, result.Details)
		return 1
	}

	fmt.Fprintf(stdout, "OK %d records\n", result.TotalRecords)
	return 0
}

func runMigrate(ctx context.Context, args []string, stdout, stderr io.Writer, deps commandDeps) int {
	if deps.migrateFunc != nil {
		if err := deps.migrateFunc(ctx); err != nil {
			fmt.Fprintf(stderr, "migration failed: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "migrations applied successfully")
		return 0
	}

	cfgLoader := deps.loadConfig
	if cfgLoader == nil {
		cfgLoader = config.Load
	}
	cfg, err := cfgLoader()
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}

	if cfg.DatabaseURL == "" {
		fmt.Fprintln(stderr, "error: AGENTGATE_DATABASE_URL is required to run migrations")
		return 1
	}

	pgPolicyStore, err := policystore.NewPostgresStore(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(stderr, "connect postgres policy store: %v\n", err)
		return 1
	}
	defer pgPolicyStore.Close()
	if err := pgPolicyStore.Migrate(ctx); err != nil {
		fmt.Fprintf(stderr, "migrate policy store: %v\n", err)
		return 1
	}

	pgAuditStore, err := audit.NewPostgresStore(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(stderr, "connect postgres audit store: %v\n", err)
		return 1
	}
	defer pgAuditStore.Close()
	if err := pgAuditStore.Migrate(ctx); err != nil {
		fmt.Fprintf(stderr, "migrate audit store: %v\n", err)
		return 1
	}

	pgToolStore, err := toolcatalog.NewPostgresStore(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(stderr, "connect postgres tool catalog store: %v\n", err)
		return 1
	}
	defer pgToolStore.Close()
	if err := pgToolStore.Migrate(ctx); err != nil {
		fmt.Fprintf(stderr, "migrate tool catalog store: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, "migrations applied successfully")
	return 0
}

func runServe(ctx context.Context, args []string, stdout, stderr io.Writer, deps commandDeps) int {
	cfgLoader := deps.loadConfig
	if cfgLoader == nil {
		cfgLoader = config.Load
	}
	cfg, err := cfgLoader()
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(stderr, "validate config: %v\n", err)
		return 1
	}

	logger := logging.New(cfg.LogLevel, stdout)
	logger.Info("starting agentgate",
		"version", version,
		"environment", cfg.Environment,
		"http_addr", cfg.HTTPAddr,
		"authz_grpc_addr", cfg.AuthzGRPCAddr,
		"metrics_addr", cfg.MetricsAddr,
		"auto_migrate", cfg.AutoMigrate,
	)

	if cfg.AdminTokenGenerated {
		logger.Warn("generated ephemeral admin token for development; set AGENTGATE_ADMIN_TOKEN for persistent access",
			"admin_token", cfg.AdminToken,
		)
	}
	if cfg.Environment == "production" && cfg.DBAllowInsecure && strings.Contains(cfg.DatabaseURL, "sslmode=disable") {
		logger.Warn("insecure database connection permitted via AGENTGATE_DB_ALLOW_INSECURE=true; not recommended for production")
	}

	srv := httpserver.New(cfg.HTTPAddr, logger)
	if cfg.HTTPTLSCert != "" && cfg.HTTPTLSKey != "" {
		httpTLSCfg, err := tlsconfig.ServerTLS(cfg.HTTPTLSCert, cfg.HTTPTLSKey, "")
		if err != nil {
			fmt.Fprintf(stderr, "configure http tls: %v\n", err)
			return 1
		}
		srv.SetTLSConfig(httpTLSCfg)
		logger.Info("http server configured with TLS")
	}

	var store policystore.Store
	var auditStore audit.Store
	var toolStore toolcatalog.Store
	if cfg.DatabaseURL != "" {
		if !cfg.AutoMigrate {
			checker := deps.migrationChecker
			if checker == nil {
				checker = PostgresMigrationChecker{}
			}
			if err := checker.VerifyMigrations(ctx, cfg.DatabaseURL); err != nil {
				fmt.Fprintf(stderr, "unmigrated database: %v\n", err)
				return 1
			}
		}

		pgStore, err := policystore.NewPostgresStore(ctx, cfg.DatabaseURL)
		if err != nil {
			fmt.Fprintf(stderr, "connect postgres policy store: %v\n", err)
			return 1
		}
		defer pgStore.Close()

		if cfg.AutoMigrate {
			if err := pgStore.Migrate(ctx); err != nil {
				fmt.Fprintf(stderr, "migrate postgres policy store: %v\n", err)
				return 1
			}
		}
		store = pgStore

		pgAuditStore, err := audit.NewPostgresStore(ctx, cfg.DatabaseURL)
		if err != nil {
			fmt.Fprintf(stderr, "connect postgres audit store: %v\n", err)
			return 1
		}
		defer pgAuditStore.Close()

		if cfg.AutoMigrate {
			if err := pgAuditStore.Migrate(ctx); err != nil {
				fmt.Fprintf(stderr, "migrate postgres audit store: %v\n", err)
				return 1
			}
		}
		auditStore = pgAuditStore

		pgToolStore, err := toolcatalog.NewPostgresStore(ctx, cfg.DatabaseURL)
		if err != nil {
			fmt.Fprintf(stderr, "connect postgres tool catalog store: %v\n", err)
			return 1
		}
		defer pgToolStore.Close()
		toolStore = pgToolStore

		srv.SetReadinessCheck(pgStore.Ping)
		logger.Info("connected to persistent postgres policy and audit store")
	} else {
		store = policystore.NewMemoryStore()
		defer store.Close()
		auditStore = audit.NewMemoryStore()
		defer auditStore.Close()
		toolStore = toolcatalog.NewMemoryStore()
		logger.Info("using in-memory policy and audit store")
	}

	metricsBundle := metrics.NewMetrics(version)

	redactor := audit.NewRedactor(cfg.AuditSalt, nil)
	auditedDecisionSvc := audit.NewAuditedDecisionService(nil, auditStore, redactor)
	auditedDecisionSvc.SetMetrics(metricsBundle.AuditAppendFailuresTotal)

	policyMgr := policymanager.NewWithListener(store, auditedDecisionSvc)
	govIntegration := governanceintegration.NewGovernanceDecisionService(policyMgr)
	auditedDecisionSvc.SetEvaluator(govIntegration)

	// Seed default workspace baseline policy if none active
	if _, err := policyMgr.GetActiveEngine("default"); err != nil {
		if _, err := policyMgr.CreateCandidateWithVersion(ctx, "default", "v1.0.0", fixturepolicy.CedarSource, "default baseline policy"); err == nil {
			_, _ = policyMgr.Activate(ctx, "default", "v1.0.0")
			logger.Info("seeded active baseline policy v1.0.0 for workspace 'default'")
		}
	}

	toolCatalog, err := loadToolCatalog(ctx, cfg, toolStore, logger)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	// Build ext_authz adapter components
	identityMapper, err := identity.NewMapper(identity.MapperConfig{
		AgentIDClaim:    "sub",
		RolesClaim:      "roles",
		OnBehalfOfClaim: "obo",
	})
	if err != nil {
		fmt.Fprintf(stderr, "init identity mapper: %v\n", err)
		return 1
	}

	govHandler := govapi.NewHandlerWithCatalog(policyMgr, cfg.AdminToken, govIntegration, auditStore, govapi.ToolCatalogService{
		Store:    toolStore,
		Catalog:  toolCatalog,
		Listener: auditedDecisionSvc,
	})
	adminAuth, err := buildAdminAuthenticator(ctx, cfg, logger)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	govHandler.SetAuthenticator(adminAuth)
	govHandler.SetRateLimiter(ratelimit.New(cfg.AdminRateLimit, cfg.AdminRateBurst))
	govHandler.SetMetrics(metricsBundle.AdminRequestsTotal)
	govHandler.RegisterRoutes(srv.Mux())
	apidocs.RegisterRoutes(srv.Mux())
	logger.Info("governance API docs available", "path", "/docs")

	authzAdapter := authz.NewAdapter(authz.AdapterConfig{
		DefaultWorkspaceID:   "default",
		AllowStaticWorkspace: true,
		DefaultBackendID:     "mcp-probe",
		IdentityMapper:       identityMapper,
		Catalog:              toolCatalog,
	})

	signingKey, err := loadSigningKey(cfg, logger)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	audiences := cfg.CredentialAudiences
	issuer, err := credential.NewJWTIssuer(credential.JWTIssuerConfig{
		Issuer:      cfg.CredentialIssuer,
		KeyID:       cfg.CredentialKeyID,
		PrivateKey:  signingKey,
		Lifetime:    cfg.CredentialLifetime,
		AudienceFor: func(backendID string) string { return audiences[backendID] },
	})
	if err != nil {
		fmt.Fprintf(stderr, "init credential issuer: %v\n", err)
		return 1
	}
	auditedDecisionSvc.SetIssuer(issuer)
	srv.Mux().Handle("GET /.well-known/jwks.json", credential.JWKSHandler(issuer.JWKS()))

	authzServer := authz.NewServerWithOptions(authzAdapter, auditedDecisionSvc, authz.ServerOptions{
		RequireDownstreamCredential: cfg.RequireDownstreamCredential,
		DecisionsTotal:              metricsBundle.DecisionsTotal,
		DecisionDurationSeconds:     metricsBundle.DecisionDurationSeconds,
		CredentialIssuanceFailures:  metricsBundle.CredentialIssuanceFailures,
	})

	// Start ext_authz gRPC Server
	grpcListener, err := net.Listen("tcp", cfg.AuthzGRPCAddr)
	if err != nil {
		fmt.Fprintf(stderr, "listen authz grpc: %v\n", err)
		return 1
	}
	defer grpcListener.Close()

	grpcOpts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(2 * 1024 * 1024),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.ConnectionTimeout(5 * time.Second),
	}
	if cfg.AuthzTLSCert != "" && cfg.AuthzTLSKey != "" {
		tlsCfg, err := tlsconfig.ServerTLS(cfg.AuthzTLSCert, cfg.AuthzTLSKey, cfg.AuthzTLSClientCA)
		if err != nil {
			fmt.Fprintf(stderr, "configure authz grpc tls: %v\n", err)
			return 1
		}
		grpcOpts = append(grpcOpts, grpc.Creds(credentials.NewTLS(tlsCfg)))
		logger.Info("authz grpc server configured with TLS", "client_mtls", cfg.AuthzTLSClientCA != "")
	}

	grpcServer := grpc.NewServer(grpcOpts...)
	authv3.RegisterAuthorizationServer(grpcServer, authzServer)

	go func() {
		if err := grpcServer.Serve(grpcListener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			logger.Error("authz grpc server stopped with error", "err", err)
		}
	}()
	logger.Info("authz grpc server listening", "addr", grpcListener.Addr().String())

	// Start Metrics HTTP Server on internal address
	metricsMux := http.NewServeMux()
	metricsMux.Handle("GET /metrics", metricsBundle.Registry.Handler())
	metricsServer := &http.Server{
		Addr:              cfg.MetricsAddr,
		Handler:           metricsMux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	metricsListener, err := net.Listen("tcp", cfg.MetricsAddr)
	if err != nil {
		fmt.Fprintf(stderr, "listen metrics http: %v\n", err)
		return 1
	}
	defer metricsListener.Close()

	go func() {
		if err := metricsServer.Serve(metricsListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("metrics http server stopped with error", "err", err)
		}
	}()
	logger.Info("metrics http server listening", "addr", metricsListener.Addr().String())

	// Start HTTP Server
	if err := srv.Start(); err != nil {
		fmt.Fprintf(stderr, "start http server: %v\n", err)
		return 1
	}
	logger.Info("http server listening", "addr", srv.Addr())

	<-ctx.Done()

	logger.Info("shutdown signal received, shutting down")

	grpcServer.GracefulStop()
	logger.Info("authz grpc server stopped cleanly")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown metrics http server error", "err", err)
	}

	if err := srv.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(stderr, "shutdown http server: %v\n", err)
		return 1
	}

	logger.Info("agentgate stopped cleanly")
	return 0
}

func loadToolCatalog(ctx context.Context, cfg config.Config, toolStore toolcatalog.Store, logger *slog.Logger) (*toolcatalog.Catalog, error) {
	if cfg.AutoMigrate {
		if err := toolStore.Migrate(ctx); err != nil {
			return nil, fmt.Errorf("migrate tool catalog store: %w", err)
		}
	}
	if cfg.ToolBootstrapFile != "" {
		defs, err := toolcatalog.LoadBootstrapFile(cfg.ToolBootstrapFile)
		if err != nil {
			return nil, fmt.Errorf("load tool bootstrap file: %w", err)
		}
		seeded, err := toolcatalog.SeedIfEmpty(ctx, toolStore, defs, "bootstrap")
		if err != nil {
			return nil, fmt.Errorf("seed tool catalog: %w", err)
		}
		logger.Info("tool catalog bootstrap processed", "file", cfg.ToolBootstrapFile, "seeded", seeded)
	}
	catalog, err := toolcatalog.NewCatalog(nil)
	if err != nil {
		return nil, fmt.Errorf("init tool catalog: %w", err)
	}
	if err := catalog.Reload(ctx, toolStore); err != nil {
		return nil, fmt.Errorf("load tool catalog: %w", err)
	}
	if len(catalog.Current().Registry.List()) == 0 {
		logger.Warn("tool catalog is empty; all tool calls will be denied as unknown_tool")
	}
	return catalog, nil
}

func loadSigningKey(cfg config.Config, logger *slog.Logger) (*rsa.PrivateKey, error) {
	if cfg.CredentialSigningKeyFile != "" {
		key, err := credential.LoadRSAPrivateKeyPEM(cfg.CredentialSigningKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load credential signing key: %w", err)
		}
		return key, nil
	}
	if cfg.Environment == "production" {
		return nil, errors.New("AGENTGATE_CREDENTIAL_SIGNING_KEY_FILE is required when AGENTGATE_ENV=production")
	}
	logger.Warn("no credential signing key file configured; using ephemeral key (tokens invalid after restart)")
	key, err := credential.GenerateEphemeralKey()
	if err != nil {
		return nil, fmt.Errorf("generate ephemeral credential key: %w", err)
	}
	return key, nil
}

// buildAdminAuthenticator selects the admin API authenticator. In oidc mode the
// verifier is built eagerly so a failed IdP discovery stops startup.
func buildAdminAuthenticator(ctx context.Context, cfg config.Config, logger *slog.Logger) (govapi.AdminAuthenticator, error) {
	if cfg.AdminAuthMode != "oidc" {
		logger.Warn("admin API uses the static shared admin token; not permitted in production", "environment", cfg.Environment)
		return govapi.NewStaticTokenAuthenticator(cfg.AdminToken), nil
	}
	vcfg := oidcverify.Config{
		IssuerURL:     cfg.OIDCIssuerURL,
		Audience:      cfg.OIDCAudience,
		RoleClaimPath: cfg.OIDCRoleClaimPath,
	}
	if cfg.Environment == "development" && strings.HasPrefix(strings.ToLower(cfg.OIDCIssuerURL), "http://") {
		vcfg.AllowInsecureHTTP = true
		logger.Warn("OIDC issuer uses plain http; allowed in development only", "issuer", cfg.OIDCIssuerURL)
	}
	v, err := oidcverify.New(ctx, vcfg)
	if err != nil {
		return nil, fmt.Errorf("init OIDC admin verifier: %w", err)
	}
	logger.Info("admin API authentication: OIDC", "issuer", cfg.OIDCIssuerURL, "role", cfg.OIDCAdminRole)
	return govapi.NewOIDCAuthenticator(v, cfg.OIDCAdminRole), nil
}
