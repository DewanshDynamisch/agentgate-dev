package govapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dynamisch-LLC/agentgate/internal/audit"
	"github.com/Dynamisch-LLC/agentgate/internal/auditevents"
	"github.com/Dynamisch-LLC/agentgate/internal/fixturepolicy"
	"github.com/Dynamisch-LLC/agentgate/internal/governanceintegration"
	"github.com/Dynamisch-LLC/agentgate/internal/oidcverify"
	"github.com/Dynamisch-LLC/agentgate/internal/policymanager"
	"github.com/Dynamisch-LLC/agentgate/internal/policystore"
	"github.com/Dynamisch-LLC/agentgate/internal/toolcatalog"
)

type fakeVerifier struct {
	principals map[string]oidcverify.Principal
	err        error
}

func (f fakeVerifier) Verify(_ context.Context, tok string) (oidcverify.Principal, error) {
	if f.err != nil {
		return oidcverify.Principal{}, f.err
	}
	p, ok := f.principals[tok]
	if !ok {
		return oidcverify.Principal{}, oidcverify.ErrInvalidToken
	}
	return p, nil
}

func oidcFake() fakeVerifier {
	return fakeVerifier{principals: map[string]oidcverify.Principal{
		"admin-tok":  {Subject: "sub-1", Username: "alice", Roles: []string{"agentgate-admin"}},
		"nouser-tok": {Subject: "sub-2", Roles: []string{"agentgate-admin"}},
		"viewer-tok": {Subject: "sub-3", Username: "bob", Roles: []string{"viewer"}},
	}}
}

func doReq(t *testing.T, h http.HandlerFunc, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

func errCode(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var e ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &e); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return e.Error.Code
}

func TestOIDCAuth_Matrix(t *testing.T) {
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }
	h := AdminAuthMiddlewareWith(NewOIDCAuthenticator(oidcFake(), "agentgate-admin"), ok)

	if rr := doReq(t, h, map[string]string{"Authorization": "Bearer admin-tok"}); rr.Code != 200 {
		t.Fatalf("admin token: got %d", rr.Code)
	}
	rr := doReq(t, h, map[string]string{"Authorization": "Bearer viewer-tok"})
	if rr.Code != 403 || errCode(t, rr) != "FORBIDDEN" {
		t.Fatalf("viewer: got %d %s", rr.Code, rr.Body)
	}
	rr = doReq(t, h, nil)
	if rr.Code != 401 || errCode(t, rr) != "UNAUTHORIZED" {
		t.Fatalf("no token: got %d", rr.Code)
	}
	// static key header must not be accepted in OIDC mode
	rr = doReq(t, h, map[string]string{"X-AgentGate-Admin-Key": "admin-tok"})
	if rr.Code != 401 {
		t.Fatalf("admin-key header in oidc mode: got %d", rr.Code)
	}
	rr = doReq(t, h, map[string]string{"Authorization": "Bearer agentgate-admin-secret-dev"})
	if rr.Code != 401 {
		t.Fatalf("static token in oidc mode: got %d", rr.Code)
	}
}

func TestOIDCAuth_IdPOutage503(t *testing.T) {
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }
	h := AdminAuthMiddlewareWith(NewOIDCAuthenticator(fakeVerifier{err: oidcverify.ErrUnavailable}, "agentgate-admin"), ok)
	rr := doReq(t, h, map[string]string{"Authorization": "Bearer admin-tok"})
	if rr.Code != 503 || errCode(t, rr) != "AUTH_UNAVAILABLE" {
		t.Fatalf("got %d %s", rr.Code, rr.Body)
	}
}

func TestStaticAuth_PrincipalInContext(t *testing.T) {
	var got AdminPrincipal
	var found bool
	h := AdminAuthMiddleware("tok", func(w http.ResponseWriter, r *http.Request) {
		got, found = PrincipalFromContext(r.Context())
	})
	doReq(t, h, map[string]string{"X-AgentGate-Admin-Key": "tok"})
	if !found || got.Method != "static" || got.Username == "" {
		t.Fatalf("principal = %+v found=%v", got, found)
	}
}

func TestActorName(t *testing.T) {
	if (AdminPrincipal{Subject: "s", Username: "u"}).ActorName() != "u" {
		t.Fatal("username preferred")
	}
	if (AdminPrincipal{Subject: "s"}).ActorName() != "s" {
		t.Fatal("subject fallback")
	}
}

func TestOIDCMutations_AttributePrincipal(t *testing.T) {
	store := policystore.NewMemoryStore()
	as := audit.NewMemoryStore()
	svc := audit.NewAuditedDecisionService(nil, as, audit.NewRedactor("salt", nil))
	mgr := policymanager.NewWithListener(store, svc)
	h := NewHandler(mgr, "unused", governanceintegration.NewGovernanceDecisionService(mgr), as, nil)
	h.SetAuthenticator(NewOIDCAuthenticator(oidcFake(), "agentgate-admin"))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	do := func(method, path, body, tok string) *http.Response {
		req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	b, _ := json.Marshal(CreateCandidateRequest{Content: fixturepolicy.CedarSource, Description: "d1"})
	resp := do("POST", "/api/v1/workspaces/default/policies", string(b), "admin-tok")
	if resp.StatusCode != 201 {
		t.Fatalf("create v1: %d", resp.StatusCode)
	}
	var rec1 policystore.PolicyRecord
	_ = json.NewDecoder(resp.Body).Decode(&rec1)

	resp = do("POST", "/api/v1/workspaces/default/policies/"+rec1.Version+"/activate", "", "nouser-tok")
	if resp.StatusCode != 200 {
		t.Fatalf("activate v1: %d", resp.StatusCode)
	}
	if resp := do("POST", "/api/v1/workspaces/default/policies/"+rec1.Version+"/activate", "", "viewer-tok"); resp.StatusCode != 403 {
		t.Fatalf("viewer activate: %d", resp.StatusCode)
	}

	b2, _ := json.Marshal(CreateCandidateRequest{Content: "permit(principal, action, resource);", Description: "d2"})
	resp = do("POST", "/api/v1/workspaces/default/policies", string(b2), "admin-tok")
	if resp.StatusCode != 201 {
		t.Fatalf("create v2: %d", resp.StatusCode)
	}
	var rec2 policystore.PolicyRecord
	_ = json.NewDecoder(resp.Body).Decode(&rec2)

	resp = do("POST", "/api/v1/workspaces/default/policies/"+rec2.Version+"/activate", "", "admin-tok")
	if resp.StatusCode != 200 {
		t.Fatalf("activate v2: %d", resp.StatusCode)
	}

	rb, _ := json.Marshal(RollbackRequest{TargetVersion: rec1.Version})
	resp = do("POST", "/api/v1/workspaces/default/policies/rollback", string(rb), "nouser-tok")
	if resp.StatusCode != 200 {
		t.Fatalf("rollback: %d", resp.StatusCode)
	}

	recs, err := as.ListRecords(context.Background(), "default", 50)
	if err != nil {
		t.Fatal(err)
	}
	// ListRecords returns newest first; reverse to chronological order.
	for i, j := 0, len(recs)-1; i < j; i, j = i+1, j-1 {
		recs[i], recs[j] = recs[j], recs[i]
	}
	var creates, activates, rollbacks []string
	for _, r := range recs {
		switch r.Reason {
		case string(auditevents.ActionCreateCandidate):
			creates = append(creates, r.PrincipalAgentID)
		case string(auditevents.ActionActivate):
			activates = append(activates, r.PrincipalAgentID)
		case string(auditevents.ActionRollback):
			rollbacks = append(rollbacks, r.PrincipalAgentID)
		}
	}
	if len(creates) != 2 || creates[0] != "alice" || creates[1] != "alice" {
		t.Errorf("create actor = %v", creates)
	}
	if len(activates) != 2 || activates[0] != "sub-2" || activates[1] != "alice" {
		t.Errorf("activate actor = %v", activates)
	}
	if len(rollbacks) != 1 || rollbacks[0] != "sub-2" {
		t.Errorf("rollback actor = %v", rollbacks)
	}
}

func TestOIDCCatalog_AttributesPrincipal(t *testing.T) {
	ps := policystore.NewMemoryStore()
	mgr := policymanager.New(ps)
	as := audit.NewMemoryStore()
	svc := audit.NewAuditedDecisionService(nil, as, audit.NewRedactor("salt", nil))
	store := toolcatalog.NewMemoryStore()
	cat, _ := toolcatalog.NewCatalog(nil)
	h := NewHandlerWithCatalog(mgr, "unused", nil, as, ToolCatalogService{Store: store, Catalog: cat, Listener: svc})
	h.SetAuthenticator(NewOIDCAuthenticator(oidcFake(), "agentgate-admin"))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// 1. PUT tool with admin-tok (username = "alice")
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/tools/be/t", strings.NewReader(`{"risk":"read","schema":{"type":"object"}}`))
	req.Header.Set("Authorization", "Bearer admin-tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("put: %v %v", err, resp)
	}
	d, err := store.Get(context.Background(), "be", "t")
	if err != nil || d.UpdatedBy != "alice" {
		t.Fatalf("UpdatedBy = %+v err=%v", d, err)
	}

	// 2. DELETE tool with nouser-tok (subject = "sub-2")
	delReq, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/tools/be/t", nil)
	delReq.Header.Set("Authorization", "Bearer nouser-tok")
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil || delResp.StatusCode != 204 {
		t.Fatalf("delete: %v %v", err, delResp)
	}

	recs, _ := as.ListRecords(context.Background(), catalogAuditWorkspace, 10)
	var putActor, deleteActor string
	for _, r := range recs {
		if r.EventType == audit.EventTypeMutation {
			if r.Reason == string(auditevents.ActionToolUpserted) {
				putActor = r.PrincipalAgentID
			}
			if r.Reason == string(auditevents.ActionToolDeleted) {
				deleteActor = r.PrincipalAgentID
			}
		}
	}
	if putActor != "alice" {
		t.Errorf("put mutation actor = %q, want alice", putActor)
	}
	if deleteActor != "sub-2" {
		t.Errorf("delete mutation actor = %q, want sub-2", deleteActor)
	}
}
