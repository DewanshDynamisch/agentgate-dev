package g5audit_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Dynamisch-LLC/agentgate/internal/audit"
	"github.com/Dynamisch-LLC/agentgate/internal/decision"
	"github.com/Dynamisch-LLC/agentgate/internal/fixturepolicy"
	"github.com/Dynamisch-LLC/agentgate/internal/governanceintegration"
	"github.com/Dynamisch-LLC/agentgate/internal/policymanager"
	"github.com/Dynamisch-LLC/agentgate/internal/policystore"
)

type qaEnvironment struct {
	pStore   policystore.Store
	pMgr     *policymanager.Manager
	aStore   audit.Store
	redactor *audit.Redactor
	auditSvc *audit.AuditedDecisionService
	govSvc   *governanceintegration.GovernanceDecisionService
	verifier *audit.ChainVerifier
}

func setupQAEnv(t *testing.T, salt string) *qaEnvironment {
	t.Helper()
	pStore := policystore.NewMemoryStore()
	aStore := audit.NewMemoryStore()
	redactor := audit.NewRedactor(salt, nil)

	auditSvc := audit.NewAuditedDecisionService(nil, aStore, redactor)
	pMgr := policymanager.NewWithListener(pStore, auditSvc)
	govSvc := governanceintegration.NewGovernanceDecisionService(pMgr)
	auditSvc.SetEvaluator(govSvc)

	return &qaEnvironment{
		pStore:   pStore,
		pMgr:     pMgr,
		aStore:   aStore,
		redactor: redactor,
		auditSvc: auditSvc,
		govSvc:   govSvc,
		verifier: audit.NewChainVerifier(),
	}
}

// 1. ALLOW -> durable row -> provenance -> redaction -> hash verifies
func TestQA_DurableAllow(t *testing.T) {
	env := setupQAEnv(t, "qa-test-salt")
	ctx := context.Background()
	ws := "ws-qa-allow"

	normalVersion := "v1.0.0"
	h := sha256.Sum256([]byte(fixturepolicy.CedarSource))
	expectedHash := hex.EncodeToString(h[:])

	cand, err := env.pMgr.CreateCandidateWithVersion(ctx, ws, normalVersion, fixturepolicy.CedarSource, "permit")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.pMgr.Activate(ctx, ws, cand.Version); err != nil {
		t.Fatal(err)
	}

	req := decision.Request{
		ExecutionID:    "exec-qa-allow-1",
		WorkspaceID:    ws,
		Identity:       decision.Identity{AgentID: "agent-qa", Roles: []string{fixturepolicy.RoleReader}},
		Tool:           decision.ToolRef{BackendID: "b1", Name: "read_records"},
		Classification: decision.ToolClassification{Known: true, Risk: fixturepolicy.RiskRead},
		Arguments:      map[string]decision.AttributeValue{"query": decision.StringAttr("status=active")},
	}

	res, err := env.auditSvc.Evaluate(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != decision.Allow {
		t.Fatalf("expected ALLOW, got %s (reason: %s)", res.Decision, res.Reason)
	}

	// Retrieve durable audit record
	record, err := env.aStore.GetLatestRecord(ctx, ws)
	if err != nil {
		t.Fatalf("failed to retrieve durable record: %v", err)
	}

	if record.Decision != "ALLOW" {
		t.Fatalf("expected audit decision ALLOW, got %s", record.Decision)
	}
	// Provenance: exact evaluated policy version
	if record.PolicyVersion != normalVersion {
		t.Fatalf("expected policy version %s, got %s", normalVersion, record.PolicyVersion)
	}
	// Provenance: exact SHA-256 of evaluated policy bytes
	if record.PolicyHash != expectedHash {
		t.Fatalf("expected policy hash %s, got %s", expectedHash, record.PolicyHash)
	}
	// Provenance invariant: policy_hash must be distinct from normal version identifier
	if record.PolicyHash == record.PolicyVersion {
		t.Fatalf("expected policy_hash != policy_version; got identical %s", record.PolicyHash)
	}
	if record.ExecutionID != "exec-qa-allow-1" {
		t.Fatalf("expected execution id exec-qa-allow-1, got %s", record.ExecutionID)
	}
	if record.RedactedArguments["query"] != "status=active" {
		t.Fatalf("expected query argument preserved, got %v", record.RedactedArguments)
	}

	// Verify chain integrity
	verRes, err := env.verifier.VerifyWorkspace(ctx, env.aStore, ws)
	if err != nil {
		t.Fatal(err)
	}
	if !verRes.Valid {
		t.Fatalf("expected valid chain, got invalid: %s", verRes.Details)
	}
}

// 2. DENY -> durable row -> provenance -> redaction -> hash verifies
func TestQA_DurableDeny(t *testing.T) {
	env := setupQAEnv(t, "qa-test-salt")
	ctx := context.Background()
	ws := "ws-qa-deny"

	req := decision.Request{
		ExecutionID:    "exec-qa-deny-1",
		WorkspaceID:    ws,
		Identity:       decision.Identity{AgentID: "agent-qa", Roles: []string{fixturepolicy.RoleReader}},
		Tool:           decision.ToolRef{BackendID: "b1", Name: "unregistered_tool"},
		Classification: decision.ToolClassification{Known: false},
	}

	res, err := env.auditSvc.Evaluate(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != decision.Deny {
		t.Fatalf("expected DENY, got %s", res.Decision)
	}

	record, err := env.aStore.GetLatestRecord(ctx, ws)
	if err != nil {
		t.Fatalf("failed to retrieve durable record: %v", err)
	}

	if record.Decision != "DENY" {
		t.Fatalf("expected audit decision DENY, got %s", record.Decision)
	}
	if record.Reason != string(decision.ReasonUnknownTool) {
		t.Fatalf("expected reason %s, got %s", decision.ReasonUnknownTool, record.Reason)
	}

	verRes, err := env.verifier.VerifyWorkspace(ctx, env.aStore, ws)
	if err != nil {
		t.Fatal(err)
	}
	if !verRes.Valid {
		t.Fatalf("expected valid chain, got: %s", verRes.Details)
	}
}

// 3. Policy change preserves old provenance
func TestQA_PolicyChangePreservesOldProvenance(t *testing.T) {
	env := setupQAEnv(t, "qa-test-salt")
	ctx := context.Background()
	ws := "ws-qa-provenance"

	v1ID := "v1.0.0"
	h1 := sha256.Sum256([]byte(fixturepolicy.CedarSource))
	expectedHash1 := hex.EncodeToString(h1[:])

	// Version 1
	cand1, _ := env.pMgr.CreateCandidateWithVersion(ctx, ws, v1ID, fixturepolicy.CedarSource, "v1")
	env.pMgr.Activate(ctx, ws, cand1.Version)

	req1 := decision.Request{
		ExecutionID:    "exec-v1",
		WorkspaceID:    ws,
		Identity:       decision.Identity{AgentID: "agent-1", Roles: []string{fixturepolicy.RoleReader}},
		Tool:           decision.ToolRef{BackendID: "b1", Name: "t1"},
		Classification: decision.ToolClassification{Known: true, Risk: fixturepolicy.RiskRead},
	}
	_, err := env.auditSvc.Evaluate(ctx, req1)
	if err != nil {
		t.Fatal(err)
	}

	// Version 2 (modified policy)
	v2ID := "v2.0.0"
	modifiedCedar := fixturepolicy.CedarSource + "\n// modification comment\n"
	h2 := sha256.Sum256([]byte(modifiedCedar))
	expectedHash2 := hex.EncodeToString(h2[:])

	cand2, _ := env.pMgr.CreateCandidateWithVersion(ctx, ws, v2ID, modifiedCedar, "v2")
	env.pMgr.Activate(ctx, ws, cand2.Version)

	req2 := decision.Request{
		ExecutionID:    "exec-v2",
		WorkspaceID:    ws,
		Identity:       decision.Identity{AgentID: "agent-1", Roles: []string{fixturepolicy.RoleReader}},
		Tool:           decision.ToolRef{BackendID: "b1", Name: "t1"},
		Classification: decision.ToolClassification{Known: true, Risk: fixturepolicy.RiskRead},
	}
	_, err = env.auditSvc.Evaluate(ctx, req2)
	if err != nil {
		t.Fatal(err)
	}

	records, err := env.aStore.ListRecords(ctx, ws, 20)
	if err != nil {
		t.Fatal(err)
	}

	var dec1, dec2 *audit.StoredRecord
	for i := range records {
		if records[i].ExecutionID == "exec-v1" {
			dec1 = &records[i]
		}
		if records[i].ExecutionID == "exec-v2" {
			dec2 = &records[i]
		}
	}
	if dec1 == nil || dec2 == nil {
		t.Fatalf("expected durable decision records for exec-v1 and exec-v2; total records: %d", len(records))
	}

	// Verify Decision 1 retains v1ID and expectedHash1
	if dec1.PolicyVersion != v1ID {
		t.Fatalf("PROVENANCE DRIFT: expected decision 1 to retain version %s, got %s", v1ID, dec1.PolicyVersion)
	}
	if dec1.PolicyHash != expectedHash1 {
		t.Fatalf("PROVENANCE HASH DRIFT: expected decision 1 to retain hash %s, got %s", expectedHash1, dec1.PolicyHash)
	}
	if dec1.PolicyHash == dec1.PolicyVersion {
		t.Fatalf("expected policy_hash != policy_version on decision 1")
	}

	// Verify Decision 2 retains v2ID and expectedHash2
	if dec2.PolicyVersion != v2ID {
		t.Fatalf("expected decision 2 to have version %s, got %s", v2ID, dec2.PolicyVersion)
	}
	if dec2.PolicyHash != expectedHash2 {
		t.Fatalf("expected decision 2 to have hash %s, got %s", expectedHash2, dec2.PolicyHash)
	}
	if dec2.PolicyHash == dec2.PolicyVersion {
		t.Fatalf("expected policy_hash != policy_version on decision 2")
	}
}

// 4. Sensitive arguments redacted before persistence/canonicalization
func TestQA_SyntheticSecretsRedacted(t *testing.T) {
	env := setupQAEnv(t, "qa-salt-secret")
	ctx := context.Background()
	ws := "ws-qa-redaction"

	cand, _ := env.pMgr.CreateCandidate(ctx, ws, fixturepolicy.CedarSource, "v1")
	env.pMgr.Activate(ctx, ws, cand.Version)

	syntheticPassword := "super-secret-password-12345"
	syntheticAPIKey := "sk-antigravity-live-key-xyz987"
	syntheticBearer := "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.dummy.sig"

	req := decision.Request{
		ExecutionID:    "exec-secrets",
		WorkspaceID:    ws,
		Identity:       decision.Identity{AgentID: "agent-sec", Roles: []string{fixturepolicy.RoleReader}},
		Tool:           decision.ToolRef{BackendID: "b1", Name: "auth_proxy"},
		Classification: decision.ToolClassification{Known: true, Risk: fixturepolicy.RiskRead},
		Arguments: map[string]decision.AttributeValue{
			"db_password":   decision.StringAttr(syntheticPassword),
			"api_key":       decision.StringAttr(syntheticAPIKey),
			"auth_header":   decision.StringAttr(syntheticBearer),
			"public_filter": decision.StringAttr("region=us-east"),
		},
	}

	_, err := env.auditSvc.Evaluate(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	rec, err := env.aStore.GetLatestRecord(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}

	// Assert raw secrets never appear in RedactedArguments map
	for k, v := range rec.RedactedArguments {
		if v == syntheticPassword || v == syntheticAPIKey || v == syntheticBearer {
			t.Fatalf("SECURITY VIOLATION: raw secret found in audit argument %s: %s", k, v)
		}
	}

	// Assert raw secrets never appear in CanonicalPayload string
	if containsSubstring(rec.CanonicalPayload, syntheticPassword) ||
		containsSubstring(rec.CanonicalPayload, syntheticAPIKey) ||
		containsSubstring(rec.CanonicalPayload, syntheticBearer) {
		t.Fatal("SECURITY VIOLATION: raw secret found inside serialized canonical payload!")
	}

	// Assert public filter is preserved
	if rec.RedactedArguments["public_filter"] != "region=us-east" {
		t.Fatalf("expected public argument preserved, got %v", rec.RedactedArguments)
	}
}

// 5. Tampered row detected immediately by verifier
func TestQA_HashChainVerificationAndTamperDetection(t *testing.T) {
	memStore := audit.NewMemoryStore()
	verifier := audit.NewChainVerifier()
	ctx := context.Background()
	ws := "ws-qa-tamper"

	prevHash := audit.GenesisHash
	for i := 1; i <= 4; i++ {
		rec := audit.DecisionRecord{
			WorkspaceID:      ws,
			ExecutionID:      fmt.Sprintf("ex-%d", i),
			Timestamp:        time.Now().UTC(),
			Decision:         "ALLOW",
			Reason:           "policy_allow",
			PrincipalAgentID: "agent",
			ToolBackendID:    "b",
			ToolName:         "t",
			ToolRisk:         "read",
			PrevHash:         prevHash,
		}
		c, _ := audit.ComputeCanonicalPayload(rec)
		rec.CanonicalPayload = string(c)
		rec.RowHash = audit.ComputeRowHash(prevHash, c)
		prevHash = rec.RowHash
		_, _ = memStore.AppendDecision(ctx, rec)
	}

	// Verify valid initially
	res, err := verifier.VerifyWorkspace(ctx, memStore, ws)
	if err != nil || !res.Valid {
		t.Fatalf("expected valid initial chain: %v", err)
	}

	// Simulate tampering on row 2
	r2, _ := memStore.GetRecordBySequence(ctx, ws, 2)
	tampered := *r2
	tampered.Decision = "DENY" // modified without recalculating hash
	memStore.SimulateTamper(ws, 2, tampered)

	// Verify tampering is caught
	res, err = verifier.VerifyWorkspace(ctx, memStore, ws)
	if err != nil {
		t.Fatal(err)
	}
	if res.Valid {
		t.Fatal("SECURITY FAILURE: verifier failed to detect tampered row!")
	}
	if res.BrokenSequence != 2 {
		t.Fatalf("expected broken at sequence 2, got %d", res.BrokenSequence)
	}
}

// 6. Audit failure fails closed on ALLOW (Invariant O-002)
func TestQA_AuditOutageFailsClosed(t *testing.T) {
	env := setupQAEnv(t, "salt")
	ctx := context.Background()
	ws := "ws-qa-outage"

	cand, _ := env.pMgr.CreateCandidate(ctx, ws, fixturepolicy.CedarSource, "v1")
	env.pMgr.Activate(ctx, ws, cand.Version)

	// Inject DB outage into audit store
	env.aStore.(*audit.MemoryStore).SetFault(errors.New("db outage"))

	req := decision.Request{
		ExecutionID:    "exec-outage-proof",
		WorkspaceID:    ws,
		Identity:       decision.Identity{AgentID: "agent-1", Roles: []string{fixturepolicy.RoleReader}},
		Tool:           decision.ToolRef{BackendID: "b1", Name: "t1"},
		Classification: decision.ToolClassification{Known: true, Risk: fixturepolicy.RiskRead},
	}

	res, err := env.auditSvc.Evaluate(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// INVARIANT O-002: Must fail closed to DENY
	if res.Decision != decision.Deny {
		t.Fatalf("CRITICAL SECURITY FAILURE: ALLOW returned during audit outage! Result: %s", res.Decision)
	}
	if res.Reason != decision.ReasonEvaluationError {
		t.Fatalf("expected reason evaluation_error, got %s", res.Reason)
	}
}

// 7. Multi-workspace isolation: independent chains and sequences
func TestQA_MultiWorkspaceChainIsolation(t *testing.T) {
	env := setupQAEnv(t, "salt")
	ctx := context.Background()
	wsA := "ws-tenant-alpha"
	wsB := "ws-tenant-beta"

	candA, _ := env.pMgr.CreateCandidate(ctx, wsA, fixturepolicy.CedarSource, "v1")
	env.pMgr.Activate(ctx, wsA, candA.Version)

	candB, _ := env.pMgr.CreateCandidate(ctx, wsB, fixturepolicy.CedarSource, "v1")
	env.pMgr.Activate(ctx, wsB, candB.Version)

	// Generate decisions in interleaved fashion
	for i := 1; i <= 3; i++ {
		reqA := decision.Request{
			ExecutionID:    fmt.Sprintf("exec-a-%d", i),
			WorkspaceID:    wsA,
			Identity:       decision.Identity{AgentID: "agent-a", Roles: []string{fixturepolicy.RoleReader}},
			Tool:           decision.ToolRef{BackendID: "b1", Name: "t1"},
			Classification: decision.ToolClassification{Known: true, Risk: fixturepolicy.RiskRead},
		}
		_, _ = env.auditSvc.Evaluate(ctx, reqA)

		reqB := decision.Request{
			ExecutionID:    fmt.Sprintf("exec-b-%d", i),
			WorkspaceID:    wsB,
			Identity:       decision.Identity{AgentID: "agent-b", Roles: []string{fixturepolicy.RoleReader}},
			Tool:           decision.ToolRef{BackendID: "b1", Name: "t1"},
			Classification: decision.ToolClassification{Known: true, Risk: fixturepolicy.RiskRead},
		}
		_, _ = env.auditSvc.Evaluate(ctx, reqB)
	}

	// Verify both workspace chains independently
	resA, err := env.verifier.VerifyWorkspace(ctx, env.aStore, wsA)
	if err != nil || !resA.Valid {
		t.Fatalf("workspace A chain invalid: %v, details: %s", err, resA.Details)
	}

	resB, err := env.verifier.VerifyWorkspace(ctx, env.aStore, wsB)
	if err != nil || !resB.Valid {
		t.Fatalf("workspace B chain invalid: %v, details: %s", err, resB.Details)
	}

	// Both should have identical number of records
	if resA.TotalRecords != resB.TotalRecords {
		t.Fatalf("expected equal records, got A=%d, B=%d", resA.TotalRecords, resB.TotalRecords)
	}
}

// 8. Concurrent audit writes produce sequential unbroken chain
func TestQA_ConcurrentAuditWrites(t *testing.T) {
	env := setupQAEnv(t, "salt")
	ctx := context.Background()
	ws := "ws-qa-concurrency"

	cand, _ := env.pMgr.CreateCandidate(ctx, ws, fixturepolicy.CedarSource, "v1")
	env.pMgr.Activate(ctx, ws, cand.Version)

	const numWorkers = 10
	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()
			req := decision.Request{
				ExecutionID:    fmt.Sprintf("exec-conc-%d", workerID),
				WorkspaceID:    ws,
				Identity:       decision.Identity{AgentID: "agent-conc", Roles: []string{fixturepolicy.RoleReader}},
				Tool:           decision.ToolRef{BackendID: "b1", Name: "t1"},
				Classification: decision.ToolClassification{Known: true, Risk: fixturepolicy.RiskRead},
			}
			_, err := env.auditSvc.Evaluate(ctx, req)
			if err != nil {
				t.Errorf("worker %d evaluation failed: %v", workerID, err)
			}
		}(i)
	}

	wg.Wait()

	// Verify chain integrity after concurrent writes
	res, err := env.verifier.VerifyWorkspace(ctx, env.aStore, ws)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("concurrency broke hash chain: %s", res.Details)
	}
	// Initial candidate creation (seq 1) + activation (seq 2) + 10 workers = 12 total records
	if res.TotalRecords != int64(numWorkers+2) {
		t.Fatalf("expected %d total records, got %d", numWorkers+2, res.TotalRecords)
	}
}

// 9. Restart preserves committed records
func TestQA_RestartPreservesCommittedRecords(t *testing.T) {
	memStore := audit.NewMemoryStore()
	ctx := context.Background()
	ws := "ws-qa-restart"

	rec := audit.DecisionRecord{
		WorkspaceID:      ws,
		ExecutionID:      "exec-pre-restart",
		Timestamp:        time.Now().UTC(),
		Decision:         "ALLOW",
		Reason:           "policy_allow",
		PrincipalAgentID: "agent",
		ToolBackendID:    "b",
		ToolName:         "t",
		ToolRisk:         "read",
		PrevHash:         audit.GenesisHash,
	}
	c, _ := audit.ComputeCanonicalPayload(rec)
	rec.CanonicalPayload = string(c)
	rec.RowHash = audit.ComputeRowHash(audit.GenesisHash, c)
	_, _ = memStore.AppendDecision(ctx, rec)

	// Simulate service restart by initializing new service using existing store
	redactor := audit.NewRedactor("salt", nil)
	newService := audit.NewAuditedDecisionService(nil, memStore, redactor)

	// New decision after restart
	req := decision.Request{
		ExecutionID:    "exec-post-restart",
		WorkspaceID:    ws,
		Identity:       decision.Identity{AgentID: "agent", Roles: []string{"reader"}},
		Tool:           decision.ToolRef{BackendID: "b", Name: "t"},
		Classification: decision.ToolClassification{Known: true, Risk: "read"},
	}
	_, _ = newService.Evaluate(ctx, req)

	verifier := audit.NewChainVerifier()
	res, err := verifier.VerifyWorkspace(ctx, memStore, ws)
	if err != nil || !res.Valid {
		t.Fatalf("restart recovery failed chain verification: %v, details: %s", err, res.Details)
	}
	if res.TotalRecords != 2 {
		t.Fatalf("expected 2 records after restart, got %d", res.TotalRecords)
	}
}

func containsSubstring(s, substr string) bool {
	return len(substr) > 0 && len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
