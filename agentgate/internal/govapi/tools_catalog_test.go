package govapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dynamisch-LLC/agentgate/internal/audit"
	"github.com/Dynamisch-LLC/agentgate/internal/auditevents"
	"github.com/Dynamisch-LLC/agentgate/internal/governanceintegration"
	"github.com/Dynamisch-LLC/agentgate/internal/policymanager"
	"github.com/Dynamisch-LLC/agentgate/internal/policystore"
	"github.com/Dynamisch-LLC/agentgate/internal/toolcatalog"
)

const catToken = "secret-token"

type catFixture struct {
	ts    *httptest.Server
	store *toolcatalog.MemoryStore
	cat   *toolcatalog.Catalog
	audit audit.Store
}

func newCatFixture(t *testing.T) *catFixture {
	t.Helper()
	ps := policystore.NewMemoryStore()
	mgr := policymanager.New(ps)
	gov := governanceintegration.NewGovernanceDecisionService(mgr)
	as := audit.NewMemoryStore()
	svc := audit.NewAuditedDecisionService(nil, as, audit.NewRedactor("salt", nil))

	store := toolcatalog.NewMemoryStore()
	cat, err := toolcatalog.NewCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandlerWithCatalog(mgr, catToken, gov, as, ToolCatalogService{Store: store, Catalog: cat, Listener: svc})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return &catFixture{ts: ts, store: store, cat: cat, audit: as}
}

func (f *catFixture) do(t *testing.T, method, path, token, body string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, f.ts.URL+path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

const putBody = `{"risk":"write","schema":{"type":"object","properties":{"id":{"type":"string"}}},"arguments":[{"name":"id","type":"string","required":true}],"description":"d"}`

func TestCatalogAPI_PutThenGetAndSnapshot(t *testing.T) {
	f := newCatFixture(t)
	resp, b := f.do(t, "PUT", "/api/v1/tools/be/do_thing", catToken, putBody)
	if resp.StatusCode != 200 {
		t.Fatalf("put: %d %s", resp.StatusCode, b)
	}
	resp, b = f.do(t, "GET", "/api/v1/tools/be/do_thing", catToken, "")
	if resp.StatusCode != 200 {
		t.Fatalf("get: %d %s", resp.StatusCode, b)
	}
	var v ToolDefinitionView
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if v.BackendID != "be" || v.ToolName != "do_thing" || v.Risk != "write" || len(v.Arguments) != 1 || !v.Arguments[0].Required || v.UpdatedBy == "" {
		t.Fatalf("bad view: %+v", v)
	}
	snap := f.cat.Current()
	if snap.ArgDecls["be/do_thing"] == nil {
		t.Fatal("snapshot missing argdecls")
	}
	found := false
	for _, r := range snap.Registry.List() {
		if r.ToolID.BackendID == "be" && r.ToolID.ToolName == "do_thing" && r.Known {
			found = true
		}
	}
	if !found {
		t.Fatal("snapshot registry missing tool")
	}

	resp, b = f.do(t, "GET", "/api/v1/tools", catToken, "")
	var list struct {
		Tools []ToolDefinitionView `json:"tools"`
	}
	_ = json.Unmarshal(b, &list)
	if resp.StatusCode != 200 || len(list.Tools) != 1 {
		t.Fatalf("list: %d %s", resp.StatusCode, b)
	}
}

func TestCatalogAPI_BodyCannotOverridePathIdentity(t *testing.T) {
	f := newCatFixture(t)
	body := `{"backend_id":"evil","tool_name":"evil","risk":"read","schema":{"type":"object"}}`
	resp, b := f.do(t, "PUT", "/api/v1/tools/be/t1", catToken, body)
	if resp.StatusCode != 200 && resp.StatusCode != 400 {
		t.Fatalf("unexpected %d %s", resp.StatusCode, b)
	}
	if _, err := f.store.Get(context.Background(), "evil", "evil"); err == nil {
		t.Fatal("body identity was honored")
	}
}

func TestCatalogAPI_InvalidInputs(t *testing.T) {
	f := newCatFixture(t)
	cases := map[string]struct{ path, body string }{
		"admin risk":    {"/api/v1/tools/be/t", `{"risk":"admin","schema":{"type":"object"}}`},
		"bad schema":    {"/api/v1/tools/be/t", `{"risk":"read","schema":[1]}`},
		"bad tool name": {"/api/v1/tools/be/bad%20name", `{"risk":"read","schema":{"type":"object"}}`},
		"bad json":      {"/api/v1/tools/be/t", `{`},
	}
	for name, c := range cases {
		resp, b := f.do(t, "PUT", c.path, catToken, c.body)
		if resp.StatusCode != 400 || !strings.Contains(string(b), `"INVALID_TOOL"`) {
			t.Errorf("%s: %d %s", name, resp.StatusCode, b)
		}
	}
	if l, _ := f.store.List(context.Background()); len(l) != 0 {
		t.Fatal("invalid input persisted")
	}
}

func TestCatalogAPI_RequiresAdminToken(t *testing.T) {
	f := newCatFixture(t)
	for _, c := range [][2]string{{"GET", "/api/v1/tools"}, {"GET", "/api/v1/tools/a/b"}, {"PUT", "/api/v1/tools/a/b"}, {"DELETE", "/api/v1/tools/a/b"}} {
		resp, _ := f.do(t, c[0], c[1], "", putBody)
		if resp.StatusCode != 401 {
			t.Errorf("%s %s: %d", c[0], c[1], resp.StatusCode)
		}
		resp, _ = f.do(t, c[0], c[1], "wrong", putBody)
		if resp.StatusCode != 401 {
			t.Errorf("%s %s wrong token: %d", c[0], c[1], resp.StatusCode)
		}
	}
}

func TestCatalogAPI_BodyTooLarge(t *testing.T) {
	f := newCatFixture(t)
	big := `{"risk":"read","schema":{"type":"object"},"description":"` + strings.Repeat("x", 2<<20) + `"}`
	resp, _ := f.do(t, "PUT", "/api/v1/tools/be/t", catToken, big)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestCatalogAPI_DeleteThenUnknown(t *testing.T) {
	f := newCatFixture(t)
	f.do(t, "PUT", "/api/v1/tools/be/t", catToken, putBody)
	resp, _ := f.do(t, "DELETE", "/api/v1/tools/be/t", catToken, "")
	if resp.StatusCode != 204 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	resp, _ = f.do(t, "GET", "/api/v1/tools/be/t", catToken, "")
	if resp.StatusCode != 404 {
		t.Fatalf("get after delete: %d", resp.StatusCode)
	}
	for _, r := range f.cat.Current().Registry.List() {
		if r.ToolID.ToolName == "t" {
			t.Fatal("snapshot still holds deleted tool")
		}
	}
	resp, _ = f.do(t, "GET", "/api/v1/tools/be/never", catToken, "")
	if resp.StatusCode != 404 {
		t.Fatalf("get missing: %d", resp.StatusCode)
	}
}

func TestCatalogAPI_MutationAuditRecorded(t *testing.T) {
	f := newCatFixture(t)
	f.do(t, "PUT", "/api/v1/tools/be/t", catToken, putBody)
	f.do(t, "DELETE", "/api/v1/tools/be/t", catToken, "")
	recs, err := f.audit.ListRecords(context.Background(), catalogAuditWorkspace, 10)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, r := range recs {
		if r.EventType == audit.EventTypeMutation {
			kinds = append(kinds, r.Reason)
		}
	}
	joined := strings.Join(kinds, ",")
	if !strings.Contains(joined, string(auditevents.ActionToolUpserted)) || !strings.Contains(joined, string(auditevents.ActionToolDeleted)) {
		t.Fatalf("missing mutation records: %v", kinds)
	}
}

type failingReloadStore struct {
	*toolcatalog.MemoryStore
	failList bool
}

func (s *failingReloadStore) List(ctx context.Context) ([]toolcatalog.ToolDefinition, error) {
	if s.failList {
		return nil, io.ErrUnexpectedEOF
	}
	return s.MemoryStore.List(ctx)
}

func TestCatalogAPI_ReloadFailureRollsBack(t *testing.T) {
	mgr := policymanager.New(policystore.NewMemoryStore())
	base := toolcatalog.NewMemoryStore()
	store := &failingReloadStore{MemoryStore: base}
	cat, _ := toolcatalog.NewCatalog(nil)
	h := NewHandlerWithCatalog(mgr, catToken, nil, audit.NewMemoryStore(), ToolCatalogService{Store: store, Catalog: cat})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	f := &catFixture{ts: ts}

	if resp, b := f.do(t, "PUT", "/api/v1/tools/be/t", catToken, putBody); resp.StatusCode != 200 {
		t.Fatalf("seed put: %d %s", resp.StatusCode, b)
	}
	store.failList = true
	upd := strings.Replace(putBody, `"risk":"write"`, `"risk":"destructive"`, 1)
	resp, _ := f.do(t, "PUT", "/api/v1/tools/be/t", catToken, upd)
	if resp.StatusCode != 500 {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
	got, _ := base.Get(context.Background(), "be", "t")
	if got == nil || got.Risk != "write" {
		t.Fatalf("previous definition not restored: %+v", got)
	}
	resp, _ = f.do(t, "PUT", "/api/v1/tools/be/new", catToken, putBody)
	if resp.StatusCode != 500 {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
	if _, err := base.Get(context.Background(), "be", "new"); err == nil {
		t.Fatal("new tool not rolled back")
	}
	resp, _ = f.do(t, "DELETE", "/api/v1/tools/be/t", catToken, "")
	if resp.StatusCode != 500 {
		t.Fatalf("expected 500 on delete, got %d", resp.StatusCode)
	}
	if _, err := base.Get(context.Background(), "be", "t"); err != nil {
		t.Fatal("deleted tool not restored")
	}
}

func TestCatalogAPI_WorkspaceToolsReadsCatalog(t *testing.T) {
	f := newCatFixture(t)
	f.do(t, "PUT", "/api/v1/tools/be/t", catToken, putBody)
	resp, b := f.do(t, "GET", "/api/v1/workspaces/ws-1/tools", catToken, "")
	var out ToolsResponse
	if err := json.Unmarshal(b, &out); err != nil || resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	if out.Source != "catalog" || len(out.Tools) != 1 || out.Tools[0].ToolID.ToolName != "t" || !out.Tools[0].Known || out.Tools[0].RegisteredFingerprint == "" {
		t.Fatalf("bad response: %+v", out)
	}
}

func TestCatalogAPI_NotConfiguredIs501(t *testing.T) {
	ts, _ := setupTestServer(catToken)
	defer ts.Close()
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/tools", nil)
	req.Header.Set("Authorization", "Bearer "+catToken)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("got %d", resp.StatusCode)
	}
}
