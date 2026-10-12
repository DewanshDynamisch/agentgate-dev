package v1security_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Dynamisch-LLC/agentgate/internal/audit"
	"github.com/jackc/pgx/v5"
)

// TestV1_01_AuthorizedRead verifies an authorized read tool call succeeds through the gateway,
// reaches the backend, and records the correct agent client subject.
func TestV1_01_AuthorizedRead(t *testing.T) {
	cfg := requireLiveEnvironment(t)
	token := cfg.FetchToken(t, "agent-reader", cfg.ReaderSecret)

	cfg.ResetBackend(t)
	stateBefore := cfg.GetBackendState(t)

	status, body, err := cfg.CallGatewayTool(t, token, "list_tickets", nil, nil)
	if err != nil {
		t.Fatalf("gateway call failed: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", status, string(body))
	}

	stateAfter := cfg.GetBackendState(t)
	if stateAfter.Count != stateBefore.Count+1 {
		t.Fatalf("expected backend invocation count %d, got %d", stateBefore.Count+1, stateAfter.Count)
	}

	last := stateAfter.Invocations[len(stateAfter.Invocations)-1]
	if last.ToolName != "list_tickets" {
		t.Fatalf("expected tool_name list_tickets, got %s", last.ToolName)
	}
	tokenSub := extractJWTSubject(token)
	if last.CredentialSub != tokenSub && last.CredentialSub != "agent-reader" {
		t.Fatalf("expected credential_sub %q or agent-reader, got %s", tokenSub, last.CredentialSub)
	}
	if !strings.Contains(string(body), "tickets") {
		t.Fatalf("expected response body to contain tickets, got: %s", string(body))
	}
}

// TestV1_02_InboundTokenNeverReachesBackend proves that the inbound Keycloak token never reaches
// the backend; the backend only receives AgentGate's downstream minted credential.
func TestV1_02_InboundTokenNeverReachesBackend(t *testing.T) {
	cfg := requireLiveEnvironment(t)
	token := cfg.FetchToken(t, "agent-reader", cfg.ReaderSecret)

	status, _, err := cfg.CallGatewayTool(t, token, "list_tickets", nil, nil)
	if err != nil || status != http.StatusOK {
		t.Fatalf("gateway call failed with status %d: %v", status, err)
	}

	state := cfg.GetBackendState(t)
	if len(state.Invocations) == 0 {
		t.Fatal("no backend invocations found")
	}
	last := state.Invocations[len(state.Invocations)-1]

	authHeader := last.Headers["Authorization"]
	if authHeader == "" {
		for k, v := range last.Headers {
			if strings.EqualFold(k, "Authorization") {
				authHeader = v
				break
			}
		}
	}
	if !strings.HasPrefix(authHeader, "<redacted:sha256:") {
		t.Fatalf("backend authorization header not redacted as expected: %s", authHeader)
	}

	inboundSum := sha256.Sum256([]byte("Bearer " + token))
	inboundHash := "<redacted:sha256:" + hex.EncodeToString(inboundSum[:])[:12] + ">"

	if authHeader == inboundHash {
		t.Fatalf("CRITICAL SECURITY DEFECT: backend received inbound Keycloak token! hash=%s", authHeader)
	}

	rawStateBytes, _ := json.Marshal(state)
	if strings.Contains(string(rawStateBytes), token) {
		t.Fatalf("CRITICAL SECURITY DEFECT: raw inbound token leaked into backend state!")
	}
}

// TestV1_03_CredentialReplay verifies that capturing a downstream minted credential and
// replaying it directly to the backend returns 401 (JTI replay protection).
func TestV1_03_CredentialReplay(t *testing.T) {
	cfg := requireLiveEnvironment(t)
	token := cfg.FetchToken(t, "agent-reader", cfg.ReaderSecret)

	// Execute call to mint credential
	status, _, err := cfg.CallGatewayTool(t, token, "list_tickets", nil, nil)
	if err != nil || status != http.StatusOK {
		t.Fatalf("initial gateway call failed: %d (%v)", status, err)
	}

	capturedCred := cfg.GetLastCredential(t)
	if capturedCred == "" {
		t.Fatal("no credential captured by support-desk-mcp")
	}

	stateBefore := cfg.GetBackendState(t)

	// Replay directly to backend
	replayStatus, replayBody, err := cfg.CallBackendDirect(t, capturedCred, "list_tickets", nil)
	if err != nil {
		t.Fatalf("replay direct call failed: %v", err)
	}
	if replayStatus != http.StatusUnauthorized {
		t.Fatalf("expected status 401 on credential replay, got %d: %s", replayStatus, string(replayBody))
	}

	replayBodyStr := string(replayBody)
	if !strings.Contains(replayBodyStr, "replayed") {
		t.Fatalf("expected 401 body to indicate replayed, got: %s", replayBodyStr)
	}
	if strings.Contains(replayBodyStr, "expired") {
		t.Fatalf("expected 401 body NOT to indicate expired, got: %s", replayBodyStr)
	}

	stateAfter := cfg.GetBackendState(t)
	if stateAfter.Count != stateBefore.Count {
		t.Fatalf("backend state mutated on replayed credential: count before=%d after=%d", stateBefore.Count, stateAfter.Count)
	}
}

// TestV1_04_AudienceConfusion verifies that sending a Keycloak token directly to the backend
// fails with 401 (wrong issuer/audience/signature).
func TestV1_04_AudienceConfusion(t *testing.T) {
	cfg := requireLiveEnvironment(t)
	token := cfg.FetchToken(t, "agent-reader", cfg.ReaderSecret)

	stateBefore := cfg.GetBackendState(t)

	status, body, err := cfg.CallBackendDirect(t, token, "list_tickets", nil)
	if err != nil {
		t.Fatalf("direct backend call failed: %v", err)
	}
	if status != http.StatusUnauthorized {
		t.Fatalf("expected status 401 on Keycloak token sent to backend, got %d: %s", status, string(body))
	}

	stateAfter := cfg.GetBackendState(t)
	if stateAfter.Count != stateBefore.Count {
		t.Fatalf("backend state mutated on audience confusion call: before=%d after=%d", stateBefore.Count, stateAfter.Count)
	}
}

// TestV1_05_CredentialExpiry verifies that captured credentials expire and return 401 after 70s.
// Marked with t.Parallel() so it does not serialize or block the rest of the test suite.
func TestV1_05_CredentialExpiry(t *testing.T) {
	t.Parallel()

	cfg := requireLiveEnvironment(t)
	token := cfg.FetchToken(t, "agent-reader", cfg.ReaderSecret)

	status, _, err := cfg.CallGatewayTool(t, token, "list_tickets", nil, nil)
	if err != nil || status != http.StatusOK {
		t.Fatalf("gateway call failed: %d (%v)", status, err)
	}

	cred := cfg.GetLastCredential(t)
	if cred == "" {
		t.Fatal("failed to capture credential for expiry test")
	}

	parts := strings.Split(cred, ".")
	if len(parts) != 3 {
		t.Fatalf("malformed credential: %s", cred)
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	var claims struct {
		Iat int64 `json:"iat"`
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		t.Fatalf("failed to unmarshal claims: %v", err)
	}
	if claims.Exp-claims.Iat > 60 {
		t.Fatalf("credential lifetime > 60s: %d", claims.Exp-claims.Iat)
	}

	t.Log("TestV1_05_CredentialExpiry: waiting 71 seconds for credential expiry...")
	time.Sleep(71 * time.Second)

	expTime := time.Unix(claims.Exp, 0)
	targetTime := expTime.Add(11 * time.Second)
	if time.Now().Before(targetTime) {
		extra := targetTime.Sub(time.Now())
		t.Logf("Clock skew detected. Sleeping an extra %v to ensure now > exp + 10s...", extra)
		time.Sleep(extra)
	}

	if !time.Now().After(expTime.Add(10 * time.Second)) {
		t.Fatalf("expected time.Now() > exp + 10s, got now=%v, exp=%v", time.Now().Format(time.RFC3339), expTime.Format(time.RFC3339))
	}

	expStatus, expBody, err := cfg.CallBackendDirect(t, cred, "list_tickets", nil)
	if err != nil {
		t.Fatalf("direct call with expired credential failed: %v", err)
	}
	if expStatus != http.StatusUnauthorized {
		t.Fatalf("expected status 401 for expired credential after 71s, got %d: %s", expStatus, string(expBody))
	}

	expBodyStr := string(expBody)
	if !strings.Contains(expBodyStr, "expired") {
		t.Fatalf("expected 401 body to indicate expired, got: %s", expBodyStr)
	}
	if strings.Contains(expBodyStr, "replayed") {
		t.Fatalf("expected 401 body NOT to indicate replayed, got: %s", expBodyStr)
	}

	t.Log("TestV1_05_CredentialExpiry: passed (received 401 as expected)")
}

// TestV1_06_DirectBackendAccessWithoutCredential verifies that unauthenticated direct backend access
// returns 401 and that the backend port is not published to the host.
func TestV1_06_DirectBackendAccessWithoutCredential(t *testing.T) {
	cfg := requireLiveEnvironment(t)

	status, body, err := cfg.CallBackendDirect(t, "", "list_tickets", nil)
	if err != nil {
		t.Fatalf("direct backend call failed: %v", err)
	}
	if status != http.StatusUnauthorized {
		t.Fatalf("expected status 401 for unauthenticated direct backend access, got %d: %s", status, string(body))
	}

	// Verify host connection to port 9200 fails (port not published)
	conn, dialErr := net.DialTimeout("tcp", cfg.HostBackendAddr, 1500*time.Millisecond)
	if dialErr == nil {
		conn.Close()
		t.Fatalf("SECURITY VIOLATION: backend port is published on %s!", cfg.HostBackendAddr)
	}

	// Directly inspect container port bindings to ensure port 9200 is not published to host
	if path, err := exec.LookPath("docker"); err == nil {
		cmd := exec.Command(path, "port", "prod-support-desk-mcp", "9200")
		out, err := cmd.CombinedOutput()
		if err == nil && len(strings.TrimSpace(string(out))) > 0 {
			t.Fatalf("SECURITY VIOLATION: docker port reports port 9200 published to host: %s", string(out))
		}
	}
}

// TestV1_07_IdentitySpoofing verifies that client headers like x-agent-id are ignored,
// the client's actual role (reader) is enforced to forbid delete_ticket, and audit principal reflects
// the verified subject, not the spoofed header.
func TestV1_07_IdentitySpoofing(t *testing.T) {
	cfg := requireLiveEnvironment(t)
	readerToken := cfg.FetchToken(t, "agent-reader", cfg.ReaderSecret)
	adminToken := cfg.FetchAdminToken(t)

	status, body, err := cfg.CallGatewayTool(t, readerToken, "delete_ticket", map[string]any{"ticket_id": "TCK-1001"}, map[string]string{
		"x-agent-id": "agent-admin",
	})
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if status != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for reader attempting delete_ticket with spoofed identity, got %d: %s", status, string(body))
	}

	state := cfg.GetBackendState(t)
	found := false
	for _, tk := range state.Tickets {
		if tk.ID == "TCK-1001" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ticket TCK-1001 was deleted despite forbidden status!")
	}

	events, err := cfg.GetAuditEvents(t, adminToken, "default")
	if err != nil || len(events) == 0 {
		t.Fatalf("failed to retrieve audit events: %v", err)
	}
	var deleteEvent *AuditEventView
	for i := range events {
		if events[i].ToolName == "delete_ticket" {
			deleteEvent = &events[i]
			break
		}
	}
	if deleteEvent == nil {
		t.Fatalf("audit event for delete_ticket not found")
	}
	tokenSub := extractJWTSubject(readerToken)
	if deleteEvent.PrincipalAgentID != tokenSub && deleteEvent.PrincipalAgentID != "agent-reader" {
		t.Fatalf("SECURITY DEFECT: audit principal recorded spoofed identity %q instead of verified %q", deleteEvent.PrincipalAgentID, tokenSub)
	}
	if deleteEvent.PrincipalAgentID == "agent-admin" {
		t.Fatalf("SECURITY DEFECT: audit principal was spoofed to agent-admin!")
	}
	if !strings.EqualFold(deleteEvent.Decision, "deny") {
		t.Fatalf("expected deny decision in audit log, got %s", deleteEvent.Decision)
	}
}

// TestV1_08_NoToken verifies that calling the gateway without any token returns 401
// and leaves backend state unchanged.
func TestV1_08_NoToken(t *testing.T) {
	cfg := requireLiveEnvironment(t)
	stateBefore := cfg.GetBackendState(t)

	status, body, err := cfg.CallGatewayTool(t, "", "list_tickets", nil, nil)
	if err != nil {
		t.Fatalf("gateway call failed: %v", err)
	}
	if status != http.StatusUnauthorized {
		t.Fatalf("expected status 401 for no token, got %d: %s", status, string(body))
	}

	stateAfter := cfg.GetBackendState(t)
	if stateAfter.Count != stateBefore.Count {
		t.Fatalf("backend count mutated on no-token call: before=%d after=%d", stateBefore.Count, stateAfter.Count)
	}
}

// TestV1_09_ForgedToken verifies that a self-signed JWT with an untrusted RSA key is rejected
// with 401 by the gateway.
func TestV1_09_ForgedToken(t *testing.T) {
	cfg := requireLiveEnvironment(t)
	forged := cfg.GenerateForgedToken(t)

	stateBefore := cfg.GetBackendState(t)

	status, body, err := cfg.CallGatewayTool(t, forged, "list_tickets", nil, nil)
	if err != nil {
		t.Fatalf("gateway call failed: %v", err)
	}
	if status != http.StatusUnauthorized {
		t.Fatalf("expected status 401 for forged token, got %d: %s", status, string(body))
	}

	stateAfter := cfg.GetBackendState(t)
	if stateAfter.Count != stateBefore.Count {
		t.Fatalf("backend count mutated on forged token call: before=%d after=%d", stateBefore.Count, stateAfter.Count)
	}
}

// TestV1_10_UnknownTool verifies that calling a nonexistent tool returns 403 unknown_tool
// and leaves backend state unchanged.
func TestV1_10_UnknownTool(t *testing.T) {
	cfg := requireLiveEnvironment(t)
	token := cfg.FetchToken(t, "agent-reader", cfg.ReaderSecret)
	adminToken := cfg.FetchAdminToken(t)
	stateBefore := cfg.GetBackendState(t)

	status, body, err := cfg.CallGatewayTool(t, token, "nonexistent_tool_99999", nil, nil)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if status != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for unknown tool, got %d: %s", status, string(body))
	}

	stateAfter := cfg.GetBackendState(t)
	if stateAfter.Count != stateBefore.Count {
		t.Fatalf("backend count mutated on unknown tool: before=%d after=%d", stateBefore.Count, stateAfter.Count)
	}

	events, err := cfg.GetAuditEvents(t, adminToken, "default")
	if err != nil || len(events) == 0 {
		t.Fatalf("failed to retrieve audit events: %v", err)
	}
	var unknownEvent *AuditEventView
	for i := range events {
		if events[i].ToolName == "nonexistent_tool_99999" {
			unknownEvent = &events[i]
			break
		}
	}
	if unknownEvent == nil {
		t.Fatalf("audit event for unknown tool not found")
	}
	if unknownEvent.Reason != "unknown_tool" {
		t.Fatalf("expected audit reason unknown_tool, got %s", unknownEvent.Reason)
	}
	if !strings.EqualFold(unknownEvent.Decision, "deny") {
		t.Fatalf("expected deny decision in audit log, got %s", unknownEvent.Decision)
	}
}

// TestV1_11_MalformedBody verifies that a malformed JSON-RPC body is rejected with 400 or 403,
// and leaves backend state unchanged.
func TestV1_11_MalformedBody(t *testing.T) {
	cfg := requireLiveEnvironment(t)
	token := cfg.FetchToken(t, "agent-reader", cfg.ReaderSecret)
	stateBefore := cfg.GetBackendState(t)

	malformed := []byte(`{"jsonrpc": "2.0", "method": tools/call, unclosed json`)
	status, body, err := cfg.CallGatewayRaw(t, token, malformed, nil)
	if err != nil {
		t.Fatalf("raw call failed: %v", err)
	}
	if status != http.StatusBadRequest && status != http.StatusForbidden {
		t.Fatalf("expected 400 or 403 for malformed JSON, got %d: %s", status, string(body))
	}

	stateAfter := cfg.GetBackendState(t)
	if stateAfter.Count != stateBefore.Count {
		t.Fatalf("backend count mutated on malformed body: before=%d after=%d", stateBefore.Count, stateAfter.Count)
	}
}

// TestV1_12_OversizedBody verifies that an oversized 2 MiB request body is rejected with 413 or 403,
// and leaves backend state unchanged.
func TestV1_12_OversizedBody(t *testing.T) {
	cfg := requireLiveEnvironment(t)
	token := cfg.FetchToken(t, "agent-reader", cfg.ReaderSecret)
	stateBefore := cfg.GetBackendState(t)

	padding := strings.Repeat("X", 2*1024*1024)
	oversized := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_tickets","arguments":{"data":"%s"}}}`, padding))

	status, body, err := cfg.CallGatewayRaw(t, token, oversized, nil)
	if err != nil {
		t.Fatalf("raw call failed: %v", err)
	}
	if status != http.StatusRequestEntityTooLarge && status != http.StatusForbidden {
		t.Fatalf("expected 413 or 403 for 2 MiB body, got %d: %s", status, string(body))
	}

	stateAfter := cfg.GetBackendState(t)
	if stateAfter.Count != stateBefore.Count {
		t.Fatalf("backend count mutated on oversized body: before=%d after=%d", stateBefore.Count, stateAfter.Count)
	}
}

// TestV1_13_AdminAPIAuth verifies Admin API access control:
// no token -> 401; agent token without admin role -> 403; dev static token -> 401; admin token -> 200.
func TestV1_13_AdminAPIAuth(t *testing.T) {
	cfg := requireLiveEnvironment(t)
	readerToken := cfg.FetchToken(t, "agent-reader", cfg.ReaderSecret)
	adminToken := cfg.FetchAdminToken(t)

	// 1. Without token -> 401
	s1, _, err := cfg.CallAdminAPI(t, "", http.MethodGet, "/api/v1/tools", nil)
	if err != nil || s1 != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d (%v)", s1, err)
	}

	// 2. With agent token -> 403
	s2, _, err := cfg.CallAdminAPI(t, readerToken, http.MethodGet, "/api/v1/tools", nil)
	if err != nil || s2 != http.StatusForbidden {
		t.Fatalf("expected 403 with reader token, got %d (%v)", s2, err)
	}

	// 3. With static dev token -> 401
	s3, _, err := cfg.CallAdminAPI(t, "agentgate-admin-secret-dev", http.MethodGet, "/api/v1/tools", nil)
	if err != nil || s3 != http.StatusUnauthorized {
		t.Fatalf("expected 401 with static dev token in prod mode, got %d (%v)", s3, err)
	}

	// 4. With valid admin token -> 200
	s4, _, err := cfg.CallAdminAPI(t, adminToken, http.MethodGet, "/api/v1/tools", nil)
	if err != nil || s4 != http.StatusOK {
		t.Fatalf("expected 200 with admin token, got %d (%v)", s4, err)
	}
}

// TestV1_14_PolicyRollbackAbuse verifies that activating a nonexistent policy version
// returns 404 or 400 and preserves the currently active policy version.
func TestV1_14_PolicyRollbackAbuse(t *testing.T) {
	cfg := requireLiveEnvironment(t)
	adminToken := cfg.FetchAdminToken(t)

	sGet1, bGet1, err := cfg.CallAdminAPI(t, adminToken, http.MethodGet, "/api/v1/workspaces/default/policies", nil)
	if err != nil || sGet1 != http.StatusOK {
		t.Fatalf("failed to list policies: %d (%v)", sGet1, err)
	}

	sAct, bAct, err := cfg.CallAdminAPI(t, adminToken, http.MethodPost, "/api/v1/workspaces/default/policies/v-nonexistent-99999/activate", nil)
	if err != nil {
		t.Fatalf("activate call failed: %v", err)
	}
	if sAct != http.StatusNotFound && sAct != http.StatusBadRequest {
		t.Fatalf("expected 404 or 400 for nonexistent policy activation, got %d: %s", sAct, string(bAct))
	}

	sGet2, bGet2, err := cfg.CallAdminAPI(t, adminToken, http.MethodGet, "/api/v1/workspaces/default/policies", nil)
	if err != nil || sGet2 != http.StatusOK {
		t.Fatalf("failed to list policies after activation attempt: %d (%v)", sGet2, err)
	}

	if string(bGet1) != string(bGet2) {
		t.Fatalf("policy state modified after failed activation!")
	}
}

// TestV1_15_AuditTamper verifies database trigger prevents UPDATE on audit_events
// and the cryptographic hash chain verification stays valid (OK).
func TestV1_15_AuditTamper(t *testing.T) {
	cfg := requireLiveEnvironment(t)
	ctx := context.Background()

	if cfg.DatabaseURL != "" {
		conn, err := pgx.Connect(ctx, cfg.DatabaseURL)
		if err != nil {
			t.Fatalf("failed to connect to database: %v", err)
		}
		defer conn.Close(ctx)

		_, err = conn.Exec(ctx, "UPDATE audit_events SET decision = 'allow' WHERE sequence_number = (SELECT MIN(sequence_number) FROM audit_events)")
		if err == nil {
			t.Fatalf("SECURITY VIOLATION: direct UPDATE on audit_events succeeded without error!")
		}
		if !strings.Contains(err.Error(), "audit_events is immutable and append-only") &&
			!strings.Contains(err.Error(), "permission denied for table audit_events") {
			t.Fatalf("expected trigger exception or permission denied, got: %v", err)
		}

		store, err := audit.NewPostgresStore(ctx, cfg.DatabaseURL)
		if err == nil {
			defer store.Close()
			res, err := audit.NewChainVerifier().VerifyWorkspace(ctx, store, "default")
			if err != nil {
				t.Fatalf("verify-audit failed: %v", err)
			}
			if !res.Valid {
				t.Fatalf("audit chain broken: %+v", res)
			}
		}
	}

	// Also verify via container CLI command if available
	if path, err := exec.LookPath("docker"); err == nil {
		cmd := exec.Command(path, "exec", cfg.ContainerName, "agentgate", "verify-audit", "--workspace", "default")
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "OK") {
			t.Logf("container verify-audit output: %s (err: %v)", string(out), err)
		}
	}
}

// TestV1_16_AgentGateOutage verifies that when the core AgentGate engine is stopped,
// requests fail closed (non-200), and when restarted it recovers to 200 without losing state.
func TestV1_16_AgentGateOutage(t *testing.T) {
	cfg := requireLiveEnvironment(t)
	token := cfg.FetchToken(t, "agent-reader", cfg.ReaderSecret)

	stateBefore := cfg.GetBackendState(t)

	// Stop AgentGate container
	t.Logf("stopping %s to simulate outage...", cfg.ContainerName)
	cfg.StopAgentGate(t)

	// Gateway call must fail (non-200)
	status, body, err := cfg.CallGatewayTool(t, token, "list_tickets", nil, nil)
	if err == nil && status == http.StatusOK {
		t.Fatalf("SECURITY DEFECT: gateway returned 200 while agentgate was stopped!")
	}
	t.Logf("gateway returned expected non-200 during outage (status: %d, err: %v, body: %s)", status, err, string(body))

	stateDuring := cfg.GetBackendState(t)
	if stateDuring.Count != stateBefore.Count {
		t.Fatalf("backend count mutated while agentgate was down: before=%d during=%d", stateBefore.Count, stateDuring.Count)
	}

	// Restart AgentGate container
	t.Logf("starting %s...", cfg.ContainerName)
	cfg.StartAgentGate(t)
	cfg.WaitForAgentGate(t, 30*time.Second)

	// Recovered: should succeed with 200
	statusAfter, bodyAfter, err := cfg.CallGatewayTool(t, token, "list_tickets", nil, nil)
	if err != nil || statusAfter != http.StatusOK {
		t.Fatalf("gateway failed to recover after agentgate restart: %d (%v) body: %s", statusAfter, err, string(bodyAfter))
	}

	stateAfter := cfg.GetBackendState(t)
	if stateAfter.Count != stateBefore.Count+1 {
		t.Fatalf("expected backend count %d after recovery, got %d", stateBefore.Count+1, stateAfter.Count)
	}
}

// TestV1_17_CatalogRace simulates concurrent runtime tool risk classification toggling
// while sending requests, asserting that every ALLOW decision was recorded with tool_risk == 'write'
// and no call was ever ALLOWed while classified as 'destructive'.
func TestV1_17_CatalogRace(t *testing.T) {
	cfg := requireLiveEnvironment(t)
	adminToken := cfg.FetchAdminToken(t)
	agentAdminToken := cfg.FetchToken(t, "agent-admin", cfg.AdminSecret)

	// Ensure cleanup restores 'write' risk
	defer func() {
		_ = cfg.SetToolRisk(t, adminToken, "default", "update_ticket_status", "write")
		_ = cfg.SetToolRisk(t, adminToken, "mcp-probe", "update_ticket_status", "write")
	}()

	stopToggles := make(chan struct{})
	var toggleWg sync.WaitGroup
	toggleErrors := make(chan error, 40)

	// Pace catalog writes below the configured admin API rate limit while requests run.
	toggleWg.Add(1)
	go func() {
		defer toggleWg.Done()
		risks := []string{"destructive", "write"}
		for i := 0; i < 20; i++ {
			select {
			case <-stopToggles:
				return
			default:
				risk := risks[i%2]
				if err := cfg.SetToolRisk(t, adminToken, "default", "update_ticket_status", risk); err != nil {
					toggleErrors <- fmt.Errorf("set default backend risk to %q: %w", risk, err)
				}
				if err := cfg.SetToolRisk(t, adminToken, "mcp-probe", "update_ticket_status", risk); err != nil {
					toggleErrors <- fmt.Errorf("set mcp-probe backend risk to %q: %w", risk, err)
				}
				time.Sleep(300 * time.Millisecond)
			}
		}
	}()

	// Fire 100 calls from agent-admin
	var callWg sync.WaitGroup
	callWg.Add(100)
	for i := 0; i < 100; i++ {
		go func(id int) {
			defer callWg.Done()
			_, _, _ = cfg.CallGatewayTool(t, agentAdminToken, "update_ticket_status", map[string]any{
				"ticket_id":  "TCK-1002",
				"new_status": "in_progress",
			}, nil)
		}(i)
		time.Sleep(50 * time.Millisecond)
	}

	callWg.Wait()
	close(stopToggles)
	toggleWg.Wait()
	close(toggleErrors)
	for err := range toggleErrors {
		t.Errorf("catalog risk toggle failed: %v", err)
	}

	// Restore write risk before verification
	if err := cfg.SetToolRisk(t, adminToken, "default", "update_ticket_status", "write"); err != nil {
		t.Errorf("restore default backend risk: %v", err)
	}
	if err := cfg.SetToolRisk(t, adminToken, "mcp-probe", "update_ticket_status", "write"); err != nil {
		t.Errorf("restore mcp-probe backend risk: %v", err)
	}

	events, err := cfg.GetAuditEvents(t, adminToken, "default")
	if err != nil {
		t.Fatalf("failed to retrieve audit events: %v", err)
	}

	allowCount, denyCount := 0, 0
	for _, e := range events {
		if e.ToolName != "update_ticket_status" || e.EventType == "mutation" {
			continue
		}
		if strings.EqualFold(e.Decision, "allow") {
			allowCount++
			if e.ToolRisk == "destructive" {
				t.Fatalf("CRITICAL SECURITY DEFECT: ALLOW decision recorded with tool_risk == 'destructive'!")
			}
			if e.ToolRisk != "write" {
				t.Fatalf("ALLOW decision recorded with unexpected tool_risk: %s", e.ToolRisk)
			}
		} else if strings.EqualFold(e.Decision, "deny") {
			denyCount++
			if e.ToolRisk != "destructive" && !strings.Contains(e.Reason, "policy_deny") && !strings.Contains(e.Reason, "no_matching_policy") {
				t.Fatalf("DENY decision recorded with unexpected tool_risk/reason: risk=%s reason=%s", e.ToolRisk, e.Reason)
			}
		}
	}

	t.Logf("TestV1_17_CatalogRace completed: %d ALLOW, %d DENY decisions audited", allowCount, denyCount)
	if allowCount == 0 {
		t.Fatalf("expected at least one ALLOW decision during catalog race, got 0")
	}
	if denyCount == 0 {
		t.Fatalf("expected at least one DENY decision during catalog race, got 0")
	}
	if allowCount+denyCount < 100 {
		t.Fatalf("expected at least 100 decisions during catalog race, got %d (%d allow, %d deny)", allowCount+denyCount, allowCount, denyCount)
	}
}
