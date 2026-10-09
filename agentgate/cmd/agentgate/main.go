// Command agentgate is AgentGate's executable entry point.
//
// Wires together typed configuration, structured logging, governance HTTP API,
// durable audit persistence, and the Envoy v3 ext_authz gRPC server for real MCP enforcement.
package main

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"log/slog"
	"net"
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
	"github.com/Dynamisch-LLC/agentgate/internal/oidcverify"
	"github.com/Dynamisch-LLC/agentgate/internal/policymanager"
	"github.com/Dynamisch-LLC/agentgate/internal/policystore"
	"github.com/Dynamisch-LLC/agentgate/internal/ratelimit"
	"github.com/Dynamisch-LLC/agentgate/internal/tlsconfig"
	"github.com/Dynamisch-LLC/agentgate/internal/toolcatalog"
	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "agentgate: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	logger := logging.New(cfg.LogLevel, os.Stdout)
	logger.Info("starting agentgate",
		"environment", cfg.Environment,
		"http_addr", cfg.HTTPAddr,
		"authz_grpc_addr", cfg.AuthzGRPCAddr,
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
			return fmt.Errorf("configure http tls: %w", err)
		}
		srv.SetTLSConfig(httpTLSCfg)
		logger.Info("http server configured with TLS")
	}

	var store policystore.Store
	var auditStore audit.Store
	var toolStore toolcatalog.Store
	if cfg.DatabaseURL != "" {
		pgStore, err := policystore.NewPostgresStore(context.Background(), cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("connect postgres policy store: %w", err)
		}
		defer pgStore.Close()

		if err := pgStore.Migrate(context.Background()); err != nil {
			return fmt.Errorf("migrate postgres policy store: %w", err)
		}
		store = pgStore

		pgAuditStore, err := audit.NewPostgresStore(context.Background(), cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("connect postgres audit store: %w", err)
		}
		defer pgAuditStore.Close()

		if err := pgAuditStore.Migrate(context.Background()); err != nil {
			return fmt.Errorf("migrate postgres audit store: %w", err)
		}
		auditStore = pgAuditStore

		pgToolStore, err := toolcatalog.NewPostgresStore(context.Background(), cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("connect postgres tool catalog store: %w", err)
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

	redactor := audit.NewRedactor(cfg.AuditSalt, nil)
	auditedDecisionSvc := audit.NewAuditedDecisionService(nil, auditStore, redactor)

	policyMgr := policymanager.NewWithListener(store, auditedDecisionSvc)
	govIntegration := governanceintegration.NewGovernanceDecisionService(policyMgr)
	auditedDecisionSvc.SetEvaluator(govIntegration)

	// Seed default workspace baseline policy if none active
	ctxInit := context.Background()
	if _, err := policyMgr.GetActiveEngine("default"); err != nil {
		if _, err := policyMgr.CreateCandidateWithVersion(ctxInit, "default", "v1.0.0", fixturepolicy.CedarSource, "default baseline policy"); err == nil {
			_, _ = policyMgr.Activate(ctxInit, "default", "v1.0.0")
			logger.Info("seeded active baseline policy v1.0.0 for workspace 'default'")
		}
	}

	toolCatalog, err := loadToolCatalog(context.Background(), cfg, toolStore, logger)
	if err != nil {
		return err
	}

	// Build ext_authz adapter components
	identityMapper, err := identity.NewMapper(identity.MapperConfig{
		AgentIDClaim:    "sub",
		RolesClaim:      "roles",
		OnBehalfOfClaim: "obo",
	})
	if err != nil {
		return fmt.Errorf("init identity mapper: %w", err)
	}

	govHandler := govapi.NewHandlerWithCatalog(policyMgr, cfg.AdminToken, govIntegration, auditStore, govapi.ToolCatalogService{
		Store:    toolStore,
		Catalog:  toolCatalog,
		Listener: auditedDecisionSvc,
	})
	adminAuth, err := buildAdminAuthenticator(context.Background(), cfg, logger)
	if err != nil {
		return err
	}
	govHandler.SetAuthenticator(adminAuth)
	govHandler.SetRateLimiter(ratelimit.New(cfg.AdminRateLimit, cfg.AdminRateBurst))
	govHandler.RegisterRoutes(srv.Mux())
	apidocs.RegisterRoutes(srv.Mux())
	logger.Info("governance API docs available", "path", "/docs")

	// In G6 single-workspace integration deployment, AllowStaticWorkspace permits fallback to
	// "default" when JWT does not declare a workspace_id claim. In multi-tenant environments,
	// setting this to false enforces fail-closed rejection for requests lacking trusted workspace claims.
	authzAdapter := authz.NewAdapter(authz.AdapterConfig{
		DefaultWorkspaceID:   "default",
		AllowStaticWorkspace: true,
		DefaultBackendID:     "mcp-probe",
		IdentityMapper:       identityMapper,
		Catalog:              toolCatalog,
	})

	signingKey, err := loadSigningKey(cfg, logger)
	if err != nil {
		return err
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
		return fmt.Errorf("init credential issuer: %w", err)
	}
	auditedDecisionSvc.SetIssuer(issuer)
	srv.Mux().Handle("GET /.well-known/jwks.json", credential.JWKSHandler(issuer.JWKS()))

	authzServer := authz.NewServerWithOptions(authzAdapter, auditedDecisionSvc, authz.ServerOptions{
		RequireDownstreamCredential: cfg.RequireDownstreamCredential,
	})

	// Start ext_authz gRPC Server
	grpcListener, err := net.Listen("tcp", cfg.AuthzGRPCAddr)
	if err != nil {
		return fmt.Errorf("listen authz grpc: %w", err)
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
			return fmt.Errorf("configure authz grpc tls: %w", err)
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
	logger.Info("authz grpc server listening", "addr", cfg.AuthzGRPCAddr)

	// Start HTTP Server
	if err := srv.Start(); err != nil {
		return fmt.Errorf("start http server: %w", err)
	}
	logger.Info("http server listening", "addr", srv.Addr())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	logger.Info("shutdown signal received, shutting down")

	grpcServer.GracefulStop()
	logger.Info("authz grpc server stopped cleanly")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}

	logger.Info("agentgate stopped cleanly")
	return nil
}

func loadToolCatalog(ctx context.Context, cfg config.Config, toolStore toolcatalog.Store, logger *slog.Logger) (*toolcatalog.Catalog, error) {
	if err := toolStore.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("migrate tool catalog store: %w", err)
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
