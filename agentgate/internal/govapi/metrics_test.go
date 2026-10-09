package govapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dynamisch-LLC/agentgate/internal/governanceintegration"
	"github.com/Dynamisch-LLC/agentgate/internal/metrics"
	"github.com/Dynamisch-LLC/agentgate/internal/policymanager"
	"github.com/Dynamisch-LLC/agentgate/internal/policystore"
)

func TestGovAPI_AdminRequestsMetric(t *testing.T) {
	store := policystore.NewMemoryStore()
	mgr := policymanager.New(store)
	govIntegration := governanceintegration.NewGovernanceDecisionService(mgr)
	handler := NewHandler(mgr, "test-admin-token", govIntegration, nil, nil)

	m := metrics.NewMetrics("test-version")
	handler.SetMetrics(m.AdminRequestsTotal)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// 1. Unauthorized request -> 401
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workspaces/default/policies", nil)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %v, want 401", resp.StatusCode)
	}

	// 2. Authorized request -> 200
	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workspaces/default/policies", nil)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	resp2, err := ts.Client().Do(req2)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("status = %v, want 200", resp2.StatusCode)
	}

	var buf strings.Builder
	if err := m.Registry.WriteText(&buf); err != nil {
		t.Fatalf("WriteText failed: %v", err)
	}
	out := buf.String()

	// Verify both 401 and 200 were recorded with route pattern
	expected401 := `agentgate_admin_requests_total{route="GET /api/v1/workspaces/{workspace_id}/policies",status="401"} 1`
	if !strings.Contains(out, expected401) {
		t.Errorf("metrics exposition missing line %q\nFull output:\n%s", expected401, out)
	}

	expected200 := `agentgate_admin_requests_total{route="GET /api/v1/workspaces/{workspace_id}/policies",status="200"} 1`
	if !strings.Contains(out, expected200) {
		t.Errorf("metrics exposition missing line %q\nFull output:\n%s", expected200, out)
	}
}
