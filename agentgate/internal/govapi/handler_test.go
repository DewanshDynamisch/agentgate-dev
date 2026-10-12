package govapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Dynamisch-LLC/agentgate/internal/audit"
	"github.com/Dynamisch-LLC/agentgate/internal/fixturepolicy"
	"github.com/Dynamisch-LLC/agentgate/internal/governanceintegration"
	"github.com/Dynamisch-LLC/agentgate/internal/policymanager"
	"github.com/Dynamisch-LLC/agentgate/internal/policystore"
	"github.com/Dynamisch-LLC/agentgate/internal/toolregistry"
)

// setupTestServer wires nil for the audit store and tool registry, matching
// every caller in this file, which predate G7 Task C and don't exercise the
// read endpoints. Deliberately does NOT delegate to setupTestServerWithReadDeps
// — doing so would silently give every existing test a real audit
// store/registry it never asked for, and would make it impossible to test
// the handler's documented nil-is-safe (501, not panic) contract at all.
func setupTestServer(adminToken string) (*httptest.Server, *policymanager.Manager) {
	store := policystore.NewMemoryStore()
	mgr := policymanager.New(store)
	govIntegration := governanceintegration.NewGovernanceDecisionService(mgr)
	handler := NewHandler(mgr, adminToken, govIntegration, nil, nil)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	return httptest.NewServer(mux), mgr
}

// setupTestServerWithReadDeps additionally wires a real audit store and tool
// registry, for tests of the G7 Task C read endpoints (handleListAuditEvents,
// handleListTools) that setupTestServer's callers don't need.
func setupTestServerWithReadDeps(adminToken string) (*httptest.Server, *policymanager.Manager, audit.Store, *toolregistry.Registry) {
	store := policystore.NewMemoryStore()
	mgr := policymanager.New(store)
	govIntegration := governanceintegration.NewGovernanceDecisionService(mgr)
	auditStore := audit.NewMemoryStore()

	readFP, _ := toolregistry.FingerprintSchema([]byte(`{"type":"object"}`))
	toolReg, err := toolregistry.NewRegistry([]toolregistry.RegistryEntry{
		{
			ToolID:                toolregistry.ToolID{BackendID: "default", ToolName: "read_status"},
			Risk:                  toolregistry.RiskRead,
			RegisteredFingerprint: readFP,
		},
		{
			ToolID:                toolregistry.ToolID{BackendID: "default", ToolName: "admin_action"},
			Risk:                  toolregistry.RiskDestructive,
			RegisteredFingerprint: readFP,
		},
	})
	if err != nil {
		panic("setupTestServerWithReadDeps: invalid fixture registry: " + err.Error())
	}

	handler := NewHandler(mgr, adminToken, govIntegration, auditStore, toolReg)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	return httptest.NewServer(mux), mgr, auditStore, toolReg
}

func TestAdminAuth_Unauthorized(t *testing.T) {
	ts, _ := setupTestServer("secret-token")
	defer ts.Close()

	// 1. Missing auth
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workspaces/ws-1/policies", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for missing token, got %d", resp.StatusCode)
	}

	// 2. Invalid auth token
	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workspaces/ws-1/policies", nil)
	req2.Header.Set("Authorization", "Bearer wrong-token")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for invalid token, got %d", resp2.StatusCode)
	}
}

func TestGovAPI_Lifecycle(t *testing.T) {
	adminToken := "test-admin-key"
	ts, _ := setupTestServer(adminToken)
	defer ts.Close()

	authHeaders := func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
	}

	ws := "ws-gov"
	validCedar := "permit(principal, action, resource);"
	invalidCedar := "syntax error not cedar"

	// 1. Validate endpoint
	valBody, _ := json.Marshal(ValidateRequest{Content: validCedar})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workspaces/"+ws+"/policies/validate", bytes.NewReader(valBody))
	authHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("validate request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("validate expected 200, got %d", resp.StatusCode)
	}
	var valResp ValidateResponse
	if err := json.NewDecoder(resp.Body).Decode(&valResp); err != nil {
		t.Fatalf("decode validate response: %v", err)
	}
	if !valResp.Valid || valResp.Version == "" {
		t.Fatalf("expected valid=true with version, got %+v", valResp)
	}

	// Validate invalid Cedar
	valBadBody, _ := json.Marshal(ValidateRequest{Content: invalidCedar})
	reqBad, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workspaces/"+ws+"/policies/validate", bytes.NewReader(valBadBody))
	authHeaders(reqBad)
	respBad, err := http.DefaultClient.Do(reqBad)
	if err != nil {
		t.Fatalf("validate bad request failed: %v", err)
	}
	defer respBad.Body.Close()

	var valBadResp ValidateResponse
	if err := json.NewDecoder(respBad.Body).Decode(&valBadResp); err != nil {
		t.Fatalf("decode bad validate: %v", err)
	}
	if valBadResp.Valid || len(valBadResp.Errors) == 0 {
		t.Fatalf("expected valid=false with errors, got %+v", valBadResp)
	}

	// 2. Create Candidate
	createBody, _ := json.Marshal(CreateCandidateRequest{
		Content:     validCedar,
		Description: "Initial permit policy",
	})
	reqCreate, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workspaces/"+ws+"/policies", bytes.NewReader(createBody))
	authHeaders(reqCreate)
	respCreate, err := http.DefaultClient.Do(reqCreate)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer respCreate.Body.Close()

	if respCreate.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", respCreate.StatusCode)
	}
	var rec1 policystore.PolicyRecord
	if err := json.NewDecoder(respCreate.Body).Decode(&rec1); err != nil {
		t.Fatalf("decode created record: %v", err)
	}
	if rec1.State != policystore.StateCandidate || rec1.Version != valResp.Version {
		t.Fatalf("unexpected created candidate: %+v", rec1)
	}

	// 3. List Policies
	reqList, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workspaces/"+ws+"/policies", nil)
	authHeaders(reqList)
	respList, err := http.DefaultClient.Do(reqList)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer respList.Body.Close()

	if respList.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", respList.StatusCode)
	}
	var listResp ListPoliciesResponse
	if err := json.NewDecoder(respList.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listResp.Policies) != 1 {
		t.Fatalf("expected 1 policy in list, got %d", len(listResp.Policies))
	}

	// 4. Get Policy details
	reqGet, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workspaces/"+ws+"/policies/"+rec1.Version, nil)
	authHeaders(reqGet)
	respGet, err := http.DefaultClient.Do(reqGet)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	defer respGet.Body.Close()

	if respGet.StatusCode != http.StatusOK {
		t.Fatalf("get expected 200, got %d", respGet.StatusCode)
	}

	// 5. Activate Policy
	reqAct, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workspaces/"+ws+"/policies/"+rec1.Version+"/activate", nil)
	authHeaders(reqAct)
	respAct, err := http.DefaultClient.Do(reqAct)
	if err != nil {
		t.Fatalf("activate request: %v", err)
	}
	defer respAct.Body.Close()

	if respAct.StatusCode != http.StatusOK {
		t.Fatalf("activate expected 200, got %d", respAct.StatusCode)
	}
	var actResp ActivateResponse
	if err := json.NewDecoder(respAct.Body).Decode(&actResp); err != nil {
		t.Fatalf("decode activate response: %v", err)
	}
	if actResp.ActiveVersion != rec1.Version {
		t.Fatalf("expected active_version %s, got %s", rec1.Version, actResp.ActiveVersion)
	}

	// 6. Create second candidate (forbid)
	validCedar2 := "forbid(principal, action, resource);"
	createBody2, _ := json.Marshal(CreateCandidateRequest{
		Content:     validCedar2,
		Description: "Second policy forbid",
	})
	reqCreate2, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workspaces/"+ws+"/policies", bytes.NewReader(createBody2))
	authHeaders(reqCreate2)
	respCreate2, _ := http.DefaultClient.Do(reqCreate2)
	var rec2 policystore.PolicyRecord
	json.NewDecoder(respCreate2.Body).Decode(&rec2)
	respCreate2.Body.Close()

	// Activate second candidate
	reqAct2, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workspaces/"+ws+"/policies/"+rec2.Version+"/activate", nil)
	authHeaders(reqAct2)
	respAct2, _ := http.DefaultClient.Do(reqAct2)
	respAct2.Body.Close()

	// 7. Rollback to first policy
	rbBody, _ := json.Marshal(RollbackRequest{TargetVersion: rec1.Version})
	reqRb, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workspaces/"+ws+"/policies/rollback", bytes.NewReader(rbBody))
	authHeaders(reqRb)
	respRb, err := http.DefaultClient.Do(reqRb)
	if err != nil {
		t.Fatalf("rollback request: %v", err)
	}
	defer respRb.Body.Close()

	if respRb.StatusCode != http.StatusOK {
		t.Fatalf("rollback expected 200, got %d", respRb.StatusCode)
	}
	var rbResp RollbackResponse
	if err := json.NewDecoder(respRb.Body).Decode(&rbResp); err != nil {
		t.Fatalf("decode rollback: %v", err)
	}
	if rbResp.ActiveVersion != rec1.Version || rbResp.RolledBackFrom != rec2.Version {
		t.Fatalf("unexpected rollback response: %+v", rbResp)
	}

	// 8. Rollback to non-existent version -> 404
	rbBadBody, _ := json.Marshal(RollbackRequest{TargetVersion: "non-existent-hash"})
	reqRbBad, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workspaces/"+ws+"/policies/rollback", bytes.NewReader(rbBadBody))
	authHeaders(reqRbBad)
	respRbBad, err := http.DefaultClient.Do(reqRbBad)
	if err != nil {
		t.Fatalf("rollback bad: %v", err)
	}
	defer respRbBad.Body.Close()

	if respRbBad.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown rollback version, got %d", respRbBad.StatusCode)
	}
}

func TestGovAPI_DryRunCompare(t *testing.T) {
	adminToken := "test-admin-key"
	ts, _ := setupTestServer(adminToken)
	defer ts.Close()

	authHeaders := func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
	}

	ws := "ws-dryrun"

	// 1. Create candidate A and activate
	createBodyA, _ := json.Marshal(CreateCandidateRequest{
		Content:     fixturepolicy.CedarSource,
		Description: "policy A - reader can read",
	})
	reqA, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workspaces/"+ws+"/policies", bytes.NewReader(createBodyA))
	authHeaders(reqA)
	respA, err := http.DefaultClient.Do(reqA)
	if err != nil {
		t.Fatal(err)
	}
	var recA policystore.PolicyRecord
	json.NewDecoder(respA.Body).Decode(&recA)
	respA.Body.Close()

	reqAct, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workspaces/"+ws+"/policies/"+recA.Version+"/activate", nil)
	authHeaders(reqAct)
	respAct, err := http.DefaultClient.Do(reqAct)
	if err != nil {
		t.Fatal(err)
	}
	respAct.Body.Close()

	// 2. Create candidate B - forbid all
	createBodyB, _ := json.Marshal(CreateCandidateRequest{
		Content:     "forbid(principal, action, resource);",
		Description: "policy B - forbid all",
	})
	reqB, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workspaces/"+ws+"/policies", bytes.NewReader(createBodyB))
	authHeaders(reqB)
	respB, err := http.DefaultClient.Do(reqB)
	if err != nil {
		t.Fatal(err)
	}
	var recB policystore.PolicyRecord
	json.NewDecoder(respB.Body).Decode(&recB)
	respB.Body.Close()

	// 3. Dry-run compare against candidate B
	dryRunReqBody, _ := json.Marshal(DryRunCompareRequest{
		SampleRequests: []DryRunSampleRequest{
			{
				ExecutionID:    "exec-dry-1",
				PrincipalID:    "agent-1",
				PrincipalRoles: []string{fixturepolicy.RoleReader},
				BackendID:      "backend-1",
				ToolName:       "read-tool",
				Risk:           fixturepolicy.RiskRead,
			},
		},
	})
	reqDryRun, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workspaces/"+ws+"/policies/"+recB.Version+"/dryrun", bytes.NewReader(dryRunReqBody))
	authHeaders(reqDryRun)
	respDryRun, err := http.DefaultClient.Do(reqDryRun)
	if err != nil {
		t.Fatal(err)
	}
	defer respDryRun.Body.Close()

	if respDryRun.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for dryrun, got %d", respDryRun.StatusCode)
	}

	var dryRunResp DryRunCompareResponse
	if err := json.NewDecoder(respDryRun.Body).Decode(&dryRunResp); err != nil {
		t.Fatal(err)
	}

	if dryRunResp.CandidateVersion != recB.Version {
		t.Fatalf("candidate version mismatch: expected %s, got %s", recB.Version, dryRunResp.CandidateVersion)
	}
	if len(dryRunResp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(dryRunResp.Results))
	}
	res := dryRunResp.Results[0]
	if res.ActiveDecision != "ALLOW" {
		t.Fatalf("expected active decision ALLOW, got %s", res.ActiveDecision)
	}
	if res.CandidateDecision != "DENY" {
		t.Fatalf("expected candidate decision DENY, got %s", res.CandidateDecision)
	}
	if !res.Changed {
		t.Fatal("expected Changed == true")
	}

	// 4. Dry-run against nonexistent version -> 404
	reqDryRun404, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workspaces/"+ws+"/policies/nonexistent-hash/dryrun", bytes.NewReader(dryRunReqBody))
	authHeaders(reqDryRun404)
	resp404, err := http.DefaultClient.Do(reqDryRun404)
	if err != nil {
		t.Fatal(err)
	}
	defer resp404.Body.Close()
	if resp404.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown version, got %d", resp404.StatusCode)
	}
}
