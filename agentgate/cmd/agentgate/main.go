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
	"syscall"

	"github.com/Dynamisch-LLC/agentgate/internal/apidocs"
	"github.com/Dynamisch-LLC/agentgate/internal/argdecl"
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
	"github.com/Dynamisch-LLC/agentgate/internal/policymanager"
	"github.com/Dynamisch-LLC/agentgate/internal/policystore"
	"github.com/Dynamisch-LLC/agentgate/internal/toolregistry"
	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	"google.golang.org/grpc"
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

	logger := logging.New(cfg.LogLevel, os.Stdout)
	logger.Info("starting agentgate",
		"environment", cfg.Environment,
		"http_addr", cfg.HTTPAddr,
		"authz_grpc_addr", cfg.AuthzGRPCAddr,
	)

	srv := httpserver.New(cfg.HTTPAddr, logger)

	var store policystore.Store
	var auditStore audit.Store
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

		srv.SetReadinessCheck(pgStore.Ping)
		logger.Info("connected to persistent postgres policy and audit store")
	} else {
		store = policystore.NewMemoryStore()
		defer store.Close()
		auditStore = audit.NewMemoryStore()
		defer auditStore.Close()
		logger.Info("using in-memory policy and audit store")
	}

	redactor := audit.NewRedactor(os.Getenv("AGENTGATE_AUDIT_SALT"), nil)
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

	// Build ext_authz adapter components
	identityMapper, err := identity.NewMapper(identity.MapperConfig{
		AgentIDClaim:    "sub",
		RolesClaim:      "roles",
		OnBehalfOfClaim: "obo",
	})
	if err != nil {
		return fmt.Errorf("init identity mapper: %w", err)
	}

	readStatusSchema := []byte(`{"type":"object","properties":{"verbose":{"type":"boolean"}}}`)
	readStatusFP, _ := toolregistry.FingerprintSchema(readStatusSchema)

	writeStatusSchema := []byte(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}}}`)
	writeStatusFP, _ := toolregistry.FingerprintSchema(writeStatusSchema)

	// G7 Task B demo tools (deploy/demo/support-desk-mcp) — additive, alongside the
	// existing G6 probe tools above, not a replacement. Registered under the same
	// "mcp-probe"/"default" backend_id namespace: BackendID here is a governance
	// namespace key, not a physical binding to any one backend process, and none
	// of these tool names collide with the existing read_status/write_status/
	// admin_action entries. This is a necessary consequence of a pre-existing gap,
	// not a new one: internal/toolregistry.Registry is still a hardcoded,
	// startup-loaded list (no persistent store) — already tracked as Backend work
	// blocking G9's multi-backend topology in docs/PHASES/PROGRESS_AND_ROADMAP.md §3.
	listTicketsSchema := []byte(`{"type":"object","properties":{"status_filter":{"type":"string"}}}`)
	listTicketsFP, _ := toolregistry.FingerprintSchema(listTicketsSchema)

	updateTicketSchema := []byte(`{"type":"object","properties":{"ticket_id":{"type":"string"},"new_status":{"type":"string"}}}`)
	updateTicketFP, _ := toolregistry.FingerprintSchema(updateTicketSchema)

	deleteTicketSchema := []byte(`{"type":"object","properties":{"ticket_id":{"type":"string"}}}`)
	deleteTicketFP, _ := toolregistry.FingerprintSchema(deleteTicketSchema)

	toolReg, err := toolregistry.NewRegistry([]toolregistry.RegistryEntry{
		{
			ToolID:                toolregistry.ToolID{BackendID: "mcp-probe", ToolName: "read_status"},
			Risk:                  toolregistry.RiskRead,
			RegisteredFingerprint: readStatusFP,
		},
		{
			ToolID:                toolregistry.ToolID{BackendID: "default", ToolName: "read_status"},
			Risk:                  toolregistry.RiskRead,
			RegisteredFingerprint: readStatusFP,
		},
		{
			ToolID:                toolregistry.ToolID{BackendID: "mcp-probe", ToolName: "write_status"},
			Risk:                  toolregistry.RiskWrite,
			RegisteredFingerprint: writeStatusFP,
		},
		{
			ToolID:                toolregistry.ToolID{BackendID: "default", ToolName: "write_status"},
			Risk:                  toolregistry.RiskWrite,
			RegisteredFingerprint: writeStatusFP,
		},
		{
			ToolID:                toolregistry.ToolID{BackendID: "mcp-probe", ToolName: "admin_action"},
			Risk:                  toolregistry.RiskDestructive,
			RegisteredFingerprint: readStatusFP,
		},
		{
			ToolID:                toolregistry.ToolID{BackendID: "default", ToolName: "admin_action"},
			Risk:                  toolregistry.RiskDestructive,
			RegisteredFingerprint: readStatusFP,
		},
		{
			ToolID:                toolregistry.ToolID{BackendID: "mcp-probe", ToolName: "list_tickets"},
			Risk:                  toolregistry.RiskRead,
			RegisteredFingerprint: listTicketsFP,
		},
		{
			ToolID:                toolregistry.ToolID{BackendID: "default", ToolName: "list_tickets"},
			Risk:                  toolregistry.RiskRead,
			RegisteredFingerprint: listTicketsFP,
		},
		{
			ToolID:                toolregistry.ToolID{BackendID: "mcp-probe", ToolName: "update_ticket_status"},
			Risk:                  toolregistry.RiskWrite,
			RegisteredFingerprint: updateTicketFP,
		},
		{
			ToolID:                toolregistry.ToolID{BackendID: "default", ToolName: "update_ticket_status"},
			Risk:                  toolregistry.RiskWrite,
			RegisteredFingerprint: updateTicketFP,
		},
		{
			ToolID:                toolregistry.ToolID{BackendID: "mcp-probe", ToolName: "delete_ticket"},
			Risk:                  toolregistry.RiskDestructive,
			RegisteredFingerprint: deleteTicketFP,
		},
		{
			ToolID:                toolregistry.ToolID{BackendID: "default", ToolName: "delete_ticket"},
			Risk:                  toolregistry.RiskDestructive,
			RegisteredFingerprint: deleteTicketFP,
		},
	})
	if err != nil {
		return fmt.Errorf("init tool registry: %w", err)
	}

	govHandler := govapi.NewHandler(policyMgr, cfg.AdminToken, govIntegration, auditStore, toolReg)
	govHandler.RegisterRoutes(srv.Mux())
	apidocs.RegisterRoutes(srv.Mux())
	logger.Info("governance API docs available", "path", "/docs")

	readStatusDecls, _ := argdecl.NewDeclarationSet([]argdecl.Declaration{
		{
			Name:     "verbose",
			Type:     argdecl.ArgTypeBool,
			Required: false,
		},
	})
	writeStatusDecls, _ := argdecl.NewDeclarationSet([]argdecl.Declaration{
		{
			Name:     "amount",
			Type:     argdecl.ArgTypeInt,
			Required: false,
		},
	})
	listTicketsDecls, _ := argdecl.NewDeclarationSet([]argdecl.Declaration{
		{Name: "status_filter", Type: argdecl.ArgTypeString, Required: false},
	})
	updateTicketDecls, _ := argdecl.NewDeclarationSet([]argdecl.Declaration{
		{Name: "ticket_id", Type: argdecl.ArgTypeString, Required: true},
		{Name: "new_status", Type: argdecl.ArgTypeString, Required: true},
	})
	deleteTicketDecls, _ := argdecl.NewDeclarationSet([]argdecl.Declaration{
		{Name: "ticket_id", Type: argdecl.ArgTypeString, Required: true},
	})

	// In G6 single-workspace integration deployment, AllowStaticWorkspace permits fallback to
	// "default" when JWT does not declare a workspace_id claim. In multi-tenant environments,
	// setting this to false enforces fail-closed rejection for requests lacking trusted workspace claims.
	authzAdapter := authz.NewAdapter(authz.AdapterConfig{
		DefaultWorkspaceID:   "default",
		AllowStaticWorkspace: true,
		DefaultBackendID:     "mcp-probe",
		IdentityMapper:       identityMapper,
		ToolRegistry:         toolReg,
		ArgDeclarations: map[string]*argdecl.DeclarationSet{
			"read_status":          readStatusDecls,
			"write_status":         writeStatusDecls,
			"list_tickets":         listTicketsDecls,
			"update_ticket_status": updateTicketDecls,
			"delete_ticket":        deleteTicketDecls,
		},
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

	grpcServer := grpc.NewServer()
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
