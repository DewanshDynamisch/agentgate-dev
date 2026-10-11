package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ScenarioType identifies an agent traffic scenario.
type ScenarioType string

const (
	ScenarioReaderList   ScenarioType = "reader_list_tickets"
	ScenarioAdminUpdate  ScenarioType = "admin_update_ticket"
	ScenarioReaderDelete ScenarioType = "reader_delete_ticket"
	ScenarioAdminDelete  ScenarioType = "admin_delete_ticket"
	ScenarioUnknownTool  ScenarioType = "unknown_tool"
)

// SelectScenario selects a scenario based on target weights:
// - reader list_tickets (50%)
// - admin update_ticket_status (25%)
// - reader delete_ticket (10%)
// - admin delete_ticket (10%)
// - unknown tool (5%)
func SelectScenario(r *rand.Rand) ScenarioType {
	val := r.Float64()
	switch {
	case val < 0.50:
		return ScenarioReaderList
	case val < 0.75:
		return ScenarioAdminUpdate
	case val < 0.85:
		return ScenarioReaderDelete
	case val < 0.95:
		return ScenarioAdminDelete
	default:
		return ScenarioUnknownTool
	}
}

// LogEntry represents the structured audit/telemetry log format.
// CRITICAL: Tokens and client secrets are NEVER included in this struct.
type LogEntry struct {
	Time       string `json:"time"`
	Agent      string `json:"agent"`
	Tool       string `json:"tool"`
	HTTPStatus int    `json:"http_status"`
	Outcome    string `json:"outcome"`
}

// JSONLogger formats and prints LogEntries as single-line JSON.
type JSONLogger struct {
	mu sync.Mutex
	w  io.Writer
}

func NewJSONLogger(w io.Writer) *JSONLogger {
	return &JSONLogger{w: w}
}

func (l *JSONLogger) Log(entry LogEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.w.Write(append(data, '\n'))
}

// ClockFunc provides time abstraction for deterministic testing.
type ClockFunc func() time.Time

// TokenProvider manages fetching and caching OAuth2 client-credentials tokens.
type TokenProvider struct {
	tokenURL   string
	httpClient *http.Client
	clock      ClockFunc
	mu         sync.RWMutex
	cache      map[string]*cachedToken
}

type cachedToken struct {
	accessToken string
	expiresAt   time.Time
}

func NewTokenProvider(tokenURL string, client *http.Client, clock ClockFunc) *TokenProvider {
	if clock == nil {
		clock = time.Now
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &TokenProvider{
		tokenURL:   tokenURL,
		httpClient: client,
		clock:      clock,
		cache:      make(map[string]*cachedToken),
	}
}

// GetToken returns a valid OAuth2 bearer token for clientID.
// It caches the token until 30 seconds before expiration.
func (p *TokenProvider) GetToken(ctx context.Context, clientID, clientSecret string) (string, error) {
	now := p.clock()

	p.mu.RLock()
	cached, ok := p.cache[clientID]
	p.mu.RUnlock()

	// Return cached token if valid and more than 30 seconds before expiration
	if ok && cached.accessToken != "" && now.Before(cached.expiresAt.Add(-30*time.Second)) {
		return cached.accessToken, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Re-check cache after acquiring write lock
	cached, ok = p.cache[clientID]
	if ok && cached.accessToken != "" && now.Before(cached.expiresAt.Add(-30*time.Second)) {
		return cached.accessToken, nil
	}

	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("create token request for %s: %w", clientID, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request token for %s: %w", clientID, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read token response for %s: %w", clientID, err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint for %s returned HTTP %d", clientID, resp.StatusCode)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("parse token response for %s: %w", clientID, err)
	}

	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("token endpoint for %s returned empty access_token", clientID)
	}

	expiresIn := tokenResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 300 // default 5 minutes
	}

	expiresAt := now.Add(time.Duration(expiresIn) * time.Second)
	p.cache[clientID] = &cachedToken{
		accessToken: tokenResp.AccessToken,
		expiresAt:   expiresAt,
	}

	return tokenResp.AccessToken, nil
}

// TicketTracker tracks ticket IDs discovered from list_tickets responses.
type TicketTracker struct {
	mu        sync.RWMutex
	ticketIDs []string
}

func NewTicketTracker() *TicketTracker {
	return &TicketTracker{
		ticketIDs: []string{"TCK-1001"},
	}
}

func (t *TicketTracker) UpdateFromTickets(ids []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(ids) == 0 {
		return
	}
	seen := make(map[string]bool)
	var clean []string
	for _, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" && !seen[trimmed] {
			seen[trimmed] = true
			clean = append(clean, trimmed)
		}
	}
	if len(clean) > 0 {
		t.ticketIDs = clean
	}
}

func (t *TicketTracker) GetRandomID(r *rand.Rand) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.ticketIDs) == 0 {
		return "TCK-1001"
	}
	return t.ticketIDs[r.Intn(len(t.ticketIDs))]
}

// Config configures the traffic generator runner.
type Config struct {
	GatewayURL    string
	Clients       map[string]string
	TokenProvider *TokenProvider
	Logger        *JSONLogger
	HTTPClient    *http.Client
	RNG           *rand.Rand
	Tracker       *TicketTracker
	Rate          float64
	Duration      time.Duration
	Clock         ClockFunc
}

// Runner coordinates scenario execution and gateway calls.
type Runner struct {
	cfg     *Config
	tracker *TicketTracker
	rng     *rand.Rand
	clock   ClockFunc
}

func NewRunner(cfg *Config) *Runner {
	if cfg.Tracker == nil {
		cfg.Tracker = NewTicketTracker()
	}
	if cfg.RNG == nil {
		cfg.RNG = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	return &Runner{
		cfg:     cfg,
		tracker: cfg.Tracker,
		rng:     cfg.RNG,
		clock:   cfg.Clock,
	}
}

// ExecuteScenario executes a single weighted scenario against the gateway.
func (rn *Runner) ExecuteScenario(ctx context.Context, sc ScenarioType) {
	var agent, tool string
	var args map[string]any

	ticketID := rn.tracker.GetRandomID(rn.rng)

	switch sc {
	case ScenarioReaderList:
		agent = "agent-reader"
		tool = "list_tickets"
		args = map[string]any{}

	case ScenarioAdminUpdate:
		agent = "agent-admin"
		tool = "update_ticket_status"
		args = map[string]any{
			"ticket_id":  ticketID,
			"new_status": "in_progress",
		}

	case ScenarioReaderDelete:
		agent = "agent-reader"
		tool = "delete_ticket"
		args = map[string]any{
			"ticket_id": ticketID,
		}

	case ScenarioAdminDelete:
		agent = "agent-admin"
		tool = "delete_ticket"
		args = map[string]any{
			"ticket_id": ticketID,
		}

	case ScenarioUnknownTool:
		agent = "agent-reader"
		tool = "execute_arbitrary_shell"
		args = map[string]any{
			"command": "id",
		}

	default:
		agent = "agent-reader"
		tool = "list_tickets"
		args = map[string]any{}
	}

	secret, ok := rn.cfg.Clients[agent]
	if !ok {
		rn.cfg.Logger.Log(LogEntry{
			Time:       rn.clock().UTC().Format(time.RFC3339),
			Agent:      agent,
			Tool:       tool,
			HTTPStatus: 0,
			Outcome:    "ERROR",
		})
		return
	}

	token, err := rn.cfg.TokenProvider.GetToken(ctx, agent, secret)
	if err != nil {
		rn.cfg.Logger.Log(LogEntry{
			Time:       rn.clock().UTC().Format(time.RFC3339),
			Agent:      agent,
			Tool:       tool,
			HTTPStatus: 0,
			Outcome:    "ERROR",
		})
		return
	}

	reqBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      rn.rng.Intn(1000000) + 1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      tool,
			"arguments": args,
		},
	})

	targetURL := rn.cfg.GatewayURL
	if !strings.HasSuffix(targetURL, "/") && !strings.Contains(targetURL[strings.LastIndex(targetURL, "/")+1:], "?") {
		// Ensure trailing slash for root paths if no subpath
		parsed, err := url.Parse(targetURL)
		if err == nil && (parsed.Path == "" || parsed.Path == "/") {
			parsed.Path = "/"
			targetURL = parsed.String()
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(reqBody))
	if err != nil {
		rn.cfg.Logger.Log(LogEntry{
			Time:       rn.clock().UTC().Format(time.RFC3339),
			Agent:      agent,
			Tool:       tool,
			HTTPStatus: 0,
			Outcome:    "ERROR",
		})
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := rn.cfg.HTTPClient.Do(req)
	if err != nil {
		rn.cfg.Logger.Log(LogEntry{
			Time:       rn.clock().UTC().Format(time.RFC3339),
			Agent:      agent,
			Tool:       tool,
			HTTPStatus: 0,
			Outcome:    "ERROR",
		})
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	statusCode := resp.StatusCode
	var outcome string
	switch statusCode {
	case http.StatusOK:
		outcome = "ALLOW"
	case http.StatusForbidden:
		outcome = "DENY"
	default:
		outcome = "ERROR"
	}

	// Update ticket tracker if list_tickets succeeded
	if statusCode == http.StatusOK && tool == "list_tickets" {
		var rpcResp struct {
			Result struct {
				Tickets []struct {
					ID string `json:"id"`
				} `json:"tickets"`
			} `json:"result"`
		}
		if err := json.Unmarshal(bodyBytes, &rpcResp); err == nil && len(rpcResp.Result.Tickets) > 0 {
			var ids []string
			for _, t := range rpcResp.Result.Tickets {
				if t.ID != "" {
					ids = append(ids, t.ID)
				}
			}
			rn.tracker.UpdateFromTickets(ids)
		}
	}

	rn.cfg.Logger.Log(LogEntry{
		Time:       rn.clock().UTC().Format(time.RFC3339),
		Agent:      agent,
		Tool:       tool,
		HTTPStatus: statusCode,
		Outcome:    outcome,
	})
}

// ParseClients parses client specifications:
// format: "agent-reader:<secret-or-secret-file>,agent-admin:<secret-or-secret-file>"
func ParseClients(raw string) (map[string]string, error) {
	clients := make(map[string]string)
	parts := strings.Split(raw, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		clientID, secret, found := strings.Cut(p, ":")
		if !found || clientID == "" {
			return nil, fmt.Errorf("invalid client spec %q; expected clientID:secret", p)
		}
		clientID = strings.TrimSpace(clientID)
		secret = strings.TrimSpace(secret)

		// Check if secret is a readable file path
		if fileBytes, err := os.ReadFile(secret); err == nil {
			clients[clientID] = strings.TrimSpace(string(fileBytes))
		} else {
			clients[clientID] = secret
		}
	}
	return clients, nil
}

// buildHTTPClient constructs an HTTP client, loading root CA if provided.
func buildHTTPClient(caFile string) (*http.Client, error) {
	var rootCAs *x509.CertPool
	if caFile != "" {
		caBytes, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read CA file %s: %w", caFile, err)
		}
		var errSys error
		rootCAs, errSys = x509.SystemCertPool()
		if errSys != nil || rootCAs == nil {
			rootCAs = x509.NewCertPool()
		}
		if !rootCAs.AppendCertsFromPEM(caBytes) {
			return nil, fmt.Errorf("failed to append CA certificate from %s", caFile)
		}
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs: rootCAs,
		},
	}
	return &http.Client{
		Transport: tr,
		Timeout:   10 * time.Second,
	}, nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func main() {
	gatewayURLFlag := flag.String("gateway-url", getEnv("DEMO_AGENT_GATEWAY_URL", "https://agentgateway:3000"), "Gateway base URL")
	tokenURLFlag := flag.String("token-url", getEnv("DEMO_AGENT_TOKEN_URL", "https://keycloak:8443/realms/agentgate/protocol/openid-connect/token"), "Keycloak token endpoint")
	caFileFlag := flag.String("ca-file", getEnv("DEMO_AGENT_CA_FILE", ""), "Path to Root CA certificate file")
	clientsFlag := flag.String("clients", getEnv("DEMO_AGENT_CLIENTS", "agent-reader:/run/secrets/agent_reader_secret,agent-admin:/run/secrets/agent_admin_secret"), "Client specs (id:secret[,id:secret])")

	defaultRateStr := getEnv("DEMO_AGENT_RATE", "1.0")
	defaultRate, _ := strconv.ParseFloat(defaultRateStr, 64)
	if defaultRate <= 0 {
		defaultRate = 1.0
	}
	rateFlag := flag.Float64("rate", defaultRate, "Traffic generation rate in calls/sec")

	defaultDurationStr := getEnv("DEMO_AGENT_DURATION", "0s")
	defaultDuration, _ := time.ParseDuration(defaultDurationStr)
	durationFlag := flag.Duration("duration", defaultDuration, "Duration to run (e.g. 60s, 0 = forever)")

	flag.Parse()

	clients, err := ParseClients(*clientsFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing clients: %v\n", err)
		os.Exit(1)
	}

	httpClient, err := buildHTTPClient(*caFileFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing HTTP client: %v\n", err)
		os.Exit(1)
	}

	tokenProvider := NewTokenProvider(*tokenURLFlag, httpClient, time.Now)
	logger := NewJSONLogger(os.Stdout)
	tracker := NewTicketTracker()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	cfg := &Config{
		GatewayURL:    *gatewayURLFlag,
		Clients:       clients,
		TokenProvider: tokenProvider,
		Logger:        logger,
		HTTPClient:    httpClient,
		RNG:           rng,
		Tracker:       tracker,
		Rate:          *rateFlag,
		Duration:      *durationFlag,
		Clock:         time.Now,
	}

	runner := NewRunner(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *durationFlag > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *durationFlag)
		defer cancel()
	}

	rate := *rateFlag
	if rate <= 0 {
		rate = 1.0
	}
	interval := time.Duration(float64(time.Second) / rate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial immediate call
	rnScenario := SelectScenario(rng)
	runner.ExecuteScenario(ctx, rnScenario)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sc := SelectScenario(rng)
			runner.ExecuteScenario(ctx, sc)
		}
	}
}
