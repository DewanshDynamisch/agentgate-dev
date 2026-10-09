package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dynamisch-LLC/agentgate/internal/audit"
	"github.com/Dynamisch-LLC/agentgate/internal/config"
)

func TestLoadSigningKeyProductionWithoutFileFails(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	t.Setenv("AGENTGATE_OIDC_ISSUER_URL", "https://idp.example")
	t.Setenv("AGENTGATE_OIDC_AUDIENCE", "agentgate")
	for _, env := range []string{"production", "Production", " PRODUCTION ", "prod"} {
		t.Setenv("AGENTGATE_ENV", env)
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := loadSigningKey(cfg, logger); err == nil {
			t.Errorf("env %q: want error without key file", env)
		}
	}
}

func TestLoadSigningKeyDevelopmentEphemeral(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	t.Setenv("AGENTGATE_ENV", "development")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if key, err := loadSigningKey(cfg, logger); err != nil || key == nil {
		t.Fatalf("key=%v err=%v", key, err)
	}
}

func TestVersionDefault(t *testing.T) {
	if version != "dev" {
		t.Errorf("version = %q, want dev", version)
	}
}

func TestRunCommandUnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCommand([]string{"nonexistent-subcommand"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("runCommand unknown = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Errorf("stderr = %q, want it to contain usage", stderr.String())
	}
}

func TestRunCommandVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCommand([]string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("runCommand version = %d, want 0", code)
	}
	if strings.TrimSpace(stdout.String()) != version {
		t.Errorf("stdout = %q, want %q", strings.TrimSpace(stdout.String()), version)
	}
}

func TestRunCommandVerifyAuditValidChain(t *testing.T) {
	ctx := context.Background()
	store := audit.NewMemoryStore()
	defer store.Close()

	prevHash := audit.GenesisHash

	rec1 := audit.DecisionRecord{
		WorkspaceID:       "ws-test",
		ExecutionID:       "exec-1",
		Timestamp:         time.Now().UTC(),
		EventType:         audit.EventTypeDecision,
		Decision:          "permit",
		Reason:            "allowed",
		PrincipalAgentID:  "agent-1",
		ToolBackendID:     "backend-1",
		ToolName:          "tool-1",
		RedactedArguments: map[string]string{},
		PrevHash:          prevHash,
	}
	canon1, err := audit.ComputeCanonicalPayload(rec1)
	if err != nil {
		t.Fatalf("ComputeCanonicalPayload rec1: %v", err)
	}
	rec1.CanonicalPayload = string(canon1)
	rec1.RowHash = audit.ComputeRowHash(prevHash, canon1)
	prevHash = rec1.RowHash
	if _, err := store.AppendDecision(ctx, rec1); err != nil {
		t.Fatalf("AppendDecision rec1: %v", err)
	}

	rec2 := audit.DecisionRecord{
		WorkspaceID:       "ws-test",
		ExecutionID:       "exec-2",
		Timestamp:         time.Now().UTC(),
		EventType:         audit.EventTypeDecision,
		Decision:          "permit",
		Reason:            "allowed",
		PrincipalAgentID:  "agent-1",
		ToolBackendID:     "backend-1",
		ToolName:          "tool-2",
		RedactedArguments: map[string]string{},
		PrevHash:          prevHash,
	}
	canon2, err := audit.ComputeCanonicalPayload(rec2)
	if err != nil {
		t.Fatalf("ComputeCanonicalPayload rec2: %v", err)
	}
	rec2.CanonicalPayload = string(canon2)
	rec2.RowHash = audit.ComputeRowHash(prevHash, canon2)
	if _, err := store.AppendDecision(ctx, rec2); err != nil {
		t.Fatalf("AppendDecision rec2: %v", err)
	}

	var stdout, stderr bytes.Buffer
	deps := commandDeps{
		auditStore: store,
	}
	code := runCommandWithDeps([]string{"verify-audit", "--workspace", "ws-test"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("runCommand verify-audit code = %d, want 0; stdout = %s; stderr = %s", code, stdout.String(), stderr.String())
	}
	expectedOut := "OK 2 records\n"
	if stdout.String() != expectedOut {
		t.Errorf("stdout = %q, want %q", stdout.String(), expectedOut)
	}
}

type tamperedStore struct {
	audit.Store
}

func (s *tamperedStore) ListRecords(ctx context.Context, workspaceID string, limit int) ([]audit.StoredRecord, error) {
	recs, err := s.Store.ListRecords(ctx, workspaceID, limit)
	if err != nil || len(recs) == 0 {
		return recs, err
	}
	// Tamper with record
	tampered := make([]audit.StoredRecord, len(recs))
	copy(tampered, recs)
	tampered[0].CanonicalPayload = `{"tampered":true}`
	return tampered, nil
}

func TestRunCommandVerifyAuditTamperedChain(t *testing.T) {
	ctx := context.Background()
	baseStore := audit.NewMemoryStore()
	defer baseStore.Close()

	prevHash := audit.GenesisHash
	rec := audit.DecisionRecord{
		WorkspaceID:       "ws-test",
		ExecutionID:       "exec-1",
		Timestamp:         time.Now().UTC(),
		EventType:         audit.EventTypeDecision,
		Decision:          "permit",
		Reason:            "allowed",
		PrincipalAgentID:  "agent-1",
		ToolBackendID:     "backend-1",
		ToolName:          "tool-1",
		RedactedArguments: map[string]string{},
		PrevHash:          prevHash,
	}
	canon, err := audit.ComputeCanonicalPayload(rec)
	if err != nil {
		t.Fatalf("ComputeCanonicalPayload: %v", err)
	}
	rec.CanonicalPayload = string(canon)
	rec.RowHash = audit.ComputeRowHash(prevHash, canon)
	if _, err := baseStore.AppendDecision(ctx, rec); err != nil {
		t.Fatalf("AppendDecision: %v", err)
	}

	store := &tamperedStore{Store: baseStore}

	var stdout, stderr bytes.Buffer
	deps := commandDeps{
		auditStore: store,
	}
	code := runCommandWithDeps([]string{"verify-audit", "--workspace", "ws-test"}, &stdout, &stderr, deps)
	if code != 1 {
		t.Fatalf("runCommand verify-audit code = %d, want 1; stdout = %s", code, stdout.String())
	}
	if !strings.HasPrefix(stdout.String(), "BROKEN at sequence 1:") {
		t.Errorf("stdout = %q, want prefix 'BROKEN at sequence 1:'", stdout.String())
	}
}

type fakeMigrationChecker struct {
	err error
}

func (f *fakeMigrationChecker) VerifyMigrations(ctx context.Context, dbURL string) error {
	return f.err
}

func TestRunCommandServeAutoMigrateFalseOnUnmigratedPostgres(t *testing.T) {
	var stdout, stderr bytes.Buffer
	checker := &fakeMigrationChecker{
		err: errors.New("missing migrations [001_create_policies]"),
	}
	deps := commandDeps{
		migrationChecker: checker,
		loadConfig: func() (config.Config, error) {
			cfg, err := config.Load()
			if err != nil {
				return config.Config{}, err
			}
			cfg.AutoMigrate = false
			cfg.DatabaseURL = "postgres://fake-host:5432/fake_db"
			return cfg, nil
		},
	}

	code := runCommandWithDeps([]string{"serve"}, &stdout, &stderr, deps)
	if code != 1 {
		t.Fatalf("runCommand serve code = %d, want 1; stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unmigrated database") && !strings.Contains(stderr.String(), "missing migrations") {
		t.Errorf("stderr = %q, want unmigrated database error", stderr.String())
	}
}

func TestRunCommandServeAutoMigrateFalseMemoryStore(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-canceled context so serve terminates gracefully

	deps := commandDeps{
		ctx: ctx,
		loadConfig: func() (config.Config, error) {
			cfg, err := config.Load()
			if err != nil {
				return config.Config{}, err
			}
			cfg.AutoMigrate = false
			cfg.DatabaseURL = ""
			// Use random available ports
			cfg.HTTPAddr = "127.0.0.1:0"
			cfg.AuthzGRPCAddr = "127.0.0.1:0"
			cfg.MetricsAddr = "127.0.0.1:0"
			return cfg, nil
		},
	}

	code := runCommandWithDeps([]string{"serve"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("runCommand serve code = %d, want 0; stderr = %s", code, stderr.String())
	}
}

func TestRunCommandHealthcheck(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := runCommand([]string{"healthcheck", "-addr", srv.Listener.Addr().String()}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runCommand healthcheck code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "OK") {
		t.Errorf("stdout = %q, want OK", stdout.String())
	}

	// Now check failure against non-listening address
	stdout.Reset()
	stderr.Reset()
	srv.Close()
	failCode := runCommand([]string{"healthcheck", "-addr", srv.Listener.Addr().String()}, &stdout, &stderr)
	if failCode != 1 {
		t.Fatalf("runCommand healthcheck closed srv code = %d, want 1", failCode)
	}
}

func TestRunCommandMigrate(t *testing.T) {
	var stdout, stderr bytes.Buffer
	called := false
	deps := commandDeps{
		migrateFunc: func(ctx context.Context) error {
			called = true
			return nil
		},
	}
	code := runCommandWithDeps([]string{"migrate"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("runCommand migrate code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if !called {
		t.Error("migrateFunc was not called")
	}
	if !strings.Contains(stdout.String(), "success") {
		t.Errorf("stdout = %q, want success message", stdout.String())
	}
}
