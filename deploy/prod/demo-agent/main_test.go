package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestScenarioSelectionWeights verifies that scenario selection follows the expected
// probability distribution within ±3% over 10,000 draws using a seeded RNG.
func TestScenarioSelectionWeights(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	const draws = 10000

	counts := make(map[ScenarioType]int)
	for i := 0; i < draws; i++ {
		sc := SelectScenario(rng)
		counts[sc]++
	}

	expected := map[ScenarioType]float64{
		ScenarioReaderList:   0.50,
		ScenarioAdminUpdate:  0.25,
		ScenarioReaderDelete: 0.10,
		ScenarioAdminDelete:  0.10,
		ScenarioUnknownTool:  0.05,
	}

	const tolerance = 0.03 // ±3%

	for sc, expRate := range expected {
		actualCount := counts[sc]
		actualRate := float64(actualCount) / float64(draws)
		diff := actualRate - expRate
		if diff < 0 {
			diff = -diff
		}

		t.Logf("Scenario %-22s: count=%4d (%.2f%%) expected=%.2f%% diff=%.2f%%",
			sc, actualCount, actualRate*100, expRate*100, diff*100)

		if diff > tolerance {
			t.Errorf("Scenario %s proportion %.4f deviated from expected %.4f by more than ±%.2f",
				sc, actualRate, expRate, tolerance)
		}
	}
}

// fakeClock is a controllable thread-safe Clock implementation for testing.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (fc *fakeClock) Now() time.Time {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.now
}

func (fc *fakeClock) Advance(d time.Duration) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.now = fc.now.Add(d)
}

// TestTokenCacheRefresh verifies that tokens are cached and refreshed when within 30 seconds of expiry.
func TestTokenCacheRefresh(t *testing.T) {
	var requestCount int32
	fakeTokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqNum := atomic.AddInt32(&requestCount, 1)

		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("failed to parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "client_credentials" {
			t.Errorf("expected grant_type=client_credentials, got %s", r.Form.Get("grant_type"))
		}

		w.Header().Set("Content-Type", "application/json")
		// Token expires in 60 seconds
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("mock-token-v%d", reqNum),
			"expires_in":   60,
			"token_type":   "Bearer",
		})
	}))
	defer fakeTokenServer.Close()

	initialTime := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: initialTime}

	provider := NewTokenProvider(fakeTokenServer.URL, fakeTokenServer.Client(), clock.Now)

	ctx := context.Background()

	// Call 1 at T=0: Should fetch new token v1
	token1, err := provider.GetToken(ctx, "agent-reader", "secret-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token1 != "mock-token-v1" {
		t.Fatalf("expected token mock-token-v1, got %s", token1)
	}
	if got := atomic.LoadInt32(&requestCount); got != 1 {
		t.Fatalf("expected 1 server request, got %d", got)
	}

	// Call 2 at T=20s (40s remaining before exp): within cache lifetime (> 30s), should return cached token
	clock.Advance(20 * time.Second)
	token2, err := provider.GetToken(ctx, "agent-reader", "secret-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token2 != "mock-token-v1" {
		t.Fatalf("expected cached token mock-token-v1, got %s", token2)
	}
	if got := atomic.LoadInt32(&requestCount); got != 1 {
		t.Fatalf("expected server request count to remain 1, got %d", got)
	}

	// Call 3 at T=31s (29s remaining before exp): <= 30s threshold -> MUST trigger refresh!
	clock.Advance(11 * time.Second) // T=31s
	token3, err := provider.GetToken(ctx, "agent-reader", "secret-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token3 != "mock-token-v2" {
		t.Fatalf("expected refreshed token mock-token-v2, got %s", token3)
	}
	if got := atomic.LoadInt32(&requestCount); got != 2 {
		t.Fatalf("expected 2 server requests after refresh, got %d", got)
	}
}

// TestLoggingNeverContainsTokenOrSecret verifies that log output is structured JSON and
// that sensitive tokens and client secrets are strictly absent from logs.
func TestLoggingNeverContainsTokenOrSecret(t *testing.T) {
	const secretReader = "SUPER_SECRET_READER_KEY_998877"
	const tokenReader = "EY_JWT_BEARER_TOKEN_VALUE_READER_112233"
	const secretAdmin = "SUPER_SECRET_ADMIN_KEY_554433"
	const tokenAdmin = "EY_JWT_BEARER_TOKEN_VALUE_ADMIN_778899"

	var logBuf bytes.Buffer
	logger := NewJSONLogger(&logBuf)

	// Mock Gateway: returns 200 for list_tickets, 403 for delete_ticket
	gwServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		params, _ := req["params"].(map[string]any)
		toolName, _ := params["name"].(string)

		if toolName == "delete_ticket" || toolName == "execute_arbitrary_shell" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":403,"message":"forbidden"}}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"tickets":[{"id":"TCK-9900"}]}}`))
	}))
	defer gwServer.Close()

	// Mock Token server returning sensitive tokens
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		clientID := r.Form.Get("client_id")
		tok := tokenReader
		if clientID == "agent-admin" {
			tok = tokenAdmin
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": tok,
			"expires_in":   300,
		})
	}))
	defer tokenServer.Close()

	clients := map[string]string{
		"agent-reader": secretReader,
		"agent-admin":  secretAdmin,
	}

	clock := func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) }
	tokenProvider := NewTokenProvider(tokenServer.URL, tokenServer.Client(), clock)
	runner := NewRunner(&Config{
		GatewayURL:    gwServer.URL,
		Clients:       clients,
		TokenProvider: tokenProvider,
		Logger:        logger,
		HTTPClient:    gwServer.Client(),
		RNG:           rand.New(rand.NewSource(1)),
	})

	ctx := context.Background()

	// Execute call 1: ALLOW
	runner.ExecuteScenario(ctx, ScenarioReaderList)

	// Execute call 2: DENY
	runner.ExecuteScenario(ctx, ScenarioReaderDelete)

	logOutput := logBuf.String()
	lines := strings.Split(strings.TrimSpace(logOutput), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines, got %d:\n%s", len(lines), logOutput)
	}

	// Verify line 1 (ALLOW)
	var entry1 LogEntry
	if err := json.Unmarshal([]byte(lines[0]), &entry1); err != nil {
		t.Fatalf("line 1 is not valid JSON: %v", err)
	}
	if entry1.Agent != "agent-reader" || entry1.Tool != "list_tickets" || entry1.HTTPStatus != 200 || entry1.Outcome != "ALLOW" {
		t.Errorf("entry 1 mismatch: %+v", entry1)
	}

	// Verify line 2 (DENY)
	var entry2 LogEntry
	if err := json.Unmarshal([]byte(lines[1]), &entry2); err != nil {
		t.Fatalf("line 2 is not valid JSON: %v", err)
	}
	if entry2.Agent != "agent-reader" || entry2.Tool != "delete_ticket" || entry2.HTTPStatus != 403 || entry2.Outcome != "DENY" {
		t.Errorf("entry 2 mismatch: %+v", entry2)
	}

	// Security Check: Verify secrets and tokens are NEVER in log output
	forbiddenSubstrings := []string{
		secretReader,
		tokenReader,
		secretAdmin,
		tokenAdmin,
	}

	for _, sub := range forbiddenSubstrings {
		if strings.Contains(logOutput, sub) {
			t.Fatalf("SECURITY VIOLATION: sensitive string %q found in log output:\n%s", sub, logOutput)
		}
	}
}

// TestParseClients verifies parsing client specifications both from raw strings and from files.
func TestParseClients(t *testing.T) {
	tmpDir := t.TempDir()
	secretFile := filepath.Join(tmpDir, "reader_secret")
	if err := os.WriteFile(secretFile, []byte("file-secret-value\n"), 0600); err != nil {
		t.Fatalf("failed to write temp secret file: %v", err)
	}

	raw := fmt.Sprintf("agent-reader:%s,agent-admin:inline-secret-value", secretFile)
	clients, err := ParseClients(raw)
	if err != nil {
		t.Fatalf("unexpected error parsing clients: %v", err)
	}

	if clients["agent-reader"] != "file-secret-value" {
		t.Errorf("expected file-secret-value, got %s", clients["agent-reader"])
	}
	if clients["agent-admin"] != "inline-secret-value" {
		t.Errorf("expected inline-secret-value, got %s", clients["agent-admin"])
	}
}

// TestTicketDiscovery verifies that list_tickets updates ticket tracker and update_ticket_status reuses them.
func TestTicketDiscovery(t *testing.T) {
	tracker := NewTicketTracker()
	if id := tracker.GetRandomID(rand.New(rand.NewSource(1))); id != "TCK-1001" {
		t.Errorf("expected fallback ticket TCK-1001, got %s", id)
	}

	tracker.UpdateFromTickets([]string{"TCK-5555", "TCK-6666"})
	id := tracker.GetRandomID(rand.New(rand.NewSource(1)))
	if id != "TCK-5555" && id != "TCK-6666" {
		t.Errorf("expected discovered ticket id, got %s", id)
	}
}
