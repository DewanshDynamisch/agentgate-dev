package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newSecuredServer(t *testing.T) (*Server, *testEnv) {
	t.Helper()
	e := newTestEnv(t)
	s := NewServerWithConfig(ServerConfig{RequireCredential: true, Verifier: e.verifier})
	return s, e
}

func callTool(h http.Handler, authz, tool, args string) *httptest.ResponseRecorder {
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + args + `}}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", bytes.NewBufferString(req))
	if authz != "" {
		r.Header.Set("Authorization", authz)
	}
	h.ServeHTTP(w, r)
	return w
}

func TestToolsCall_WithoutCredentialIs401AndStateUnchanged(t *testing.T) {
	s, _ := newSecuredServer(t)
	w := callTool(s.Handler(), "", "delete_ticket", `{"ticket_id":"TCK-1001"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	var resp jsonrpcResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil || resp.Error.Code != -32001 || !strings.HasPrefix(resp.Error.Message, "unauthorized: ") {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	s.mu.Lock()
	_, exists := s.tickets["TCK-1001"]
	n := len(s.invocations)
	s.mu.Unlock()
	if !exists || n != 0 {
		t.Fatalf("state mutated on failed auth: exists=%v invocations=%d", exists, n)
	}
}

func TestToolsCall_ValidCredentialRecordsIdentityAndRedactsAuthorization(t *testing.T) {
	s, e := newSecuredServer(t)
	c := validClaims(*e.clock)
	c["tool"], c["scope"] = "update_ticket_status", "tickets:write"
	tok := signWith(t, e.key, rs256Header(testKid), c)
	authz := "Bearer " + tok
	w := callTool(s.Handler(), authz, "update_ticket_status", `{"ticket_id":"TCK-1001","new_status":"resolved"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tickets["TCK-1001"].Status != "resolved" || len(s.invocations) != 1 {
		t.Fatalf("expected mutation and one invocation")
	}
	rec := s.invocations[0]
	if rec.CredentialSub != "user-1" || rec.CredentialAct != "agent-1" || rec.CredentialJTI != c["jti"] {
		t.Fatalf("credential fields not recorded: %+v", rec)
	}
	sum := sha256.Sum256([]byte(authz))
	want := "<redacted:sha256:" + hex.EncodeToString(sum[:])[:12] + ">"
	if rec.Headers["Authorization"] != want {
		t.Fatalf("authorization header = %q, want %q", rec.Headers["Authorization"], want)
	}
	b, _ := json.Marshal(rec)
	if strings.Contains(string(b), tok) {
		t.Fatal("raw token leaked into serialised record")
	}
}

func TestToolsCall_ReplayedCredentialRejected(t *testing.T) {
	s, e := newSecuredServer(t)
	tok := signWith(t, e.key, rs256Header(testKid), validClaims(*e.clock))
	if w := callTool(s.Handler(), "Bearer "+tok, "delete_ticket", `{"ticket_id":"TCK-1001"}`); w.Code != 200 {
		t.Fatalf("first call: %d", w.Code)
	}
	if w := callTool(s.Handler(), "Bearer "+tok, "delete_ticket", `{"ticket_id":"TCK-1002"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("replay expected 401, got %d", w.Code)
	}
	s.mu.Lock()
	_, exists := s.tickets["TCK-1002"]
	s.mu.Unlock()
	if !exists {
		t.Fatal("replay mutated state")
	}
}

func TestInitializeAndToolsList_NoCredentialRequired(t *testing.T) {
	s, _ := newSecuredServer(t)
	for _, m := range []string{"initialize", "tools/list"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"`+m+`"}`))
		s.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", m, w.Code)
		}
	}
}

func TestRequireCredentialWithoutVerifierFailsClosed(t *testing.T) {
	s := NewServerWithConfig(ServerConfig{RequireCredential: true})
	w := callTool(s.Handler(), "Bearer x.y.z", "list_tickets", `{}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestListTickets_ReturnsSeedData(t *testing.T) {
	s := NewServer()
	handler := s.Handler()

	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_tickets","arguments":{}}}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", bytes.NewBufferString(req))
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp jsonrpcResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

func TestUpdateTicketStatus_MutatesState(t *testing.T) {
	s := NewServer()
	handler := s.Handler()

	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"update_ticket_status","arguments":{"ticket_id":"TCK-1001","new_status":"resolved"}}}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", bytes.NewBufferString(req))
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	s.mu.Lock()
	got := s.tickets["TCK-1001"].Status
	s.mu.Unlock()
	if got != "resolved" {
		t.Fatalf("expected ticket status to be updated to resolved, got %q", got)
	}
}

func TestUpdateTicketStatus_UnknownTicketErrors(t *testing.T) {
	s := NewServer()
	handler := s.Handler()

	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"update_ticket_status","arguments":{"ticket_id":"NO-SUCH-TICKET","new_status":"resolved"}}}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", bytes.NewBufferString(req))
	handler.ServeHTTP(w, r)

	var resp jsonrpcResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil {
		t.Fatalf("expected an error for an unknown ticket id")
	}
}

func TestDeleteTicket_RemovesFromState(t *testing.T) {
	s := NewServer()
	handler := s.Handler()

	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete_ticket","arguments":{"ticket_id":"TCK-1001"}}}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", bytes.NewBufferString(req))
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	s.mu.Lock()
	_, stillExists := s.tickets["TCK-1001"]
	s.mu.Unlock()
	if stillExists {
		t.Fatalf("expected TCK-1001 to be removed after delete_ticket")
	}
}

func TestReset_RestoresSeedData(t *testing.T) {
	s := NewServer()

	s.mu.Lock()
	delete(s.tickets, "TCK-1001")
	s.mu.Unlock()

	s.reset()

	s.mu.Lock()
	_, exists := s.tickets["TCK-1001"]
	invocationCount := len(s.invocations)
	s.mu.Unlock()

	if !exists {
		t.Fatalf("expected reset to restore TCK-1001")
	}
	if invocationCount != 0 {
		t.Fatalf("expected reset to clear invocation history, got %d entries", invocationCount)
	}
}

func TestUnknownTool_ReturnsError(t *testing.T) {
	s := NewServer()
	handler := s.Handler()

	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"not_a_real_tool","arguments":{}}}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", bytes.NewBufferString(req))
	handler.ServeHTTP(w, r)

	var resp jsonrpcResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil {
		t.Fatalf("expected an error for an unknown tool")
	}
}

func TestLastCredential_DisabledAndEnabled(t *testing.T) {
	// Disabled case (default)
	sDisabled := NewServer()
	hDisabled := sDisabled.Handler()
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("GET", "/_demo/last-credential", nil)
	hDisabled.ServeHTTP(w1, r1)
	if w1.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when ExposeLastCredential is false, got %d", w1.Code)
	}

	// Enabled case
	sEnabled := NewServerWithConfig(ServerConfig{ExposeLastCredential: true})
	hEnabled := sEnabled.Handler()

	// Initial check before any call
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/_demo/last-credential", nil)
	hEnabled.ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 when ExposeLastCredential is true, got %d", w2.Code)
	}
	var res2 map[string]string
	if err := json.Unmarshal(w2.Body.Bytes(), &res2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res2["credential"] != "" {
		t.Fatalf("expected empty credential initially, got %q", res2["credential"])
	}

	// Tool call with Authorization header
	rawToken := "Bearer test-credential-jwt-value"
	callTool(hEnabled, rawToken, "list_tickets", "{}")

	w3 := httptest.NewRecorder()
	r3 := httptest.NewRequest("GET", "/_demo/last-credential", nil)
	hEnabled.ServeHTTP(w3, r3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200 after tool call, got %d", w3.Code)
	}
	var res3 map[string]string
	if err := json.Unmarshal(w3.Body.Bytes(), &res3); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res3["credential"] != rawToken {
		t.Fatalf("expected %q, got %q", rawToken, res3["credential"])
	}

	// Reset clears last credential
	wReset := httptest.NewRecorder()
	rReset := httptest.NewRequest("POST", "/_demo/reset", nil)
	hEnabled.ServeHTTP(wReset, rReset)

	w4 := httptest.NewRecorder()
	r4 := httptest.NewRequest("GET", "/_demo/last-credential", nil)
	hEnabled.ServeHTTP(w4, r4)
	var res4 map[string]string
	if err := json.Unmarshal(w4.Body.Bytes(), &res4); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res4["credential"] != "" {
		t.Fatalf("expected empty credential after reset, got %q", res4["credential"])
	}
}
