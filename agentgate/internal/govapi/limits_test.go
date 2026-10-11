package govapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dynamisch-LLC/agentgate/internal/policymanager"
	"github.com/Dynamisch-LLC/agentgate/internal/policystore"
	"github.com/Dynamisch-LLC/agentgate/internal/ratelimit"
)

func TestGovAPI_RateLimitReturns429(t *testing.T) {
	manager := policymanager.New(policystore.NewMemoryStore())
	h := NewHandler(manager, "test-secret", nil, nil, nil)
	// Rate limit: 1 req/s, burst: 2
	h.SetRateLimiter(ratelimit.New(1.0, 2))

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Make 2 allowed requests (burst = 2)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/default/policies", nil)
		req.Header.Set("X-AgentGate-Admin-Key", "test-secret")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d (body: %s)", i, rr.Code, rr.Body.String())
		}
	}

	// 3rd request exceeds burst -> 429 RATE_LIMITED
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/default/policies", nil)
	req.Header.Set("X-AgentGate-Admin-Key", "test-secret")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429 Too Many Requests, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if retryAfter := rr.Header().Get("Retry-After"); retryAfter != "1" {
		t.Errorf("expected Retry-After: 1 header, got %q", retryAfter)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error JSON: %v", err)
	}
	if errResp.Error.Code != "RATE_LIMITED" {
		t.Errorf("expected error code RATE_LIMITED, got %q", errResp.Error.Code)
	}
}

func TestGovAPI_PreAuthRateLimitReturns429(t *testing.T) {
	manager := policymanager.New(policystore.NewMemoryStore())
	h := NewHandler(manager, "test-secret", nil, nil, nil)
	// Rate limit: 1 req/s, burst: 2
	h.SetRateLimiter(ratelimit.New(1.0, 2))

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Make 2 unauthenticated requests (burst = 2) from IP 192.0.2.1:12345
	// They should fail with 401 Unauthorized, consuming rate limit tokens for that IP
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/default/policies", nil)
		req.RemoteAddr = "192.0.2.1:12345"
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("request %d: expected 401, got %d (body: %s)", i, rr.Code, rr.Body.String())
		}
	}

	// 3rd unauthenticated request from same IP exceeds burst -> 429 RATE_LIMITED
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/default/policies", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429 Too Many Requests, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if retryAfter := rr.Header().Get("Retry-After"); retryAfter != "1" {
		t.Errorf("expected Retry-After: 1 header, got %q", retryAfter)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error JSON: %v", err)
	}
	if errResp.Error.Code != "RATE_LIMITED" {
		t.Errorf("expected error code RATE_LIMITED, got %q", errResp.Error.Code)
	}

	// Unauthenticated request from a different IP (192.0.2.2:12345) should NOT be rate limited
	reqOther := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/default/policies", nil)
	reqOther.RemoteAddr = "192.0.2.2:12345"
	rrOther := httptest.NewRecorder()
	mux.ServeHTTP(rrOther, reqOther)
	if rrOther.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 for different IP, got %d (body: %s)", rrOther.Code, rrOther.Body.String())
	}
}

func TestGovAPI_PayloadTooLargeReturns413(t *testing.T) {
	manager := policymanager.New(policystore.NewMemoryStore())
	h := NewHandler(manager, "test-secret", nil, nil, nil)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Create a 2 MiB body
	largeContent := strings.Repeat("A", 2*1024*1024)
	reqBody, _ := json.Marshal(map[string]string{
		"content":     largeContent,
		"description": "oversized policy",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/default/policies", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AgentGate-Admin-Key", "test-secret")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status 413 Payload Too Large, got %d (body: %s)", rr.Code, rr.Body.String())
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error JSON: %v", err)
	}
	if errResp.Error.Code != "PAYLOAD_TOO_LARGE" {
		t.Errorf("expected error code PAYLOAD_TOO_LARGE, got %q", errResp.Error.Code)
	}
}
