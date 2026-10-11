package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestRegistry_ExpositionFormat(t *testing.T) {
	reg := NewRegistry()

	decisions := reg.NewCounterVec(
		"agentgate_decisions_total",
		"Total number of authorization decisions evaluated.",
		[]string{"decision", "reason"},
	)
	duration := reg.NewHistogramVec(
		"agentgate_decision_duration_seconds",
		"Duration of authorization decisions in seconds.",
		[]string{"decision"},
		DefaultBuckets,
	)
	auditFailures := reg.NewCounter(
		"agentgate_audit_append_failures_total",
		"Total number of audit append failures.",
	)
	credFailures := reg.NewCounter(
		"agentgate_credential_issuance_failures_total",
		"Total number of downstream credential issuance failures.",
	)
	buildInfo := reg.NewGauge(
		"agentgate_build_info",
		"AgentGate build and version information.",
		[]string{"version"},
	)

	// Populate values
	decisions.Inc("DENY", "unknown_tool")
	decisions.Inc("ALLOW", "policy_allow")
	decisions.Inc("ALLOW", "policy_allow")

	duration.Observe(0.002, "ALLOW")
	duration.Observe(0.05, "ALLOW")

	auditFailures.Inc()
	_ = credFailures // verified as 0

	buildInfo.Set(1, "v1.0.0-prod")

	var buf strings.Builder
	if err := reg.WriteText(&buf); err != nil {
		t.Fatalf("WriteText failed: %v", err)
	}

	out := buf.String()

	// Verify HELP and TYPE comments exist
	expectedHeaders := []string{
		"# HELP agentgate_decisions_total Total number of authorization decisions evaluated.",
		"# TYPE agentgate_decisions_total counter",
		"# HELP agentgate_decision_duration_seconds Duration of authorization decisions in seconds.",
		"# TYPE agentgate_decision_duration_seconds histogram",
		"# HELP agentgate_audit_append_failures_total Total number of audit append failures.",
		"# TYPE agentgate_audit_append_failures_total counter",
		"# HELP agentgate_credential_issuance_failures_total Total number of downstream credential issuance failures.",
		"# TYPE agentgate_credential_issuance_failures_total counter",
		"# HELP agentgate_build_info AgentGate build and version information.",
		"# TYPE agentgate_build_info gauge",
	}
	for _, h := range expectedHeaders {
		if !strings.Contains(out, h) {
			t.Errorf("output missing header %q\nFull output:\n%s", h, out)
		}
	}

	// Verify counter values and sorted label sets
	expectedSamples := []string{
		`agentgate_decisions_total{decision="ALLOW",reason="policy_allow"} 2`,
		`agentgate_decisions_total{decision="DENY",reason="unknown_tool"} 1`,
		`agentgate_audit_append_failures_total 1`,
		`agentgate_credential_issuance_failures_total 0`,
		`agentgate_build_info{version="v1.0.0-prod"} 1`,
	}
	for _, s := range expectedSamples {
		if !strings.Contains(out, s) {
			t.Errorf("output missing sample %q\nFull output:\n%s", s, out)
		}
	}

	// Verify histogram bucket lines (cumulative), _sum and _count
	expectedHistogram := []string{
		`agentgate_decision_duration_seconds_bucket{decision="ALLOW",le="0.001"} 0`,
		`agentgate_decision_duration_seconds_bucket{decision="ALLOW",le="0.005"} 1`,
		`agentgate_decision_duration_seconds_bucket{decision="ALLOW",le="0.01"} 1`,
		`agentgate_decision_duration_seconds_bucket{decision="ALLOW",le="0.025"} 1`,
		`agentgate_decision_duration_seconds_bucket{decision="ALLOW",le="0.05"} 2`,
		`agentgate_decision_duration_seconds_bucket{decision="ALLOW",le="0.1"} 2`,
		`agentgate_decision_duration_seconds_bucket{decision="ALLOW",le="0.25"} 2`,
		`agentgate_decision_duration_seconds_bucket{decision="ALLOW",le="0.5"} 2`,
		`agentgate_decision_duration_seconds_bucket{decision="ALLOW",le="1"} 2`,
		`agentgate_decision_duration_seconds_bucket{decision="ALLOW",le="2.5"} 2`,
		`agentgate_decision_duration_seconds_bucket{decision="ALLOW",le="+Inf"} 2`,
		`agentgate_decision_duration_seconds_sum{decision="ALLOW"} 0.052`,
		`agentgate_decision_duration_seconds_count{decision="ALLOW"} 2`,
	}
	for _, h := range expectedHistogram {
		if !strings.Contains(out, h) {
			t.Errorf("output missing histogram line %q\nFull output:\n%s", h, out)
		}
	}
}

func TestCounter_ConcurrentRace(t *testing.T) {
	reg := NewRegistry()
	c := reg.NewCounter("concur_test_total", "Test counter")
	cv := reg.NewCounterVec("concur_vec_total", "Test counter vec", []string{"label"})

	const workers = 50
	const increments = 200

	var wg sync.WaitGroup
	wg.Add(workers * 2)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < increments; j++ {
				c.Inc()
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < increments; j++ {
				cv.Inc("val")
			}
		}()
	}

	wg.Wait()

	if got := c.Value(); got != float64(workers*increments) {
		t.Errorf("Counter.Value() = %v, want %v", got, float64(workers*increments))
	}

	var buf strings.Builder
	if err := reg.WriteText(&buf); err != nil {
		t.Fatalf("WriteText failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `concur_test_total 10000`) {
		t.Errorf("missing expected concur_test_total count in output: %s", out)
	}
	if !strings.Contains(out, `concur_vec_total{label="val"} 10000`) {
		t.Errorf("missing expected concur_vec_total count in output: %s", out)
	}
}

func TestHTTPHandler(t *testing.T) {
	reg := NewRegistry()
	c := reg.NewCounter("test_counter", "Test counter help")
	c.Inc()

	handler := reg.Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status code = %v, want %v", resp.StatusCode, http.StatusOK)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") || !strings.Contains(ct, "version=0.0.4") {
		t.Errorf("Content-Type = %q, want text/plain; version=0.0.4", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if !strings.Contains(string(body), "test_counter 1") {
		t.Errorf("body missing counter line: %s", string(body))
	}
}
