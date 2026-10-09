package audit

import (
	"context"
	"errors"
	"testing"

	"github.com/Dynamisch-LLC/agentgate/internal/decision"
	"github.com/Dynamisch-LLC/agentgate/internal/metrics"
)

type failingAppendStore struct {
	Store
}

func (f *failingAppendStore) AppendDecision(_ context.Context, _ DecisionRecord) (*StoredRecord, error) {
	return nil, errors.New("simulated append failure")
}

func (f *failingAppendStore) GetLatestRecord(_ context.Context, _ string) (*StoredRecord, error) {
	return nil, nil
}

func TestAuditedDecisionService_AppendFailureMetric(t *testing.T) {
	counter := metrics.NewCounter("agentgate_audit_append_failures_total", "help")
	store := &failingAppendStore{Store: NewMemoryStore()}
	svc := NewAuditedDecisionService(nil, store, nil)
	svc.SetMetrics(counter)

	req := decision.Request{
		ExecutionID: "exec-fail-1",
		WorkspaceID: "ws-test",
		Identity: decision.Identity{
			AgentID: "agent-1",
			Roles:   []string{"admin"},
		},
		Tool: decision.ToolRef{
			BackendID: "backend-1",
			Name:      "test_tool",
		},
		Classification: decision.ToolClassification{
			Known: true,
			Risk:  "read",
		},
	}

	res, err := svc.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Fail-closed to DENY
	if res.Decision != decision.Deny {
		t.Errorf("decision = %v, want DENY", res.Decision)
	}

	if got := counter.Value(); got != 1 {
		t.Errorf("counter.Value() = %v, want 1", got)
	}
}
