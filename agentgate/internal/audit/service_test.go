package audit_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/Dynamisch-LLC/agentgate/internal/audit"
	"github.com/Dynamisch-LLC/agentgate/internal/credential"
	"github.com/Dynamisch-LLC/agentgate/internal/decision"
)

type mockEvaluator struct {
	result decision.Result
}

func (m *mockEvaluator) EvaluateWithActivePolicy(_ context.Context, _ string, req decision.Request) (decision.Result, error) {
	res := m.result
	res.ExecutionID = req.ExecutionID
	return res, nil
}

func TestAuditedDecisionService_Allow(t *testing.T) {
	evaluator := &mockEvaluator{
		result: decision.Result{
			Decision:      decision.Allow,
			Reason:        decision.ReasonPolicyAllow,
			PolicyVersion: "ver-cand-123",
		},
	}
	aStore := audit.NewMemoryStore()
	redactor := audit.NewRedactor("salt", nil)
	auditSvc := audit.NewAuditedDecisionService(evaluator, aStore, redactor)

	ctx := context.Background()
	ws := "ws-audited-svc"

	req := decision.Request{
		ExecutionID:    "exec-audit-1",
		WorkspaceID:    ws,
		Identity:       decision.Identity{AgentID: "agent-1", Roles: []string{"reader"}},
		Tool:           decision.ToolRef{BackendID: "b1", Name: "t1"},
		Classification: decision.ToolClassification{Known: true, Risk: "read"},
		Arguments:      map[string]decision.AttributeValue{"query": decision.StringAttr("SELECT 1")},
	}

	res, err := auditSvc.Evaluate(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != decision.Allow {
		t.Fatalf("expected ALLOW, got %s (reason: %s, message: %s)", res.Decision, res.Reason, res.Message)
	}

	latest, err := aStore.GetLatestRecord(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	if latest.ExecutionID != "exec-audit-1" || latest.Decision != "ALLOW" {
		t.Fatalf("audit mismatch: %+v", latest)
	}
	if latest.PolicyVersion != "ver-cand-123" {
		t.Fatalf("expected policy version ver-cand-123, got %s", latest.PolicyVersion)
	}
	if latest.RedactedArguments["query"] != "SELECT 1" {
		t.Fatalf("expected query SELECT 1, got %v", latest.RedactedArguments)
	}
}

func TestAuditedDecisionService_Deny(t *testing.T) {
	evaluator := &mockEvaluator{
		result: decision.Result{
			Decision: decision.Deny,
			Reason:   decision.ReasonUnknownTool,
		},
	}
	aStore := audit.NewMemoryStore()
	redactor := audit.NewRedactor("salt", nil)
	auditSvc := audit.NewAuditedDecisionService(evaluator, aStore, redactor)

	ctx := context.Background()
	ws := "ws-deny-svc"

	req := decision.Request{
		ExecutionID:    "exec-2",
		WorkspaceID:    ws,
		Identity:       decision.Identity{AgentID: "agent-1", Roles: []string{"reader"}},
		Tool:           decision.ToolRef{BackendID: "b1", Name: "unlisted-tool"},
		Classification: decision.ToolClassification{Known: false},
	}

	res, err := auditSvc.Evaluate(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != decision.Deny {
		t.Fatalf("expected DENY, got %s", res.Decision)
	}

	latest, err := aStore.GetLatestRecord(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Decision != "DENY" || latest.Reason != string(decision.ReasonUnknownTool) {
		t.Fatalf("expected durable DENY record, got %+v", latest)
	}
}

func TestAuditedDecisionService_AuditOutageFailsClosed(t *testing.T) {
	evaluator := &mockEvaluator{
		result: decision.Result{
			Decision:      decision.Allow,
			Reason:        decision.ReasonPolicyAllow,
			PolicyVersion: "ver-123",
		},
	}
	aStore := audit.NewMemoryStore()
	aStore.SetFault(errors.New("audit store unavailable"))

	redactor := audit.NewRedactor("salt", nil)
	auditSvc := audit.NewAuditedDecisionService(evaluator, aStore, redactor)

	ctx := context.Background()
	ws := "ws-outage-test"

	req := decision.Request{
		ExecutionID:    "exec-outage",
		WorkspaceID:    ws,
		Identity:       decision.Identity{AgentID: "agent-1", Roles: []string{"reader"}},
		Tool:           decision.ToolRef{BackendID: "b1", Name: "t1"},
		Classification: decision.ToolClassification{Known: true, Risk: "read"},
	}

	res, err := auditSvc.Evaluate(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != decision.Deny {
		t.Fatalf("invariant O-002: expected DENY, got %s", res.Decision)
	}
	if res.Reason != decision.ReasonEvaluationError {
		t.Fatalf("expected ReasonEvaluationError, got %s", res.Reason)
	}
}

type mockEvaluatorWithProvenance struct {
	result  decision.Result
	version string
	hash    string
}

func (m *mockEvaluatorWithProvenance) EvaluateWithActivePolicy(_ context.Context, _ string, req decision.Request) (decision.Result, error) {
	res := m.result
	res.ExecutionID = req.ExecutionID
	return res, nil
}

func (m *mockEvaluatorWithProvenance) GetActiveProvenance(_ string) (string, string, error) {
	return m.version, m.hash, nil
}

func TestAuditedDecisionService_ExactPolicyHashProvenance(t *testing.T) {
	policyBytes := []byte("permit(principal, action, resource);")
	h := sha256.Sum256(policyBytes)
	expectedHash := hex.EncodeToString(h[:])
	normalVersion := "v1.0.0"

	evaluator := &mockEvaluatorWithProvenance{
		result: decision.Result{
			Decision:      decision.Allow,
			Reason:        decision.ReasonPolicyAllow,
			PolicyVersion: expectedHash, // G1 decision.Engine returns SHA-256 in PolicyVersion
		},
		version: normalVersion,
		hash:    expectedHash,
	}

	aStore := audit.NewMemoryStore()
	redactor := audit.NewRedactor("salt", nil)
	auditSvc := audit.NewAuditedDecisionService(evaluator, aStore, redactor)

	ctx := context.Background()
	ws := "ws-provenance-test"

	req := decision.Request{
		ExecutionID:    "exec-prov-1",
		WorkspaceID:    ws,
		Identity:       decision.Identity{AgentID: "agent-1", Roles: []string{"reader"}},
		Tool:           decision.ToolRef{BackendID: "b1", Name: "t1"},
		Classification: decision.ToolClassification{Known: true, Risk: "read"},
	}

	res, err := auditSvc.Evaluate(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != decision.Allow {
		t.Fatalf("expected ALLOW, got %s", res.Decision)
	}

	record, err := aStore.GetLatestRecord(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Assert exact policy version preserved
	if record.PolicyVersion != normalVersion {
		t.Fatalf("expected policy_version %s, got %s", normalVersion, record.PolicyVersion)
	}

	// 2. Assert exact policy hash equals SHA-256 of evaluated policy bytes
	if record.PolicyHash != expectedHash {
		t.Fatalf("expected policy_hash == SHA-256 of evaluated bytes (%s), got %s", expectedHash, record.PolicyHash)
	}

	// 3. Assert policy_hash != policy_version for normal version identifier
	if record.PolicyHash == record.PolicyVersion {
		t.Fatalf("regression: policy_hash must not equal policy_version (%s == %s)", record.PolicyHash, record.PolicyVersion)
	}

	// 4. Pre-Cedar denial must leave both policy_version and policy_hash empty
	denyEvaluator := &mockEvaluatorWithProvenance{
		result: decision.Result{
			Decision:      decision.Deny,
			Reason:        decision.ReasonUnknownTool,
			PolicyVersion: "", // Cedar not reached
		},
		version: normalVersion,
		hash:    expectedHash,
	}
	auditSvc.SetEvaluator(denyEvaluator)

	reqDeny := decision.Request{
		ExecutionID:    "exec-prov-2",
		WorkspaceID:    ws,
		Identity:       decision.Identity{AgentID: "agent-1", Roles: []string{"reader"}},
		Tool:           decision.ToolRef{BackendID: "b1", Name: "unregistered"},
		Classification: decision.ToolClassification{Known: false},
	}
	_, err = auditSvc.Evaluate(ctx, reqDeny)
	if err != nil {
		t.Fatal(err)
	}
	recordDeny, err := aStore.GetLatestRecord(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	if recordDeny.PolicyVersion != "" || recordDeny.PolicyHash != "" {
		t.Fatalf("pre-Cedar denial must not carry policy provenance: version=%q hash=%q", recordDeny.PolicyVersion, recordDeny.PolicyHash)
	}
}

type fakeIssuer struct {
	calls int
	cred  credential.Credential
	err   error
}

func (f *fakeIssuer) Issue(_ context.Context, _ decision.Request, _ decision.Result) (credential.Credential, error) {
	f.calls++
	return f.cred, f.err
}

type failAppendStore struct{ audit.Store }

func (failAppendStore) AppendDecision(context.Context, audit.DecisionRecord) (*audit.StoredRecord, error) {
	return nil, errors.New("append failed")
}

func credTestReq(ws, exec string) decision.Request {
	return decision.Request{
		ExecutionID:    exec,
		WorkspaceID:    ws,
		Identity:       decision.Identity{AgentID: "agent-1", Roles: []string{"reader"}},
		Tool:           decision.ToolRef{BackendID: "b1", Name: "t1"},
		Classification: decision.ToolClassification{Known: true, Risk: "read"},
	}
}

func allowEvaluator() *mockEvaluator {
	return &mockEvaluator{result: decision.Result{Decision: decision.Allow, Reason: decision.ReasonPolicyAllow, PolicyVersion: "v1"}}
}

func TestEvaluateWithCredential_AllowIssuesAndRecordsRef(t *testing.T) {
	ctx := context.Background()
	store := audit.NewMemoryStore()
	iss := &fakeIssuer{cred: credential.Credential{HeaderName: "Authorization", HeaderValue: "Bearer tok", Ref: "ref-1"}}
	svc := audit.NewAuditedDecisionService(allowEvaluator(), store, nil)
	svc.SetIssuer(iss)

	res, cred, err := svc.EvaluateWithCredential(ctx, credTestReq("ws-c1", "e1"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != decision.Allow || cred == nil || cred.Ref != "ref-1" || cred.HeaderValue != "Bearer tok" {
		t.Fatalf("unexpected result: %+v cred=%+v", res, cred)
	}
	rec, err := store.GetLatestRecord(ctx, "ws-c1")
	if err != nil {
		t.Fatal(err)
	}
	if rec.DownstreamCredentialRef != "ref-1" || rec.Decision != "ALLOW" {
		t.Fatalf("record mismatch: %+v", rec)
	}
	vr, err := audit.NewChainVerifier().VerifyWorkspace(ctx, store, "ws-c1")
	if err != nil || !vr.Valid {
		t.Fatalf("chain invalid: %+v err=%v", vr, err)
	}
}

func TestEvaluateWithCredential_IssueFailureDenies(t *testing.T) {
	ctx := context.Background()
	store := audit.NewMemoryStore()
	iss := &fakeIssuer{err: errors.New("signer down")}
	svc := audit.NewAuditedDecisionService(allowEvaluator(), store, nil)
	svc.SetIssuer(iss)

	res, cred, err := svc.EvaluateWithCredential(ctx, credTestReq("ws-c2", "e2"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != decision.Deny || res.Reason != decision.ReasonCredentialIssuanceFailure || cred != nil {
		t.Fatalf("expected credential_issuance_failure deny, got %+v cred=%+v", res, cred)
	}
	if res.PolicyVersion != "v1" || res.ExecutionID != "e2" {
		t.Fatalf("provenance lost: %+v", res)
	}
	rec, err := store.GetLatestRecord(ctx, "ws-c2")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Decision != "DENY" || rec.Reason != string(decision.ReasonCredentialIssuanceFailure) || rec.DownstreamCredentialRef != "" {
		t.Fatalf("record mismatch: %+v", rec)
	}
	vr, err := audit.NewChainVerifier().VerifyWorkspace(ctx, store, "ws-c2")
	if err != nil || !vr.Valid {
		t.Fatalf("chain invalid: %+v err=%v", vr, err)
	}
}

func TestEvaluateWithCredential_DenyNeverIssues(t *testing.T) {
	ev := &mockEvaluator{result: decision.Result{Decision: decision.Deny, Reason: decision.ReasonUnknownTool}}
	iss := &fakeIssuer{cred: credential.Credential{Ref: "x"}}
	svc := audit.NewAuditedDecisionService(ev, audit.NewMemoryStore(), nil)
	svc.SetIssuer(iss)

	res, cred, err := svc.EvaluateWithCredential(context.Background(), credTestReq("ws-c3", "e3"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != decision.Deny || cred != nil || iss.calls != 0 {
		t.Fatalf("issuer must not run on deny: res=%+v cred=%+v calls=%d", res, cred, iss.calls)
	}
}

func TestEvaluateWithCredential_NoIssuerUnchanged(t *testing.T) {
	ctx := context.Background()
	store := audit.NewMemoryStore()
	svc := audit.NewAuditedDecisionService(allowEvaluator(), store, nil)

	res, cred, err := svc.EvaluateWithCredential(ctx, credTestReq("ws-c4", "e4"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != decision.Allow || cred != nil {
		t.Fatalf("unexpected: %+v cred=%+v", res, cred)
	}
	rec, _ := store.GetLatestRecord(ctx, "ws-c4")
	if rec.DownstreamCredentialRef != "" {
		t.Fatalf("ref must be empty: %+v", rec)
	}
}

func TestEvaluateWithCredential_AppendFailureDiscardsCredential(t *testing.T) {
	iss := &fakeIssuer{cred: credential.Credential{Ref: "ref-5", HeaderValue: "Bearer tok"}}
	svc := audit.NewAuditedDecisionService(allowEvaluator(), failAppendStore{audit.NewMemoryStore()}, nil)
	svc.SetIssuer(iss)

	res, cred, err := svc.EvaluateWithCredential(context.Background(), credTestReq("ws-c5", "e5"))
	if err != nil {
		t.Fatal(err)
	}
	if iss.calls != 1 {
		t.Fatalf("issuer should have run, calls=%d", iss.calls)
	}
	if res.Decision != decision.Deny || res.Reason != decision.ReasonEvaluationError || cred != nil {
		t.Fatalf("expected deny with nil credential, got %+v cred=%+v", res, cred)
	}
}

func TestEvaluate_WrapperDiscardsCredential(t *testing.T) {
	iss := &fakeIssuer{cred: credential.Credential{Ref: "ref-6"}}
	svc := audit.NewAuditedDecisionService(allowEvaluator(), audit.NewMemoryStore(), nil)
	svc.SetIssuer(iss)
	res, err := svc.Evaluate(context.Background(), credTestReq("ws-c6", "e6"))
	if err != nil || res.Decision != decision.Allow {
		t.Fatalf("unexpected: %+v err=%v", res, err)
	}
}
