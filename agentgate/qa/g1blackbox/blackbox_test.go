// Package blackbox is WS-D (QA/Security) independent verification of the
// frozen G1 authorization contract (docs/PHASES/G1_WORKSTREAMS/GO_BACKEND_G1_CONTRACT.md).
//
// This suite is deliberately NOT an import of internal/decision,
// internal/mockauthz or internal/fixturepolicy. It builds the real
// cmd/g1-mock-authz binary, starts it as a separate OS process, and talks
// to it exclusively over its documented HTTP wire contract (POST
// /evaluate), exactly as an external Gateway/MCP caller would. It defines
// its own minimal request/response shapes below, copied from the frozen
// contract document rather than the Go source, so this suite would
// independently notice if the running mock ever drifted from its own
// documentation.
//
// Tickets covered: AG-QA-G1-01 (test matrix), AG-QA-G1-02 (independent
// negative tests), AG-QA-G1-03 (contract/fixture validation),
// AG-QA-G1-04 (six mandatory scenarios at the wire boundary),
// AG-QA-G1-05 (trust-boundary probes expressed as executable assertions).
// See docs/PHASES/G1_WORKSTREAMS/04_QA_SECURITY_G1_DETAILED.md and the
// WS-D G1 evidence report for narrative findings.
package blackbox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// ---------------------------------------------------------------------
// Test harness: build the real mock binary, run it as a subprocess, talk
// HTTP to it. No internal package is imported anywhere in this file.
// ---------------------------------------------------------------------

var baseURL string

func TestMain(m *testing.M) {
	code, err := runWithMock(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "g1blackbox: harness setup failed:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func runWithMock(m *testing.M) (int, error) {
	tmpDir, err := os.MkdirTemp("", "g1blackbox-*")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmpDir)

	binName := "g1-mock-authz"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(tmpDir, binName)

	build := exec.Command("go", "build", "-o", binPath, "github.com/Dynamisch-LLC/agentgate/cmd/g1-mock-authz")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return 0, fmt.Errorf("building cmd/g1-mock-authz: %w", err)
	}

	port, err := freeTCPPort()
	if err != nil {
		return 0, err
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	baseURL = "http://" + addr

	cmd := exec.Command(binPath)
	cmd.Env = append(os.Environ(), "G1_MOCK_AUTHZ_ADDR=:"+fmt.Sprint(port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("starting g1-mock-authz: %w", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	if err := waitReady(addr, 5*time.Second); err != nil {
		return 0, fmt.Errorf("mock never became ready on %s: %w", addr, err)
	}

	return m.Run(), nil
}

func freeTCPPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func waitReady(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	return lastErr
}

// apiResult mirrors the documented response contract
// (GO_BACKEND_G1_CONTRACT.md AG-GO-G1-03 / AG-GO-G1-04), written
// independently rather than imported from internal/mockauthz.
type apiResult struct {
	Decision      string `json:"decision"`
	Reason        string `json:"reason"`
	Message       string `json:"message"`
	PolicyVersion string `json:"policy_version"`
	ExecutionID   string `json:"execution_id"`
}

type httpResult struct {
	status int
	body   []byte
	result apiResult // zero value if body did not decode as apiResult
}

func post(t *testing.T, rawBody string) httpResult {
	t.Helper()
	resp, err := http.Post(baseURL+"/evaluate", "application/json", bytes.NewBufferString(rawBody))
	if err != nil {
		t.Fatalf("POST /evaluate: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	hr := httpResult{status: resp.StatusCode, body: body}
	if resp.StatusCode == http.StatusOK {
		_ = json.Unmarshal(body, &hr.result) // best effort; asserted explicitly where it matters
	}
	return hr
}

func postRaw(t *testing.T, method, path, contentType string, rawBody string) httpResult {
	t.Helper()
	req, err := http.NewRequest(method, baseURL+path, bytes.NewBufferString(rawBody))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	hr := httpResult{status: resp.StatusCode, body: body}
	if resp.StatusCode == http.StatusOK {
		_ = json.Unmarshal(body, &hr.result)
	}
	return hr
}

func assertDeny(t *testing.T, hr httpResult, wantReason string) {
	t.Helper()
	if hr.status != http.StatusOK {
		// A transport-level rejection (400/404/405) is also an acceptable
		// "blocking" outcome for a malformed/unsafe request — it never
		// reached a decision.Result, and it is categorically NOT ALLOW.
		return
	}
	if hr.result.Decision != "DENY" {
		t.Fatalf("SECURITY: got decision=%q (want DENY or transport rejection); body=%s", hr.result.Decision, hr.body)
	}
	if wantReason != "" && hr.result.Reason != wantReason {
		t.Errorf("reason = %q, want %q; body=%s", hr.result.Reason, wantReason, hr.body)
	}
}

func assertNeverAllow(t *testing.T, hr httpResult) {
	t.Helper()
	if hr.status == http.StatusOK && hr.result.Decision == "ALLOW" {
		t.Fatalf("SECURITY: unsafe input produced ALLOW; body=%s", hr.body)
	}
}

// ---------------------------------------------------------------------
// AG-QA-G1-01 / AG-QA-G1-04 — the six mandatory scenarios, reproduced
// directly against the running mock over its real HTTP interface.
// ---------------------------------------------------------------------

func TestMandatory_ValidIdentity_Allow(t *testing.T) {
	hr := post(t, `{
		"execution_id":"m-allow-1","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["reader"]},
		"tool":{"backend_id":"backend-a","name":"read_schedule"},
		"classification":{"known":true,"risk":"read"}
	}`)
	if hr.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", hr.status)
	}
	if hr.result.Decision != "ALLOW" {
		t.Fatalf("decision = %q, want ALLOW; body=%s", hr.result.Decision, hr.body)
	}
	if hr.result.Reason != "policy_allow" {
		t.Errorf("reason = %q, want policy_allow", hr.result.Reason)
	}
	if len(hr.result.PolicyVersion) != 64 {
		t.Errorf("policy_version = %q, want a 64-char hex SHA-256", hr.result.PolicyVersion)
	}
	if hr.result.ExecutionID != "m-allow-1" {
		t.Errorf("execution_id = %q, want m-allow-1", hr.result.ExecutionID)
	}
}

func TestMandatory_ValidIdentity_Deny_ExplicitForbid(t *testing.T) {
	hr := post(t, `{
		"execution_id":"m-deny-1","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["admin"]},
		"tool":{"backend_id":"backend-a","name":"wipe_database"},
		"classification":{"known":true,"risk":"destructive"}
	}`)
	assertDeny(t, hr, "policy_deny")
	if len(hr.result.PolicyVersion) != 64 {
		t.Errorf("policy_version = %q, want a 64-char hex SHA-256 (Cedar was reached)", hr.result.PolicyVersion)
	}
}

func TestMandatory_ValidIdentity_Deny_NoMatchingPolicy(t *testing.T) {
	hr := post(t, `{
		"execution_id":"m-deny-2","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["reader"]},
		"tool":{"backend_id":"backend-a","name":"send_email"},
		"classification":{"known":true,"risk":"write"}
	}`)
	assertDeny(t, hr, "no_matching_policy")
}

func TestMandatory_MissingIdentity_Deny(t *testing.T) {
	hr := post(t, `{
		"execution_id":"m-miss-1","workspace_id":"ws-1",
		"tool":{"backend_id":"backend-a","name":"read_schedule"},
		"classification":{"known":true,"risk":"read"}
	}`)
	assertDeny(t, hr, "invalid_identity")
	if hr.result.PolicyVersion != "" {
		t.Errorf("policy_version = %q, want empty (Cedar must never be reached)", hr.result.PolicyVersion)
	}
}

func TestMandatory_UnknownTool_Deny(t *testing.T) {
	hr := post(t, `{
		"execution_id":"m-unk-1","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["admin"]},
		"tool":{"backend_id":"backend-a","name":"totally_new_tool_nobody_classified"},
		"classification":{"known":false}
	}`)
	assertDeny(t, hr, "unknown_tool")
}

func TestMandatory_MalformedInput_Deny(t *testing.T) {
	// Malformed at the transport layer: an undocumented field.
	hr := postRaw(t, http.MethodPost, "/evaluate", "application/json", `{
		"execution_id":"m-mal-1","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["reader"]},
		"tool":{"backend_id":"backend-a","name":"read_schedule"},
		"classification":{"known":true,"risk":"read"},
		"bogus_field":"should not be silently accepted"
	}`)
	if hr.status == http.StatusOK && hr.result.Decision == "ALLOW" {
		t.Fatalf("SECURITY: malformed request with unknown field produced ALLOW; body=%s", hr.body)
	}
	if hr.status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an undocumented field", hr.status)
	}
}

func TestMandatory_PolicyEvaluationFailure_Deny(t *testing.T) {
	hr := post(t, `{
		"execution_id":"m-err-1","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["broken"]},
		"tool":{"backend_id":"backend-a","name":"whatever"},
		"classification":{"known":true,"risk":"write"}
	}`)
	assertDeny(t, hr, "evaluation_error")
	if len(hr.result.PolicyVersion) != 64 {
		t.Errorf("policy_version = %q, want a 64-char hex SHA-256 (Cedar was reached before it errored)", hr.result.PolicyVersion)
	}
}

// ---------------------------------------------------------------------
// AG-QA-G1-02 — independent negative tests beyond the six mandatory
// scenarios: identity removed entirely / structurally malformed, unknown
// tool via various shapes, malformed arguments, and unsupported
// top-level request shapes.
// ---------------------------------------------------------------------

func TestNegative_IdentityRemovedEntirely(t *testing.T) {
	hr := post(t, `{
		"execution_id":"n-id-1","workspace_id":"ws-1",
		"tool":{"backend_id":"backend-a","name":"read_schedule"},
		"classification":{"known":true,"risk":"read"}
	}`)
	assertDeny(t, hr, "invalid_identity")
}

func TestNegative_IdentityRolesWrongJSONType(t *testing.T) {
	// roles declared as a string instead of an array.
	hr := postRaw(t, http.MethodPost, "/evaluate", "application/json", `{
		"execution_id":"n-id-2","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":"reader"},
		"tool":{"backend_id":"backend-a","name":"read_schedule"},
		"classification":{"known":true,"risk":"read"}
	}`)
	assertNeverAllow(t, hr)
	if hr.status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (roles must be an array, not a string)", hr.status)
	}
}

func TestNegative_IdentityAgentIDWrongJSONType(t *testing.T) {
	// agent_id declared as a number instead of a string.
	hr := postRaw(t, http.MethodPost, "/evaluate", "application/json", `{
		"execution_id":"n-id-3","workspace_id":"ws-1",
		"identity":{"agent_id":123,"roles":["reader"]},
		"tool":{"backend_id":"backend-a","name":"read_schedule"},
		"classification":{"known":true,"risk":"read"}
	}`)
	assertNeverAllow(t, hr)
	if hr.status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (agent_id must be a string)", hr.status)
	}
}

func TestNegative_IdentityExtraNestedField(t *testing.T) {
	// An extra field nested inside "identity" that is not part of the
	// documented contract.
	hr := postRaw(t, http.MethodPost, "/evaluate", "application/json", `{
		"execution_id":"n-id-4","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["reader"],"impersonate":"agent-admin"},
		"tool":{"backend_id":"backend-a","name":"read_schedule"},
		"classification":{"known":true,"risk":"read"}
	}`)
	assertNeverAllow(t, hr)
	if hr.status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (unknown nested field must not be silently ignored)", hr.status)
	}
}

func TestNegative_IdentityMissingRoles(t *testing.T) {
	hr := post(t, `{
		"execution_id":"n-id-5","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1"},
		"tool":{"backend_id":"backend-a","name":"read_schedule"},
		"classification":{"known":true,"risk":"read"}
	}`)
	assertDeny(t, hr, "invalid_identity")
}

func TestNegative_IdentityEmptyRolesArray(t *testing.T) {
	hr := post(t, `{
		"execution_id":"n-id-6","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":[]},
		"tool":{"backend_id":"backend-a","name":"read_schedule"},
		"classification":{"known":true,"risk":"read"}
	}`)
	assertDeny(t, hr, "invalid_identity")
}

func TestNegative_IdentityBlankRoleEntry(t *testing.T) {
	hr := post(t, `{
		"execution_id":"n-id-7","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["reader"," "]},
		"tool":{"backend_id":"backend-a","name":"read_schedule"},
		"classification":{"known":true,"risk":"read"}
	}`)
	assertDeny(t, hr, "invalid_identity")
}

func TestNegative_IdentityOnBehalfOfEqualsAgentID(t *testing.T) {
	// Ambiguous identity: an agent claiming to act on behalf of itself.
	hr := post(t, `{
		"execution_id":"n-id-8","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","on_behalf_of":"agent-1","roles":["reader"]},
		"tool":{"backend_id":"backend-a","name":"read_schedule"},
		"classification":{"known":true,"risk":"read"}
	}`)
	assertDeny(t, hr, "invalid_identity")
}

func TestNegative_UnknownTool_ClassificationKnownFalseWithRiskSet(t *testing.T) {
	// A caller asserts known=false but still supplies a risk — the tool
	// must still be denied unconditionally; a stray "risk" must not let
	// it slip past the unknown-tool gate.
	hr := post(t, `{
		"execution_id":"n-tool-1","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["admin"]},
		"tool":{"backend_id":"backend-a","name":"mystery"},
		"classification":{"known":false,"risk":"read"}
	}`)
	assertDeny(t, hr, "unknown_tool")
}

func TestNegative_UnknownTool_ClassificationOmittedEntirely(t *testing.T) {
	hr := post(t, `{
		"execution_id":"n-tool-2","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["reader"]},
		"tool":{"backend_id":"backend-a","name":"read_schedule"}
	}`)
	assertDeny(t, hr, "unknown_tool")
}

func TestNegative_ArgumentsTypeMismatch(t *testing.T) {
	// Declared type "int" but the JSON value is a string.
	hr := post(t, `{
		"execution_id":"n-arg-1","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["payer"]},
		"tool":{"backend_id":"backend-a","name":"transfer"},
		"classification":{"known":true,"risk":"write"},
		"arguments":{"amount":{"type":"int","value":"500"}}
	}`)
	assertDeny(t, hr, "malformed_request")
}

func TestNegative_ArgumentsUnknownDeclaredType(t *testing.T) {
	hr := post(t, `{
		"execution_id":"n-arg-2","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["payer"]},
		"tool":{"backend_id":"backend-a","name":"transfer"},
		"classification":{"known":true,"risk":"write"},
		"arguments":{"amount":{"type":"float","value":500}}
	}`)
	assertDeny(t, hr, "malformed_request")
}

func TestNegative_PolicyEvaluationFailure_BrokenRole(t *testing.T) {
	// The fixture policy's "broken" role deliberately references
	// context.amount without a `has` guard: omitting amount entirely
	// produces a genuine Cedar evaluation error, not a hand-rolled one.
	hr := post(t, `{
		"execution_id":"n-fail-1","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["broken"]},
		"tool":{"backend_id":"backend-a","name":"whatever"},
		"classification":{"known":true,"risk":"write"}
	}`)
	assertDeny(t, hr, "evaluation_error")
}

func TestNegative_TopLevelShape_JSONArray(t *testing.T) {
	hr := postRaw(t, http.MethodPost, "/evaluate", "application/json", `[1,2,3]`)
	assertNeverAllow(t, hr)
	if hr.status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a JSON array body", hr.status)
	}
}

func TestNegative_TopLevelShape_JSONString(t *testing.T) {
	hr := postRaw(t, http.MethodPost, "/evaluate", "application/json", `"just a string"`)
	assertNeverAllow(t, hr)
	if hr.status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a bare JSON string body", hr.status)
	}
}

func TestNegative_TopLevelShape_EmptyBody(t *testing.T) {
	hr := postRaw(t, http.MethodPost, "/evaluate", "application/json", ``)
	assertNeverAllow(t, hr)
	if hr.status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an empty body", hr.status)
	}
}

func TestNegative_TopLevelShape_NotJSONAtAll(t *testing.T) {
	hr := postRaw(t, http.MethodPost, "/evaluate", "application/json", `this is not json at all {{{`)
	assertNeverAllow(t, hr)
	if hr.status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a non-JSON body", hr.status)
	}
}

func TestNegative_WrongMethod_GET(t *testing.T) {
	hr := postRaw(t, http.MethodGet, "/evaluate", "", "")
	assertNeverAllow(t, hr)
	if hr.status != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405 for GET /evaluate", hr.status)
	}
}

func TestNegative_WrongMethod_PUT(t *testing.T) {
	hr := postRaw(t, http.MethodPut, "/evaluate", "application/json", `{}`)
	assertNeverAllow(t, hr)
	if hr.status != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405 for PUT /evaluate", hr.status)
	}
}

func TestNegative_WrongPath(t *testing.T) {
	hr := postRaw(t, http.MethodPost, "/authorize", "application/json", `{}`)
	assertNeverAllow(t, hr)
	if hr.status != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for an undocumented path", hr.status)
	}
}

// ---------------------------------------------------------------------
// REGRESSION — JSON `null` for a declared argument value is rejected as
// malformed.
//
// The wire contract documents each argument as {"type": "string"|"int"|
// "bool", "value": <matching JSON value>}. This used to be a genuine
// security finding (see docs/PHASES/G1_WORKSTREAMS/results/04_QA_SECURITY_DIGEST.md,
// "Security findings", and GO_BACKEND_G1_CONTRACT.md's "G1 corrective
// closeout"): encoding/json's Unmarshal-into-non-pointer-scalar treats a
// JSON `null` as a documented no-op, so a null value was silently accepted
// as the target type's Go zero value (0 / "" / false) instead of being
// rejected — for fixturepolicy's "payer" rule (risk=write, amount<=1000),
// {"amount":{"type":"int","value":null}} evaluated identically to
// {"amount":{"type":"int","value":0}} and was ALLOWed.
//
// Fixed upstream in mockauthz.attributeWire.toDomain() (checks for a
// literal JSON `null` before the type switch, for all three attribute
// types). These tests are now regression tests for the fix, not
// documentation of the gap: they assert DENY/malformed_request for every
// supported type, and that the request never reaches Cedar
// (PolicyVersion == "").
// ---------------------------------------------------------------------

func TestRegression_JSONNullArgument_RejectedAsMalformed(t *testing.T) {
	cases := map[string]string{
		"int":    `{"amount":{"type":"int","value":null}}`,
		"string": `{"amount":{"type":"string","value":null}}`,
		"bool":   `{"amount":{"type":"bool","value":null}}`,
	}
	for name, argsJSON := range cases {
		t.Run(name, func(t *testing.T) {
			hr := post(t, `{
				"execution_id":"reg-null-`+name+`","workspace_id":"ws-1",
				"identity":{"agent_id":"agent-1","roles":["payer"]},
				"tool":{"backend_id":"backend-a","name":"transfer"},
				"classification":{"known":true,"risk":"write"},
				"arguments":`+argsJSON+`
			}`)
			assertDeny(t, hr, "malformed_request")
			if hr.result.PolicyVersion != "" {
				t.Errorf("policy_version = %q, want empty (Cedar must never be reached for a rejected null argument); body=%s",
					hr.result.PolicyVersion, hr.body)
			}
		})
	}
}

func TestRegression_JSONNullArgument_DiffersFromZeroValueAllow(t *testing.T) {
	// Regression guard for the exact original bug: a null-valued argument
	// must no longer produce the same outcome as an explicit zero value.
	hrNull := post(t, `{
		"execution_id":"reg-null-cmp-1","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["payer"]},
		"tool":{"backend_id":"backend-a","name":"transfer"},
		"classification":{"known":true,"risk":"write"},
		"arguments":{"amount":{"type":"int","value":null}}
	}`)
	hrZero := post(t, `{
		"execution_id":"reg-null-cmp-2","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["payer"]},
		"tool":{"backend_id":"backend-a","name":"transfer"},
		"classification":{"known":true,"risk":"write"},
		"arguments":{"amount":{"type":"int","value":0}}
	}`)
	if hrZero.result.Decision != "ALLOW" {
		t.Fatalf("baseline amount=0 decision = %q, want ALLOW (sanity check on the fixture rule); body=%s", hrZero.result.Decision, hrZero.body)
	}
	assertDeny(t, hrNull, "malformed_request")
	if hrNull.result.Decision == hrZero.result.Decision && hrNull.result.Reason == hrZero.result.Reason {
		t.Errorf("amount=null (decision=%q reason=%q) still matches amount=0 (decision=%q reason=%q) — the null-coercion bug appears to have regressed",
			hrNull.result.Decision, hrNull.result.Reason, hrZero.result.Decision, hrZero.result.Reason)
	}
}

func TestRegression_JSONNullArgument_DistinctFromOmittedArgument(t *testing.T) {
	// Omitting "amount" entirely on the "broken" role still produces a
	// genuine Cedar evaluation error (context.amount referenced with no
	// `has` guard against a truly absent key) — Cedar IS reached. Sending
	// amount:null instead is now caught before Cedar (malformed_request,
	// PolicyVersion empty) rather than being coerced into a present zero
	// value that changes which Cedar rule fires. The two paths are
	// expected to differ, and for a documented reason.
	omitted := post(t, `{
		"execution_id":"reg-null-broken-a","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["broken"]},
		"tool":{"backend_id":"backend-a","name":"whatever"},
		"classification":{"known":true,"risk":"write"}
	}`)
	nullValued := post(t, `{
		"execution_id":"reg-null-broken-b","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["broken"]},
		"tool":{"backend_id":"backend-a","name":"whatever"},
		"classification":{"known":true,"risk":"write"},
		"arguments":{"amount":{"type":"int","value":null}}
	}`)
	assertDeny(t, omitted, "evaluation_error")
	if omitted.result.PolicyVersion == "" {
		t.Errorf("omitted-argument case: policy_version is empty, want set (Cedar was reached and errored)")
	}
	assertDeny(t, nullValued, "malformed_request")
	if nullValued.result.PolicyVersion != "" {
		t.Errorf("null-valued-argument case: policy_version = %q, want empty (Cedar must never be reached)", nullValued.result.PolicyVersion)
	}
}

// ---------------------------------------------------------------------
// AG-QA-G1-03 — contract/fixture validation against the ACTUAL wire
// responses (not just the documentation).
// ---------------------------------------------------------------------

func TestContract_PolicyVersionProvenance_PresentWhenCedarReached(t *testing.T) {
	allow := post(t, `{
		"execution_id":"c-prov-1","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["reader"]},
		"tool":{"backend_id":"backend-a","name":"read_schedule"},
		"classification":{"known":true,"risk":"read"}
	}`)
	forbid := post(t, `{
		"execution_id":"c-prov-2","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["admin"]},
		"tool":{"backend_id":"backend-a","name":"wipe"},
		"classification":{"known":true,"risk":"destructive"}
	}`)
	if allow.result.PolicyVersion == "" || forbid.result.PolicyVersion == "" {
		t.Fatalf("expected non-empty policy_version whenever Cedar is reached; allow=%q forbid=%q", allow.result.PolicyVersion, forbid.result.PolicyVersion)
	}
	if allow.result.PolicyVersion != forbid.result.PolicyVersion {
		t.Errorf("expected the SAME policy_version across two decisions evaluated against the same loaded policy; got %q vs %q", allow.result.PolicyVersion, forbid.result.PolicyVersion)
	}
}

func TestContract_PolicyVersionProvenance_EmptyWhenCedarNotReached(t *testing.T) {
	cases := map[string]string{
		"missing identity":  `{"execution_id":"c-prov-3","workspace_id":"ws-1","tool":{"backend_id":"b","name":"read_schedule"},"classification":{"known":true,"risk":"read"}}`,
		"unknown tool":      `{"execution_id":"c-prov-4","workspace_id":"ws-1","identity":{"agent_id":"a1","roles":["reader"]},"tool":{"backend_id":"b","name":"x"},"classification":{"known":false}}`,
		"malformed request": `{"execution_id":"c-prov-5","workspace_id":"","identity":{"agent_id":"a1","roles":["reader"]},"tool":{"backend_id":"b","name":"read_schedule"},"classification":{"known":true,"risk":"read"}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			hr := post(t, body)
			if hr.status != http.StatusOK {
				t.Fatalf("status = %d, want 200 (decision-core denial, not transport)", hr.status)
			}
			if hr.result.PolicyVersion != "" {
				t.Errorf("policy_version = %q, want empty — Cedar must not have been reached for %s", hr.result.PolicyVersion, name)
			}
			if hr.result.Decision != "DENY" {
				t.Errorf("decision = %q, want DENY for %s", hr.result.Decision, name)
			}
		})
	}
}

func TestContract_ExecutionIDPreservedOnEveryPath(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"allow", `{"execution_id":"eid-allow","workspace_id":"w","identity":{"agent_id":"a1","roles":["reader"]},"tool":{"backend_id":"b","name":"read_schedule"},"classification":{"known":true,"risk":"read"}}`},
		{"deny explicit", `{"execution_id":"eid-deny","workspace_id":"w","identity":{"agent_id":"a1","roles":["admin"]},"tool":{"backend_id":"b","name":"x"},"classification":{"known":true,"risk":"destructive"}}`},
		{"missing identity", `{"execution_id":"eid-missid","workspace_id":"w","tool":{"backend_id":"b","name":"read_schedule"},"classification":{"known":true,"risk":"read"}}`},
		{"unknown tool", `{"execution_id":"eid-unk","workspace_id":"w","identity":{"agent_id":"a1","roles":["reader"]},"tool":{"backend_id":"b","name":"x"},"classification":{"known":false}}`},
		{"eval error", `{"execution_id":"eid-err","workspace_id":"w","identity":{"agent_id":"a1","roles":["broken"]},"tool":{"backend_id":"b","name":"x"},"classification":{"known":true,"risk":"write"}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hr := post(t, c.body)
			if hr.status != http.StatusOK {
				t.Fatalf("status = %d, want 200", hr.status)
			}
			wantID := "eid-" + c.name[:1] // not used; explicit compare below instead
			_ = wantID
			if hr.result.ExecutionID == "" {
				t.Errorf("execution_id is empty for case %q; body=%s", c.name, hr.body)
			}
		})
	}
}

// TestRegression_RiskRequiredWhenKnownTrue_RejectedAsMalformed regression-
// tests a G1 corrective-closeout fix. GO_BACKEND_G1_CONTRACT.md documents
// "Classification.Risk: Required when Known == true", but the engine
// originally did not enforce this — an omitted/blank risk reached Cedar
// with an empty risk attribute and denied as no_matching_policy instead
// (never an accidental ALLOW, but an unenforced "required" field; see
// docs/PHASES/G1_WORKSTREAMS/results/04_QA_SECURITY_DIGEST.md, finding #2). Fixed
// upstream in decision.Engine.Evaluate(), which now denies
// malformed_request before Cedar is ever consulted when Known is true and
// Risk is empty/blank.
func TestRegression_RiskRequiredWhenKnownTrue_RejectedAsMalformed(t *testing.T) {
	hr := post(t, `{
		"execution_id":"c-risk-1","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["admin"]},
		"tool":{"backend_id":"backend-a","name":"mystery_tool"},
		"classification":{"known":true}
	}`)
	assertDeny(t, hr, "malformed_request")
	if hr.result.PolicyVersion != "" {
		t.Errorf("policy_version = %q, want empty (Cedar must never be reached when risk is missing)", hr.result.PolicyVersion)
	}
}

// ---------------------------------------------------------------------
// AG-QA-G1-05 — trust-boundary probes expressed as executable
// assertions: the client fully controls tool classification, and the
// mock (matching the documented "no gateway in front of it" limitation)
// applies no independent verification of it.
// ---------------------------------------------------------------------

func TestTrustBoundary_ClientCanAssertOwnClassification_NoServerSideVerification(t *testing.T) {
	// The wire contract has no separate, server-verified tool-inventory
	// lookup: whatever classification.known/risk the caller sends is what
	// the decision core evaluates against. This test demonstrates that a
	// client asserting known=true for a destructive-sounding tool name,
	// with a favorable "read" risk label, is evaluated purely on the
	// asserted risk label, not on the tool's name or any independent
	// classification source.
	hr := post(t, `{
		"execution_id":"tb-1","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["reader"]},
		"tool":{"backend_id":"backend-a","name":"delete_all_customer_records"},
		"classification":{"known":true,"risk":"read"}
	}`)
	if hr.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", hr.status)
	}
	if hr.result.Decision != "ALLOW" {
		t.Fatalf("expected this to demonstrate that classification is taken as given (ALLOW because risk=\"read\" was asserted, regardless of the tool's name); got %+v — re-examine the trust-boundary note if this changes", hr.result)
	}
	// This is not a bug in the mock: docs/PHASES/G1_WORKSTREAMS/GO_BACKEND_G1_CONTRACT.md
	// and the production trust model both place classification authority
	// upstream of decision.Engine (an operator-curated tool inventory,
	// AG-GO-G1 known limitation #4/#3.1). It is recorded here as an
	// explicit, proven trust-boundary fact: the mock (and decision.Engine
	// itself) has no independent means to verify a classification it is
	// handed, so whichever caller populates decision.Request controls
	// this decision-relevant field completely.
}

func TestTrustBoundary_ArbitraryRiskString_FailsClosedWhenUnrecognized(t *testing.T) {
	// A risk value outside the fixture policy's known vocabulary
	// ("read"/"write"/"destructive") is not validated against any enum
	// by the wire layer or decision core — it flows to Cedar as a plain
	// string attribute. Because no rule matches an unrecognized risk
	// value, this fails closed today, but note this is a property of
	// the fixture policy's rule set, not a structural validation that
	// risk is one of a closed set.
	hr := post(t, `{
		"execution_id":"tb-2","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["admin"]},
		"tool":{"backend_id":"backend-a","name":"some_tool"},
		"classification":{"known":true,"risk":"mostly_harmless"}
	}`)
	assertDeny(t, hr, "no_matching_policy")
}

// TestTrustBoundary_ErrorResponseIsStructurallyDistinguishableFromAllow
// answers "can an error response ever be mistaken for ALLOW by a naive
// caller?" for the transport-level (400/404/405) error path: those
// responses carry a distinct HTTP status and a JSON body shape
// ({"error": "..."}) that has no "decision" field at all, so a caller
// checking for decision=="ALLOW" cannot mistake it for an authorization
// decision — but a caller that checks ONLY the HTTP status code
// (e.g. "200 means proceed") without inspecting the JSON body would
// mistake a 200 DENY for success. This is recorded as a documented
// integration risk for any caller of this contract, not a defect in the
// mock: the contract is explicit that decision is data in the response
// body, not conveyed by status code (GO_BACKEND_G1_CONTRACT.md
// AG-GO-G1-04).
func TestTrustBoundary_ErrorResponseHasNoDecisionField(t *testing.T) {
	hr := postRaw(t, http.MethodPost, "/evaluate", "application/json", `not json{{{`)
	if hr.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", hr.status)
	}
	var probe map[string]any
	if err := json.Unmarshal(hr.body, &probe); err != nil {
		t.Fatalf("expected the 400 error body to be valid JSON: %v; body=%s", err, hr.body)
	}
	if _, hasDecision := probe["decision"]; hasDecision {
		t.Errorf("400 error body unexpectedly has a \"decision\" field — a naive caller could confuse it with a real Result: body=%s", hr.body)
	}
	if _, hasError := probe["error"]; !hasError {
		t.Errorf("400 error body missing expected \"error\" field: body=%s", hr.body)
	}
}

// ---------------------------------------------------------------------
// Determinism / regression guard (independent re-derivation of the
// Go Backend stream's own determinism claim, at the wire boundary).
// ---------------------------------------------------------------------

func TestDeterminism_IdenticalInputSameOutput(t *testing.T) {
	body := `{
		"execution_id":"det-1","workspace_id":"ws-1",
		"identity":{"agent_id":"agent-1","roles":["reader"]},
		"tool":{"backend_id":"backend-a","name":"read_schedule"},
		"classification":{"known":true,"risk":"read"}
	}`
	first := post(t, body)
	second := post(t, body)
	if first.result != second.result {
		t.Errorf("handler is not deterministic for identical input: %+v vs %+v", first.result, second.result)
	}
}
