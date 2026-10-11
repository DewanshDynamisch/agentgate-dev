package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

// ToolResult represents the parsed tool execution result.
type ToolResult struct {
	Content []map[string]any `json:"content,omitempty"`
	IsError bool             `json:"isError,omitempty"`
	Raw     string           `json:"raw"`
}

// Client is a minimal MCP client for contract probes.
type Client struct {
	BaseURL      string
	Token        string
	AgentID      string
	Roles        string
	OnBehalfOf   string
	ExtraHeaders map[string]string
	HTTPClient   *http.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		BaseURL:      baseURL,
		Token:        token,
		ExtraHeaders: make(map[string]string),
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Client) sendRaw(body []byte) (int, []byte, error) {
	req, err := http.NewRequest("POST", c.BaseURL, bytes.NewBuffer(body))
	if err != nil {
		return 0, nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.AgentID != "" {
		req.Header.Set("x-agent-id", c.AgentID)
	}
	if c.Roles != "" {
		req.Header.Set("x-roles", c.Roles)
	}
	if c.OnBehalfOf != "" {
		req.Header.Set("x-on-behalf-of", c.OnBehalfOf)
	}
	for k, v := range c.ExtraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("failed to read response: %w", err)
	}

	return resp.StatusCode, respBytes, nil
}

func (c *Client) Initialize() error {
	reqBody, _ := json.Marshal(jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2026-07-28",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "g6-probe-client",
				"version": "1.0.0",
			},
		},
	})

	statusCode, respBytes, err := c.sendRaw(reqBody)
	if err != nil {
		return err
	}
	if statusCode != http.StatusOK {
		return fmt.Errorf("initialize failed with status %d: %s", statusCode, string(respBytes))
	}

	var resp jsonrpcResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return fmt.Errorf("failed to parse initialize response: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("initialize returned error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return nil
}

func (c *Client) CallTool(name string, args map[string]any) (*ToolResult, error) {
	reqBody, _ := json.Marshal(jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/call",
		Params: map[string]any{
			"name":      name,
			"arguments": args,
		},
	})

	statusCode, respBytes, err := c.sendRaw(reqBody)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("call tool failed with status %d: %s", statusCode, string(respBytes))
	}

	var resp jsonrpcResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse tool response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("tool call returned error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	var parsedResult struct {
		Content []map[string]any `json:"content"`
		IsError bool             `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &parsedResult); err != nil {
		return nil, fmt.Errorf("failed to parse tool result: %w", err)
	}

	return &ToolResult{
		Content: parsedResult.Content,
		IsError: parsedResult.IsError,
		Raw:     string(respBytes),
	}, nil
}

func main() {
	urlFlag := flag.String("url", "http://localhost:3000", "Base MCP URL")
	tokenFlag := flag.String("token", "", "Bearer token")
	agentIDFlag := flag.String("agent-id", "agent-reader", "Caller Agent ID")
	rolesFlag := flag.String("roles", "reader", "Caller Roles")
	oboFlag := flag.String("obo", "", "On-behalf-of human identity")
	toolFlag := flag.String("tool", "read_status", "Tool to invoke")
	argsFlag := flag.String("args", "{}", "Tool arguments JSON")
	rawFlag := flag.String("raw", "", "Raw JSON payload to send directly")
	skipInitFlag := flag.Bool("skip-init", false, "Skip initialize handshake")
	printTokenFlag := flag.Bool("print-token", false, "Print the inbound bearer token to stderr (test evidence only)")
	flag.Parse()

	token := *tokenFlag
	// AgentGate trusts identity only from a gateway-verified JWT, never
	// from plain headers (docs/SECURITY/PRODUCTION-INVARIANTS.md) — mint
	// one from the same agent-id/roles/obo flags this client already
	// accepted, unless the caller supplied a real token explicitly.
	if token == "" && *agentIDFlag != "" {
		token = mintJWT(*agentIDFlag, *rolesFlag, *oboFlag)
	}

	if *printTokenFlag {
		fmt.Fprintln(os.Stderr, "INBOUND_TOKEN="+token)
	}

	client := NewClient(*urlFlag, token)
	client.AgentID = *agentIDFlag
	client.Roles = *rolesFlag
	client.OnBehalfOf = *oboFlag

	if *rawFlag != "" {
		code, resp, err := client.sendRaw([]byte(*rawFlag))
		if err != nil {
			log.Fatalf("raw request failed: %v", err)
		}
		fmt.Printf("HTTP %d\n%s\n", code, string(resp))
		return
	}

	if !*skipInitFlag {
		if err := client.Initialize(); err != nil {
			log.Fatalf("initialize failed: %v", err)
		}
	}

	var args map[string]any
	if *argsFlag != "" && *argsFlag != "{}" {
		if err := json.Unmarshal([]byte(*argsFlag), &args); err != nil {
			log.Fatalf("invalid args JSON: %v (input: %s)", err, *argsFlag)
		}
	} else {
		args = map[string]any{"verbose": true}
	}

	result, err := client.CallTool(*toolFlag, args)
	if err != nil {
		log.Fatalf("tool call failed: %v", err)
	}

	fmt.Printf("Tool Call Succeeded:\n%s\n", result.Raw)
}
