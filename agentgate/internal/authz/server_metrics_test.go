package authz

import (
	"context"
	"strings"
	"testing"

	"github.com/Dynamisch-LLC/agentgate/internal/audit"
	"github.com/Dynamisch-LLC/agentgate/internal/decision"
	"github.com/Dynamisch-LLC/agentgate/internal/metrics"
	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	"google.golang.org/grpc/codes"
)

func TestAuthzServer_MetricsExposition(t *testing.T) {
	adapter := setupTestAdapter(t)
	mockEval := &mockEvaluator{decision: decision.Deny, reason: decision.ReasonUnknownTool}
	memStore := audit.NewMemoryStore()
	auditedSvc := audit.NewAuditedDecisionService(mockEval, memStore, audit.NewRedactor("", nil))

	m := metrics.NewMetrics("test-version")

	srv := NewServerWithOptions(adapter, auditedSvc, ServerOptions{
		DecisionsTotal:             m.DecisionsTotal,
		DecisionDurationSeconds:    m.DecisionDurationSeconds,
		CredentialIssuanceFailures: m.CredentialIssuanceFailures,
	})

	// Request with an unknown tool
	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Method: "POST",
					Path:   "/",
					Body:   `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"unknown_unclassified_tool"}}`,
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
		t.Fatalf("expected PermissionDenied, got %v", resp.Status.Code)
	}

	var buf strings.Builder
	if err := m.Registry.WriteText(&buf); err != nil {
		t.Fatalf("WriteText error: %v", err)
	}
	out := buf.String()

	// 1. Verify decisions total contains DENY unknown_tool
	expectedDecision := `agentgate_decisions_total{decision="DENY",reason="unknown_tool"} 1`
	if !strings.Contains(out, expectedDecision) {
		t.Errorf("metrics exposition missing expected decision line %q\nFull output:\n%s", expectedDecision, out)
	}

	// 2. Verify histogram recorded observation for DENY
	expectedHistCount := `agentgate_decision_duration_seconds_count{decision="DENY"} 1`
	if !strings.Contains(out, expectedHistCount) {
		t.Errorf("metrics exposition missing histogram count line %q\nFull output:\n%s", expectedHistCount, out)
	}
}

func TestAuthzServer_MetricsCredentialIssuanceFailure(t *testing.T) {
	adapter := setupTestAdapter(t)
	// Evaluator returns credential issuance failure
	mockEval := &mockEvaluator{decision: decision.Deny, reason: decision.ReasonCredentialIssuanceFailure}
	memStore := audit.NewMemoryStore()
	auditedSvc := audit.NewAuditedDecisionService(mockEval, memStore, audit.NewRedactor("", nil))

	m := metrics.NewMetrics("test-version")

	srv := NewServerWithOptions(adapter, auditedSvc, ServerOptions{
		DecisionsTotal:             m.DecisionsTotal,
		DecisionDurationSeconds:    m.DecisionDurationSeconds,
		CredentialIssuanceFailures: m.CredentialIssuanceFailures,
	})

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Method: "POST",
					Path:   "/",
					Body:   `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_status"}}`,
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
		t.Fatalf("expected PermissionDenied, got %v", resp.Status.Code)
	}

	var buf strings.Builder
	if err := m.Registry.WriteText(&buf); err != nil {
		t.Fatalf("WriteText error: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, `agentgate_credential_issuance_failures_total 1`) {
		t.Errorf("metrics exposition missing expected credential failure count:\n%s", out)
	}
}
