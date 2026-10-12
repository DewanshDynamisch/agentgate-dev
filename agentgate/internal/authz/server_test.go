package authz

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Dynamisch-LLC/agentgate/internal/audit"
	"github.com/Dynamisch-LLC/agentgate/internal/decision"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/structpb"
)

type mockEvaluator struct {
	decision decision.Decision
	reason   decision.ReasonCode
	err      error
}

func (m *mockEvaluator) EvaluateWithActivePolicy(_ context.Context, _ string, req decision.Request) (decision.Result, error) {
	if m.err != nil {
		return decision.Result{}, m.err
	}
	if !req.Classification.Known {
		return decision.Result{
			Decision:      decision.Deny,
			Reason:        decision.ReasonUnknownTool,
			ExecutionID:   req.ExecutionID,
			PolicyVersion: "",
		}, nil
	}
	return decision.Result{
		Decision:      m.decision,
		Reason:        m.reason,
		ExecutionID:   req.ExecutionID,
		PolicyVersion: "v1.0.0",
	}, nil
}

type failingAuditStore struct {
	audit.Store
}

func (f *failingAuditStore) AppendDecision(_ context.Context, _ audit.DecisionRecord) (*audit.StoredRecord, error) {
	return nil, errors.New("simulated disk write failure")
}
func (f *failingAuditStore) GetLatestRecord(_ context.Context, _ string) (*audit.StoredRecord, error) {
	return nil, nil
}

func newTestJWTMetadata(t *testing.T, sub string, roles string) *corev3.Metadata {
	t.Helper()
	s, err := structpb.NewStruct(map[string]any{
		"sub":   sub,
		"roles": roles,
	})
	if err != nil {
		t.Fatalf("structpb: %v", err)
	}
	return &corev3.Metadata{
		FilterMetadata: map[string]*structpb.Struct{
			"envoy.filters.http.jwt_authn": s,
		},
	}
}

func setupTestServer(t *testing.T, dec decision.Decision, reason decision.ReasonCode) (*Server, *audit.MemoryStore) {
	t.Helper()
	adapter := setupTestAdapter(t)
	mockEval := &mockEvaluator{decision: dec, reason: reason}
	memStore := audit.NewMemoryStore()
	auditedSvc := audit.NewAuditedDecisionService(mockEval, memStore, audit.NewRedactor("", nil))
	return NewServer(adapter, auditedSvc), memStore
}

func TestAuthzServer_Allow(t *testing.T) {
	srv, _ := setupTestServer(t, decision.Allow, decision.ReasonPolicyAllow)

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Method: "POST",
					Path:   "/",
					Body:   `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_status","arguments":{"verbose":true}}}`,
				},
			},
			MetadataContext: newTestJWTMetadata(t, "agent-001", "reader"),
		},
	}

	resp, err := srv.Check(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Check error: %v", err)
	}
	if resp.Status.Code != int32(codes.OK) {
		t.Errorf("expected Status.Code == OK (0), got %d", resp.Status.Code)
	}
	if resp.GetOkResponse() == nil {
		t.Fatal("expected OkResponse, got nil")
	}

	hasAllowHeader := false
	for _, h := range resp.GetOkResponse().Headers {
		if h.Header.Key == "x-agentgate-decision" && h.Header.Value == "allow" {
			hasAllowHeader = true
		}
	}
	if !hasAllowHeader {
		t.Errorf("expected x-agentgate-decision: allow header in OkResponse")
	}
}

func TestAuthzServer_Deny(t *testing.T) {
	srv, _ := setupTestServer(t, decision.Deny, decision.ReasonPolicyDeny)

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Method: "POST",
					Path:   "/",
					Body:   `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_status","arguments":{"verbose":true}}}`,
				},
			},
			MetadataContext: newTestJWTMetadata(t, "agent-001", "reader"),
		},
	}

	resp, err := srv.Check(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Check error: %v", err)
	}
	if resp.Status.Code != int32(codes.PermissionDenied) {
		t.Errorf("expected Status.Code == PermissionDenied (7), got %d", resp.Status.Code)
	}
	deniedResp := resp.GetDeniedResponse()
	if deniedResp == nil {
		t.Fatal("expected DeniedResponse, got nil")
	}
	if deniedResp.Status.Code != typev3.StatusCode_Forbidden {
		t.Errorf("expected HTTP status Forbidden (403), got %v", deniedResp.Status.Code)
	}

	hasDenyHeader := false
	for _, h := range deniedResp.Headers {
		if h.Header.Key == "x-agentgate-decision" && h.Header.Value == "deny" {
			hasDenyHeader = true
		}
	}
	if !hasDenyHeader {
		t.Errorf("expected x-agentgate-decision: deny header in DeniedResponse")
	}
}

func TestAuthzServer_UnknownTool(t *testing.T) {
	srv, _ := setupTestServer(t, decision.Allow, decision.ReasonPolicyAllow)

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"unknown_tool"}}`,
				},
			},
			MetadataContext: newTestJWTMetadata(t, "agent-001", "reader"),
		},
	}

	resp, err := srv.Check(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Check error: %v", err)
	}
	if resp.Status.Code != int32(codes.PermissionDenied) {
		t.Errorf("expected PermissionDenied for unknown tool, got %d", resp.Status.Code)
	}
	if resp.GetDeniedResponse() == nil {
		t.Fatal("expected DeniedResponse for unknown tool, got nil")
	}
}

func TestAuthzServer_MissingIdentity(t *testing.T) {
	srv, _ := setupTestServer(t, decision.Allow, decision.ReasonPolicyAllow)

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_status"}}`,
				},
			},
		},
	}

	resp, err := srv.Check(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Check error: %v", err)
	}
	if resp.Status.Code != int32(codes.PermissionDenied) {
		t.Errorf("expected PermissionDenied for missing identity, got %d", resp.Status.Code)
	}
	if resp.GetDeniedResponse() == nil {
		t.Fatal("expected DeniedResponse for missing identity, got nil")
	}
}

func TestAuthzServer_AuditFailureFailsClosed_O002(t *testing.T) {
	adapter := setupTestAdapter(t)
	// Evaluator would allow
	mockEval := &mockEvaluator{decision: decision.Allow, reason: decision.ReasonPolicyAllow}
	// Audit store will fail on append
	failingStore := &failingAuditStore{Store: audit.NewMemoryStore()}
	auditedSvc := audit.NewAuditedDecisionService(mockEval, failingStore, audit.NewRedactor("", nil))
	srv := NewServer(adapter, auditedSvc)

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_status","arguments":{"verbose":true}}}`,
				},
			},
			MetadataContext: newTestJWTMetadata(t, "agent-001", "reader"),
		},
	}

	resp, err := srv.Check(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Check error: %v", err)
	}
	if resp.Status.Code != int32(codes.PermissionDenied) {
		t.Errorf("O-002 invariant violated: expected PermissionDenied on audit failure, got %d", resp.Status.Code)
	}
	if resp.GetDeniedResponse() == nil {
		t.Fatal("expected DeniedResponse on audit failure, got nil")
	}
}

func TestAuthzServer_MalformedRequest(t *testing.T) {
	srv, _ := setupTestServer(t, decision.Allow, decision.ReasonPolicyAllow)

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Body: `{NOT VALID JSON`,
				},
			},
			MetadataContext: newTestJWTMetadata(t, "agent-001", "reader"),
		},
	}

	resp, err := srv.Check(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Check error: %v", err)
	}
	if resp.Status.Code != int32(codes.PermissionDenied) {
		t.Errorf("expected PermissionDenied on malformed JSON, got %d", resp.Status.Code)
	}
	if resp.GetDeniedResponse() == nil {
		t.Fatal("expected DeniedResponse on malformed JSON, got nil")
	}
}

// TestAuthzServer_AdapterError_Audited proves that pre-decision adaptation failures
// (e.g. unknown tool or malformed request) durably record a DENY audit event.
func TestAuthzServer_AdapterError_Audited(t *testing.T) {
	srv, memStore := setupTestServer(t, decision.Allow, decision.ReasonPolicyAllow)

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"unknown_tool"}}`,
				},
			},
			MetadataContext: newTestJWTMetadata(t, "agent-001", "reader"),
		},
	}

	resp, err := srv.Check(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Check error: %v", err)
	}
	if resp.Status.Code != int32(codes.PermissionDenied) {
		t.Errorf("expected PermissionDenied, got %d", resp.Status.Code)
	}

	// Verify that a durable DENY audit record was written to the store
	latest, err := memStore.GetLatestRecord(context.Background(), "ws-test")
	if err != nil || latest == nil {
		t.Fatalf("expected durable audit record for adaptation failure, got record=%v, err=%v", latest, err)
	}
	if latest.Decision != "DENY" {
		t.Errorf("expected audited decision DENY, got %q", latest.Decision)
	}
}

type slowFakeEvaluator struct{}

func (s *slowFakeEvaluator) Evaluate(ctx context.Context, req decision.Request) (decision.Result, error) {
	select {
	case <-time.After(5 * time.Second):
		return decision.Result{Decision: decision.Allow, Reason: decision.ReasonPolicyAllow}, nil
	case <-ctx.Done():
		return decision.Result{}, ctx.Err()
	}
}

func TestCheck_SlowEvaluatorTimesOut(t *testing.T) {
	adapter := setupTestAdapter(t)
	slowEval := &slowFakeEvaluator{}
	srv := NewServer(adapter, slowEval)

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Method: "POST",
					Path:   "/",
					Body:   `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_status","arguments":{"verbose":true}}}`,
				},
			},
			MetadataContext: newTestJWTMetadata(t, "agent-001", "reader"),
		},
	}

	start := time.Now()
	resp, err := srv.Check(context.Background(), req)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected Check error: %v", err)
	}
	if resp.Status.Code != int32(codes.PermissionDenied) {
		t.Fatalf("expected Status.Code == PermissionDenied, got %d", resp.Status.Code)
	}
	if resp.GetDeniedResponse() == nil {
		t.Fatal("expected DeniedResponse, got nil")
	}

	// Must return within ~3s, not 5s
	if elapsed < 2500*time.Millisecond || elapsed > 4500*time.Millisecond {
		t.Fatalf("expected Check to return within ~3s (between 2.5s and 4.5s), took %v", elapsed)
	}

	hasDenyReason := false
	for _, h := range resp.GetDeniedResponse().Headers {
		if h.Header.Key == "x-agentgate-reason" && h.Header.Value == "evaluation_error" {
			hasDenyReason = true
		}
	}
	if !hasDenyReason {
		t.Errorf("expected x-agentgate-reason == evaluation_error header in response")
	}
}
