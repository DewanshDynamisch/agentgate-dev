package v1security_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type Ticket struct {
	ID      string `json:"id"`
	Subject string `json:"subject"`
	Status  string `json:"status"`
}

type InvocationRecord struct {
	Timestamp     string            `json:"timestamp"`
	ToolName      string            `json:"tool_name"`
	Arguments     map[string]any    `json:"arguments"`
	Headers       map[string]string `json:"headers"`
	RawRequest    string            `json:"raw_request"`
	CredentialSub string            `json:"credential_sub"`
	CredentialAct string            `json:"credential_act"`
	CredentialJTI string            `json:"credential_jti"`
}

type BackendState struct {
	Tickets     []Ticket           `json:"tickets"`
	Invocations []InvocationRecord `json:"invocations"`
	Count       int                `json:"count"`
}

type AuditEventView struct {
	ID                  int64     `json:"id"`
	WorkspaceID         string    `json:"workspace_id"`
	SequenceNumber      int64     `json:"sequence_number"`
	ExecutionID         string    `json:"execution_id"`
	Timestamp           time.Time `json:"timestamp"`
	EventType           string    `json:"event_type"`
	Decision            string    `json:"decision"`
	Reason              string    `json:"reason"`
	PrincipalAgentID    string    `json:"principal_agent_id"`
	PrincipalRoles      []string  `json:"principal_roles"`
	PrincipalOnBehalfOf string    `json:"principal_on_behalf_of"`
	ToolBackendID       string    `json:"tool_backend_id"`
	ToolName            string    `json:"tool_name"`
	ToolRisk            string            `json:"tool_risk"`
	PolicyVersion       string            `json:"policy_version"`
	PolicyHash          string            `json:"policy_hash"`
	RedactedArguments   map[string]string `json:"redacted_arguments"`
}

func extractJWTSubject(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) >= 2 {
		raw, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err == nil {
			var claims struct {
				Sub string `json:"sub"`
			}
			if json.Unmarshal(raw, &claims) == nil && claims.Sub != "" {
				return claims.Sub
			}
		}
	}
	return ""
}

// FetchToken obtains a JWT token from Keycloak using client_credentials grant.
func (cfg *TestConfig) FetchToken(t *testing.T, clientID, clientSecret string) string {
	t.Helper()
	endpoint := fmt.Sprintf("%s/realms/agentgate/protocol/openid-connect/token", cfg.KeycloakURL)
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)

	var lastErr error
	var lastStatus int
	var lastBody []byte

	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			time.Sleep(1 * time.Second)
		}
		req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(data.Encode()))
		if err != nil {
			t.Fatalf("failed to create token request: %v", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := cfg.HTTPClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		lastStatus = resp.StatusCode
		lastBody = body

		if resp.StatusCode == http.StatusOK {
			var parsed struct {
				AccessToken string `json:"access_token"`
			}
			if err := json.Unmarshal(body, &parsed); err != nil {
				t.Fatalf("failed to unmarshal token response: %v", err)
			}
			if parsed.AccessToken == "" {
				t.Fatalf("empty access_token received for client %s", clientID)
			}
			return parsed.AccessToken
		}
	}

	t.Fatalf("token endpoint returned status %d: %s (err: %v)", lastStatus, string(lastBody), lastErr)
	return ""
}

// FetchAdminToken obtains an admin token from Keycloak using password grant on agentgate-ops-cli.
func (cfg *TestConfig) FetchAdminToken(t *testing.T) string {
	t.Helper()
	endpoint := fmt.Sprintf("%s/realms/agentgate/protocol/openid-connect/token", cfg.KeycloakURL)
	data := url.Values{}
	data.Set("grant_type", "password")
	data.Set("client_id", "agentgate-ops-cli")
	data.Set("client_secret", cfg.OpsCliSecret)
	data.Set("username", "admin")
	data.Set("password", cfg.OpsCliPassword)

	var lastErr error
	var lastStatus int
	var lastBody []byte

	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			time.Sleep(1 * time.Second)
		}
		req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(data.Encode()))
		if err != nil {
			t.Fatalf("failed to create admin token request: %v", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := cfg.HTTPClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		lastStatus = resp.StatusCode
		lastBody = body

		if resp.StatusCode == http.StatusOK {
			var parsed struct {
				AccessToken string `json:"access_token"`
			}
			if err := json.Unmarshal(body, &parsed); err != nil {
				t.Fatalf("failed to unmarshal admin token: %v", err)
			}
			if parsed.AccessToken == "" {
				t.Fatalf("empty access_token received for client agentgate-ops-cli")
			}
			return parsed.AccessToken
		}
	}

	t.Fatalf("admin token endpoint returned status %d: %s (err: %v)", lastStatus, string(lastBody), lastErr)
	return ""
}

// CallGatewayTool executes an MCP tool call through the gateway.
func (cfg *TestConfig) CallGatewayTool(t *testing.T, token, toolName string, args map[string]any, extraHeaders map[string]string) (int, []byte, error) {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      time.Now().UnixNano(),
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal JSON-RPC tool call: %v", err)
	}
	return cfg.CallGatewayRaw(t, token, raw, extraHeaders)
}

// CallGatewayRaw sends raw bytes to the gateway root.
func (cfg *TestConfig) CallGatewayRaw(t *testing.T, token string, body []byte, extraHeaders map[string]string) (int, []byte, error) {
	t.Helper()
	endpoint := cfg.GatewayURL + "/"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		if strings.HasPrefix(token, "Bearer ") {
			req.Header.Set("Authorization", token)
		} else {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, respBody, nil
}

// CallBackendDirect sends a tool call directly to support-desk-mcp.
func (cfg *TestConfig) CallBackendDirect(t *testing.T, cred, toolName string, args map[string]any) (int, []byte, error) {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      time.Now().UnixNano(),
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}
	raw, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, cfg.BackendURL+"/", bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cred != "" {
		if strings.HasPrefix(cred, "Bearer ") {
			req.Header.Set("Authorization", cred)
		} else {
			req.Header.Set("Authorization", "Bearer "+cred)
		}
	}

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, body, err
}

// GetBackendState retrieves current tickets and invocations recorded by the backend.
func (cfg *TestConfig) GetBackendState(t *testing.T) *BackendState {
	t.Helper()
	resp, err := cfg.HTTPClient.Get(cfg.BackendURL + "/_demo/state")
	if err != nil {
		t.Fatalf("failed to query backend state: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("backend state returned status %d", resp.StatusCode)
	}

	var state BackendState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		t.Fatalf("failed to decode backend state: %v", err)
	}
	return &state
}

// GetLastCredential retrieves the last unredacted raw credential captured by support-desk-mcp.
func (cfg *TestConfig) GetLastCredential(t *testing.T) string {
	t.Helper()
	resp, err := cfg.HTTPClient.Get(cfg.BackendURL + "/_demo/last-credential")
	if err != nil {
		t.Fatalf("failed to fetch last credential: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("last-credential endpoint returned status %d", resp.StatusCode)
	}

	var result struct {
		Credential string `json:"credential"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode last credential response: %v", err)
	}
	return result.Credential
}

// ResetBackend resets backend tickets and invocations to initial seed.
func (cfg *TestConfig) ResetBackend(t *testing.T) {
	t.Helper()
	resp, err := cfg.HTTPClient.Post(cfg.BackendURL+"/_demo/reset", "application/json", nil)
	if err != nil {
		t.Fatalf("failed to reset backend: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("backend reset returned status %d", resp.StatusCode)
	}
}

// CallAdminAPI invokes an Admin API route.
func (cfg *TestConfig) CallAdminAPI(t *testing.T, token, method, path string, body []byte) (int, []byte, error) {
	t.Helper()
	endpoint := cfg.AdminURL + path
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, endpoint, reader)
	if err != nil {
		return 0, nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		if strings.HasPrefix(token, "Bearer ") {
			req.Header.Set("Authorization", token)
		} else {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody, err
}

// SetToolRisk updates a tool's risk classification in the runtime tool catalog.
func (cfg *TestConfig) SetToolRisk(t *testing.T, token, backendID, toolName, risk string) error {
	t.Helper()
	// Get current definition first
	status, body, err := cfg.CallAdminAPI(t, token, http.MethodGet, fmt.Sprintf("/api/v1/tools/%s/%s", backendID, toolName), nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("get tool returned status %d: %s", status, string(body))
	}

	var current struct {
		Schema      json.RawMessage `json:"schema"`
		Arguments   []any           `json:"arguments"`
		Description string          `json:"description"`
	}
	_ = json.Unmarshal(body, &current)

	putBody, _ := json.Marshal(map[string]any{
		"risk":        risk,
		"schema":      current.Schema,
		"arguments":   current.Arguments,
		"description": current.Description,
	})

	status, putResp, err := cfg.CallAdminAPI(t, token, http.MethodPut, fmt.Sprintf("/api/v1/tools/%s/%s", backendID, toolName), putBody)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("put tool returned status %d: %s", status, string(putResp))
	}
	return nil
}

// GetAuditEvents fetches audit events from Admin API, paging backwards to gather up to 500 records.
func (cfg *TestConfig) GetAuditEvents(t *testing.T, token, workspaceID string) ([]AuditEventView, error) {
	t.Helper()
	var allEvents []AuditEventView
	var beforeSeq *int64

	for len(allEvents) < 500 {
		path := fmt.Sprintf("/api/v1/workspaces/%s/audit-events?limit=100", workspaceID)
		if beforeSeq != nil {
			path = fmt.Sprintf("/api/v1/workspaces/%s/audit-events?limit=100&before_sequence=%d", workspaceID, *beforeSeq)
		}
		status, body, err := cfg.CallAdminAPI(t, token, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("audit events returned status %d: %s", status, string(body))
		}

		var resp struct {
			Events             []AuditEventView `json:"events"`
			NextBeforeSequence *int64           `json:"next_before_sequence"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, err
		}
		if len(resp.Events) == 0 {
			break
		}
		allEvents = append(allEvents, resp.Events...)
		if resp.NextBeforeSequence == nil {
			break
		}
		beforeSeq = resp.NextBeforeSequence
	}
	return allEvents, nil
}

// StopAgentGate stops the agentgate container.
func (cfg *TestConfig) StopAgentGate(t *testing.T) {
	t.Helper()
	if err := dockerContainerAction(t, cfg.ContainerName, "stop"); err != nil {
		t.Fatalf("failed to stop %s: %v", cfg.ContainerName, err)
	}
}

// StartAgentGate starts the agentgate container.
func (cfg *TestConfig) StartAgentGate(t *testing.T) {
	t.Helper()
	if err := dockerContainerAction(t, cfg.ContainerName, "start"); err != nil {
		t.Fatalf("failed to start %s: %v", cfg.ContainerName, err)
	}
}

func dockerContainerAction(t *testing.T, name, action string) error {
	t.Helper()
	// Try docker CLI first
	if path, err := exec.LookPath("docker"); err == nil {
		cmd := exec.Command(path, action, name)
		if out, err := cmd.CombinedOutput(); err == nil {
			return nil
		} else {
			t.Logf("docker cli %s failed (%v): %s; falling back to socket", action, err, string(out))
		}
	}

	// Try unix socket
	sockClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", "/var/run/docker.sock")
			},
		},
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://localhost/containers/%s/%s", name, action), nil)
	if err != nil {
		return err
	}
	resp, err := sockClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotModified {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("docker API status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// WaitForAgentGate polls healthz until healthy or timeout.
func (cfg *TestConfig) WaitForAgentGate(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := cfg.HTTPClient.Get(cfg.AdminURL + "/healthz")
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for agentgate healthy on %s/healthz", cfg.AdminURL)
}

// GenerateForgedToken generates a self-signed RSA JWT with untrusted key.
func (cfg *TestConfig) GenerateForgedToken(t *testing.T) string {
	t.Helper()
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	header := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
		"kid": "untrusted-forged-kid",
	}
	headerBytes, _ := json.Marshal(header)
	headerB64 := base64.RawURLEncoding.EncodeToString(headerBytes)

	now := time.Now()
	claims := map[string]any{
		"iss":   cfg.KeycloakURL + "/realms/agentgate",
		"aud":   []string{"agentgate-gateway"},
		"sub":   "agent-admin",
		"roles": []string{"admin"},
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}
	claimsBytes, _ := json.Marshal(claims)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsBytes)

	sigInput := headerB64 + "." + claimsB64
	sum := sha256.Sum256([]byte(sigInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("failed to sign forged token: %v", err)
	}
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return sigInput + "." + sigB64
}
