package authz

import (
	"context"
	"testing"

	"github.com/Dynamisch-LLC/agentgate/internal/argdecl"
	"github.com/Dynamisch-LLC/agentgate/internal/decision"
	"github.com/Dynamisch-LLC/agentgate/internal/identity"
	"github.com/Dynamisch-LLC/agentgate/internal/toolregistry"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	"google.golang.org/protobuf/types/known/structpb"
)

func setupTestAdapter(t *testing.T) *Adapter {
	t.Helper()

	mapper, err := identity.NewMapper(identity.MapperConfig{
		AgentIDClaim:    "sub",
		RolesClaim:      "roles",
		OnBehalfOfClaim: "obo",
	})
	if err != nil {
		t.Fatalf("NewMapper failed: %v", err)
	}

	readStatusSchema := []byte(`{"type":"object","properties":{"verbose":{"type":"boolean"}}}`)
	readStatusFP, err := toolregistry.FingerprintSchema(readStatusSchema)
	if err != nil {
		t.Fatalf("FingerprintSchema failed: %v", err)
	}

	reg, err := toolregistry.NewRegistry([]toolregistry.RegistryEntry{
		{
			ToolID: toolregistry.ToolID{
				BackendID: "mcp-probe",
				ToolName:  "read_status",
			},
			Risk:                  toolregistry.RiskRead,
			RegisteredFingerprint: readStatusFP,
		},
		{
			ToolID: toolregistry.ToolID{
				BackendID: "mcp-probe",
				ToolName:  "transfer_funds",
			},
			Risk:                  toolregistry.RiskDestructive,
			RegisteredFingerprint: readStatusFP,
		},
	})
	if err != nil {
		t.Fatalf("NewRegistry failed: %v", err)
	}

	readStatusDecls, err := argdecl.NewDeclarationSet([]argdecl.Declaration{
		{
			Name:     "verbose",
			Type:     argdecl.ArgTypeBool,
			Required: false,
		},
	})
	if err != nil {
		t.Fatalf("NewDeclarationSet failed: %v", err)
	}

	transferDecls, err := argdecl.NewDeclarationSet([]argdecl.Declaration{
		{
			Name:     "amount",
			Type:     argdecl.ArgTypeInt,
			Required: true,
		},
	})
	if err != nil {
		t.Fatalf("NewDeclarationSet failed: %v", err)
	}

	return NewAdapter(AdapterConfig{
		DefaultWorkspaceID:   "ws-test",
		AllowStaticWorkspace: true,
		DefaultBackendID:     "mcp-probe",
		IdentityMapper:       mapper,
		ToolRegistry:         reg,
		ArgDeclarations: map[string]*argdecl.DeclarationSet{
			"read_status":    readStatusDecls,
			"transfer_funds": transferDecls,
		},
	})
}

func newValidJWTMetadata(t *testing.T, sub string, roles string, obo string, extra ...map[string]any) *corev3.Metadata {
	t.Helper()
	m := map[string]any{
		"sub":   sub,
		"roles": roles,
	}
	if obo != "" {
		m["obo"] = obo
	}
	for _, ext := range extra {
		for k, v := range ext {
			m[k] = v
		}
	}
	s, err := structpb.NewStruct(m)
	if err != nil {
		t.Fatalf("failed to create structpb: %v", err)
	}
	return &corev3.Metadata{
		FilterMetadata: map[string]*structpb.Struct{
			"envoy.filters.http.jwt_authn": s,
		},
	}
}

func TestAdapter_ValidToolCall_FromJWT(t *testing.T) {
	adapter := setupTestAdapter(t)

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Method: "POST",
					Path:   "/",
					Headers: map[string]string{
						"x-execution-id": "exec-12345",
					},
					Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_status","arguments":{"verbose":true}}}`,
				},
			},
			MetadataContext: newValidJWTMetadata(t, "agent-007", "reader auditor", "user-corp", map[string]any{
				"workspace_id": "ws-custom",
			}),
		},
	}

	decisionReq, aerr := adapter.Adapt(context.Background(), req)
	if aerr != nil {
		t.Fatalf("unexpected adapter error: %v", aerr)
	}

	if decisionReq.WorkspaceID != "ws-custom" {
		t.Errorf("expected workspace 'ws-custom', got %q", decisionReq.WorkspaceID)
	}
	if decisionReq.ExecutionID != "exec-12345" {
		t.Errorf("expected execution ID 'exec-12345', got %q", decisionReq.ExecutionID)
	}
	if decisionReq.Identity.AgentID != "agent-007" {
		t.Errorf("expected agent 'agent-007', got %q", decisionReq.Identity.AgentID)
	}
	if decisionReq.Identity.OnBehalfOf != "user-corp" {
		t.Errorf("expected on-behalf-of 'user-corp', got %q", decisionReq.Identity.OnBehalfOf)
	}
	if len(decisionReq.Identity.Roles) != 2 || decisionReq.Identity.Roles[0] != "reader" || decisionReq.Identity.Roles[1] != "auditor" {
		t.Errorf("unexpected roles: %v", decisionReq.Identity.Roles)
	}
	if decisionReq.Tool.Name != "read_status" || decisionReq.Tool.BackendID != "mcp-probe" {
		t.Errorf("unexpected tool: %+v", decisionReq.Tool)
	}
	if !decisionReq.Classification.Known || decisionReq.Classification.Risk != "read" {
		t.Errorf("unexpected classification: %+v", decisionReq.Classification)
	}
	if val, ok := decisionReq.Arguments["verbose"]; !ok || val.String() != "true" {
		t.Errorf("expected argument verbose=true, got %v", decisionReq.Arguments)
	}
}

// TestAdapter_RejectUnverifiedHeaders proves that client-provided headers (x-agent-id, Authorization)
// are strictly rejected when verified gateway metadata is absent.
func TestAdapter_RejectUnverifiedHeaders(t *testing.T) {
	adapter := setupTestAdapter(t)

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Method: "POST",
					Path:   "/",
					Headers: map[string]string{
						"x-agent-id":    "attacker-agent",
						"x-roles":       "admin",
						"authorization": "Bearer forged-token",
					},
					RawBody: []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_status","arguments":{"verbose":true}}}`),
				},
			},
		},
	}

	_, aerr := adapter.Adapt(context.Background(), req)
	if aerr == nil {
		t.Fatal("security defect: expected failure when relying on unverified headers, got nil")
	}
	if aerr.ReasonCode != decision.ReasonInvalidIdentity {
		t.Errorf("expected ReasonInvalidIdentity, got %s", aerr.ReasonCode)
	}
}

func TestAdapter_WorkspaceResolution_FromJWT(t *testing.T) {
	adapter := setupTestAdapter(t)

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Method: "POST",
					Body:   `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_status"}}`,
				},
			},
			MetadataContext: newValidJWTMetadata(t, "agent-001", "reader", "", map[string]any{
				"workspace_id": "ws-from-jwt",
			}),
		},
	}

	decisionReq, aerr := adapter.Adapt(context.Background(), req)
	if aerr != nil {
		t.Fatalf("unexpected adapter error: %v", aerr)
	}
	if decisionReq.WorkspaceID != "ws-from-jwt" {
		t.Errorf("expected workspace 'ws-from-jwt', got %q", decisionReq.WorkspaceID)
	}
}

func TestAdapter_WorkspaceResolution_FromContextExtensions(t *testing.T) {
	adapter := setupTestAdapter(t)

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Method: "POST",
					Body:   `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_status"}}`,
				},
			},
			ContextExtensions: map[string]string{
				"workspace_id": "ws-from-route-ext",
			},
			MetadataContext: newValidJWTMetadata(t, "agent-001", "reader", ""),
		},
	}

	decisionReq, aerr := adapter.Adapt(context.Background(), req)
	if aerr != nil {
		t.Fatalf("unexpected adapter error: %v", aerr)
	}
	if decisionReq.WorkspaceID != "ws-from-route-ext" {
		t.Errorf("expected workspace 'ws-from-route-ext', got %q", decisionReq.WorkspaceID)
	}
}

func TestAdapter_WorkspaceResolution_ClientHeaderIgnored(t *testing.T) {
	adapter := setupTestAdapter(t)

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Method: "POST",
					Headers: map[string]string{
						"x-agentgate-workspace-id": "forged-workspace",
					},
					Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_status"}}`,
				},
			},
			MetadataContext: newValidJWTMetadata(t, "agent-001", "reader", "", map[string]any{
				"workspace_id": "trusted-workspace",
			}),
		},
	}

	decisionReq, aerr := adapter.Adapt(context.Background(), req)
	if aerr != nil {
		t.Fatalf("unexpected adapter error: %v", aerr)
	}
	if decisionReq.WorkspaceID != "trusted-workspace" {
		t.Errorf("security defect: expected trusted-workspace, got %q (client header was trusted)", decisionReq.WorkspaceID)
	}
}

func TestAdapter_WorkspaceResolution_FailsClosed_WithoutStaticFallback(t *testing.T) {
	cfg := AdapterConfig{
		DefaultWorkspaceID:   "ws-test",
		AllowStaticWorkspace: false, // Strict multi-tenant mode
		IdentityMapper:       setupTestAdapter(t).cfg.IdentityMapper,
		ToolRegistry:         setupTestAdapter(t).cfg.ToolRegistry,
		ArgDeclarations:      setupTestAdapter(t).cfg.ArgDeclarations,
	}
	adapter := NewAdapter(cfg)

	// No workspace in JWT, no route context extensions
	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Method: "POST",
					Body:   `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_status"}}`,
				},
			},
			MetadataContext: newValidJWTMetadata(t, "agent-001", "reader", ""),
		},
	}

	_, aerr := adapter.Adapt(context.Background(), req)
	if aerr == nil {
		t.Fatal("expected failure when workspace cannot be resolved and static fallback disabled, got nil")
	}
	if aerr.ReasonCode != decision.ReasonInvalidIdentity {
		t.Errorf("expected ReasonInvalidIdentity, got %s", aerr.ReasonCode)
	}
}

func TestAdapter_MissingIdentity(t *testing.T) {
	adapter := setupTestAdapter(t)

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_status"}}`,
				},
			},
		},
	}

	_, aerr := adapter.Adapt(context.Background(), req)
	if aerr == nil {
		t.Fatal("expected adapter error for missing identity, got nil")
	}
	if aerr.ReasonCode != decision.ReasonInvalidIdentity {
		t.Errorf("expected ReasonInvalidIdentity, got %s", aerr.ReasonCode)
	}
}

func TestAdapter_AmbiguousIdentity(t *testing.T) {
	adapter := setupTestAdapter(t)

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_status"}}`,
				},
			},
			MetadataContext: newValidJWTMetadata(t, "agent-same", "reader", "agent-same"),
		},
	}

	_, aerr := adapter.Adapt(context.Background(), req)
	if aerr == nil {
		t.Fatal("expected error for ambiguous identity (on_behalf_of == agent_id), got nil")
	}
	if aerr.ReasonCode != decision.ReasonInvalidIdentity {
		t.Errorf("expected ReasonInvalidIdentity, got %s", aerr.ReasonCode)
	}
}

func TestAdapter_NonToolCall(t *testing.T) {
	adapter := setupTestAdapter(t)

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Body: `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2026-07-28"}}`,
				},
			},
			MetadataContext: newValidJWTMetadata(t, "agent-001", "reader", ""),
		},
	}

	_, aerr := adapter.Adapt(context.Background(), req)
	if aerr == nil {
		t.Fatal("expected error for non-tool call, got nil")
	}
	if aerr.ReasonCode != decision.ReasonMalformedRequest {
		t.Errorf("expected ReasonMalformedRequest, got %s", aerr.ReasonCode)
	}
}

func TestAdapter_MalformedJSON(t *testing.T) {
	adapter := setupTestAdapter(t)

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params": BROKEN JSON`,
				},
			},
			MetadataContext: newValidJWTMetadata(t, "agent-001", "reader", ""),
		},
	}

	_, aerr := adapter.Adapt(context.Background(), req)
	if aerr == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	if aerr.ReasonCode != decision.ReasonMalformedRequest {
		t.Errorf("expected ReasonMalformedRequest, got %s", aerr.ReasonCode)
	}
}

func TestAdapter_UnknownTool(t *testing.T) {
	adapter := setupTestAdapter(t)

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete_everything"}}`,
				},
			},
			MetadataContext: newValidJWTMetadata(t, "agent-001", "reader", ""),
		},
	}

	_, aerr := adapter.Adapt(context.Background(), req)
	if aerr == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
	if aerr.ReasonCode != decision.ReasonUnknownTool {
		t.Errorf("expected ReasonUnknownTool, got %s", aerr.ReasonCode)
	}
}

func TestAdapter_InvalidArguments(t *testing.T) {
	adapter := setupTestAdapter(t)

	// transfer_funds requires amount as int64. Passing string "one_million" should fail.
	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"transfer_funds","arguments":{"amount":"one_million"}}}`,
				},
			},
			MetadataContext: newValidJWTMetadata(t, "agent-001", "admin", ""),
		},
	}

	_, aerr := adapter.Adapt(context.Background(), req)
	if aerr == nil {
		t.Fatal("expected error for invalid argument type, got nil")
	}
	if aerr.ReasonCode != decision.ReasonMalformedRequest {
		t.Errorf("expected ReasonMalformedRequest, got %s", aerr.ReasonCode)
	}
}

func TestAdapter_UndeclaredArgumentsIgnored(t *testing.T) {
	adapter := setupTestAdapter(t)

	// read_status only declares verbose. Passing extra undeclared argument should not reach decision.Arguments.
	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_status","arguments":{"verbose":true,"exploit_payload":"malicious"}}}`,
				},
			},
			MetadataContext: newValidJWTMetadata(t, "agent-001", "reader", ""),
		},
	}

	decisionReq, aerr := adapter.Adapt(context.Background(), req)
	if aerr != nil {
		t.Fatalf("unexpected adapter error: %v", aerr)
	}

	if _, ok := decisionReq.Arguments["exploit_payload"]; ok {
		t.Errorf("security defect: undeclared argument 'exploit_payload' leaked to decision.Arguments: %v", decisionReq.Arguments)
	}
	if val, ok := decisionReq.Arguments["verbose"]; !ok || val.String() != "true" {
		t.Errorf("expected verbose=true, got %v", decisionReq.Arguments)
	}
}

func TestAdapter_ListRolesClaim(t *testing.T) {
	adapter := setupTestAdapter(t)

	s, err := structpb.NewStruct(map[string]any{
		"sub":   "agent-001",
		"roles": []any{"reader", "admin"},
	})
	if err != nil {
		t.Fatalf("failed to create structpb: %v", err)
	}
	metadata := &corev3.Metadata{
		FilterMetadata: map[string]*structpb.Struct{
			"envoy.filters.http.jwt_authn": s,
		},
	}

	req := &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Method: "POST",
					Path:   "/",
					Body:   `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_status","arguments":{"verbose":true}}}`,
				},
			},
			MetadataContext: metadata,
		},
	}

	decisionReq, aerr := adapter.Adapt(context.Background(), req)
	if aerr != nil {
		t.Fatalf("unexpected adapter error: %v", aerr)
	}

	if decisionReq.Identity.AgentID != "agent-001" {
		t.Errorf("expected AgentID 'agent-001', got %q", decisionReq.Identity.AgentID)
	}
	expectedRoles := []string{"reader", "admin"}
	if len(decisionReq.Identity.Roles) != len(expectedRoles) {
		t.Fatalf("expected roles %v, got %v", expectedRoles, decisionReq.Identity.Roles)
	}
	for i, r := range expectedRoles {
		if decisionReq.Identity.Roles[i] != r {
			t.Errorf("role[%d]: expected %q, got %q", i, r, decisionReq.Identity.Roles[i])
		}
	}
}
