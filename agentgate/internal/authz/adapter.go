package authz

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Dynamisch-LLC/agentgate/internal/argdecl"
	"github.com/Dynamisch-LLC/agentgate/internal/contextassembly"
	"github.com/Dynamisch-LLC/agentgate/internal/decision"
	"github.com/Dynamisch-LLC/agentgate/internal/identity"
	"github.com/Dynamisch-LLC/agentgate/internal/toolregistry"
	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	"google.golang.org/protobuf/types/known/structpb"
)

// Adapter translates an Envoy v3 ext_authz CheckRequest into a frozen decision.Request.
type Adapter struct {
	cfg AdapterConfig
}

// TrustedWorkspaceResolver resolves workspace identity strictly from verified JWT claims
// or trusted gateway routing context (never from unauthenticated client headers).
type TrustedWorkspaceResolver struct {
	AllowStaticFallback bool
	StaticWorkspaceID   string
}

// NewTrustedWorkspaceResolver constructs a TrustedWorkspaceResolver.
func NewTrustedWorkspaceResolver(allowStaticFallback bool, staticWorkspaceID string) *TrustedWorkspaceResolver {
	return &TrustedWorkspaceResolver{
		AllowStaticFallback: allowStaticFallback,
		StaticWorkspaceID:   staticWorkspaceID,
	}
}

// ResolveWorkspace extracts workspace identity from verified JWT claims or gateway context extensions.
func (r *TrustedWorkspaceResolver) ResolveWorkspace(ctx context.Context, checkReq *authv3.CheckRequest, claims map[string]string) (string, error) {
	// 1. Check verified JWT claims (cryptographically validated by gateway)
	if ws, ok := claims["workspace_id"]; ok && strings.TrimSpace(ws) != "" {
		return strings.TrimSpace(ws), nil
	}
	if ws, ok := claims["workspace"]; ok && strings.TrimSpace(ws) != "" {
		return strings.TrimSpace(ws), nil
	}

	// 2. Check trusted gateway route context extensions (server-side route config)
	if checkReq != nil && checkReq.Attributes != nil && checkReq.Attributes.ContextExtensions != nil {
		if ws, ok := checkReq.Attributes.ContextExtensions["workspace_id"]; ok && strings.TrimSpace(ws) != "" {
			return strings.TrimSpace(ws), nil
		}
	}

	// 3. Controlled static fallback if explicitly enabled for single-workspace deployments
	if r.AllowStaticFallback && strings.TrimSpace(r.StaticWorkspaceID) != "" {
		return strings.TrimSpace(r.StaticWorkspaceID), nil
	}

	return "", fmt.Errorf("trusted workspace identity could not be resolved from JWT claims or gateway routing context")
}

// NewAdapter constructs a new Adapter instance.
func NewAdapter(cfg AdapterConfig) *Adapter {
	if cfg.DefaultWorkspaceID == "" {
		cfg.DefaultWorkspaceID = "default"
	}
	if cfg.WorkspaceResolver == nil {
		cfg.WorkspaceResolver = NewTrustedWorkspaceResolver(cfg.AllowStaticWorkspace, cfg.DefaultWorkspaceID)
	}
	if cfg.DefaultBackendID == "" {
		cfg.DefaultBackendID = "default"
	}
	if cfg.ArgDeclarations == nil {
		cfg.ArgDeclarations = make(map[string]*argdecl.DeclarationSet)
	}
	return &Adapter{cfg: cfg}
}

// Adapt processes an ext_authz CheckRequest, validating caller identity,
// MCP JSON-RPC structure, tool governance classification, and declared arguments.
func (a *Adapter) Adapt(ctx context.Context, checkReq *authv3.CheckRequest) (decision.Request, *AdapterError) {
	if checkReq == nil || checkReq.Attributes == nil || checkReq.Attributes.Request == nil || checkReq.Attributes.Request.Http == nil {
		return decision.Request{}, &AdapterError{
			ReasonCode: decision.ReasonMalformedRequest,
			Message:    "missing http request context in ext_authz check",
		}
	}

	registry := a.cfg.ToolRegistry
	argDecls := a.cfg.ArgDeclarations
	byToolIDOnly := false
	if a.cfg.Catalog != nil {
		snap := a.cfg.Catalog.Current()
		if snap == nil {
			return decision.Request{}, &AdapterError{
				ReasonCode: decision.ReasonUnknownTool,
				Message:    "tool catalog not available",
			}
		}
		registry = snap.Registry
		argDecls = snap.ArgDecls
		byToolIDOnly = true
	}

	httpReq := checkReq.Attributes.Request.Http
	headers := httpReq.Headers
	if headers == nil {
		headers = make(map[string]string)
	}

	// 1. Extract Body
	var bodyBytes []byte
	if len(httpReq.RawBody) > 0 {
		bodyBytes = httpReq.RawBody
	} else if len(httpReq.Body) > 0 {
		bodyBytes = []byte(httpReq.Body)
	}

	if len(bodyBytes) == 0 {
		return decision.Request{}, &AdapterError{
			ReasonCode: decision.ReasonMalformedRequest,
			Message:    "missing request body in ext_authz check",
		}
	}

	// 2. Parse JSON-RPC MCP structure
	var rpcReq JSONRPCRequest
	if err := json.Unmarshal(bodyBytes, &rpcReq); err != nil {
		return decision.Request{}, &AdapterError{
			ReasonCode: decision.ReasonMalformedRequest,
			Message:    "malformed JSON-RPC body",
			Err:        err,
		}
	}

	if rpcReq.Method != "tools/call" {
		return decision.Request{}, &AdapterError{
			ReasonCode: decision.ReasonMalformedRequest,
			Message:    fmt.Sprintf("unsupported MCP method %q (only tools/call is authorized)", rpcReq.Method),
		}
	}

	if len(rpcReq.Params) == 0 {
		return decision.Request{}, &AdapterError{
			ReasonCode: decision.ReasonMalformedRequest,
			Message:    "missing params in tools/call request",
		}
	}

	var toolParams ToolCallParams
	if err := json.Unmarshal(rpcReq.Params, &toolParams); err != nil {
		return decision.Request{}, &AdapterError{
			ReasonCode: decision.ReasonMalformedRequest,
			Message:    "malformed tools/call params",
			Err:        err,
		}
	}

	toolName := strings.TrimSpace(toolParams.Name)
	if toolName == "" {
		return decision.Request{}, &AdapterError{
			ReasonCode: decision.ReasonMalformedRequest,
			Message:    "empty tool name in tools/call params",
		}
	}

	// 3. Extract Identity Claims (strictly from verified JWT filter metadata)
	claims := a.extractClaims(checkReq)
	if a.cfg.IdentityMapper == nil {
		return decision.Request{}, &AdapterError{
			ReasonCode: decision.ReasonInvalidIdentity,
			Message:    "identity mapper not configured",
		}
	}

	mappedIdentity, merr := a.cfg.IdentityMapper.Map(claims)
	if merr != nil {
		return decision.Request{}, &AdapterError{
			ReasonCode: decision.ReasonInvalidIdentity,
			Message:    merr.Error(),
			Err:        merr,
		}
	}

	// 4. Resolve WorkspaceID strictly from trusted claims/context
	workspaceID, werr := a.cfg.WorkspaceResolver.ResolveWorkspace(ctx, checkReq, claims)
	if werr != nil {
		return decision.Request{}, &AdapterError{
			ReasonCode: decision.ReasonInvalidIdentity,
			Message:    fmt.Sprintf("trusted workspace resolution failed: %v", werr),
			Err:        werr,
		}
	}

	executionID := getHeader(headers, "x-execution-id")
	if executionID == "" {
		executionID = getHeader(headers, "x-request-id")
	}
	if executionID == "" {
		executionID = generateExecutionID()
	}

	// 5. Tool Registry Lookup & Governance
	backendID := getHeader(headers, "x-agentgate-backend-id")
	if backendID == "" && checkReq.Attributes.ContextExtensions != nil {
		backendID = checkReq.Attributes.ContextExtensions["backend_id"]
	}
	if backendID == "" {
		backendID = a.cfg.DefaultBackendID
	}

	toolID := toolregistry.ToolID{
		BackendID: backendID,
		ToolName:  toolName,
	}

	if registry == nil {
		return decision.Request{}, &AdapterError{
			ReasonCode: decision.ReasonUnknownTool,
			Message:    "tool registry not configured",
		}
	}

	liveFP := toolregistry.SchemaFingerprint(getHeader(headers, "x-tool-fingerprint"))
	if liveFP == "" {
		liveFP = toolregistry.SchemaFingerprint(getHeader(headers, "x-agentgate-tool-fingerprint"))
	}
	if liveFP == "" && checkReq.Attributes.ContextExtensions != nil {
		liveFP = toolregistry.SchemaFingerprint(checkReq.Attributes.ContextExtensions["tool_fingerprint"])
	}

	govRecord := registry.Lookup(toolID, liveFP)
	if !govRecord.Known {
		return decision.Request{}, &AdapterError{
			ReasonCode: decision.ReasonUnknownTool,
			Message:    fmt.Sprintf("tool %s is not registered in governance catalog", toolID),
		}
	}

	if govRecord.DriftStatus == toolregistry.DriftDetected {
		return decision.Request{}, &AdapterError{
			ReasonCode: decision.ReasonUnknownTool,
			Message:    fmt.Sprintf("tool %s schema drift detected", toolID),
		}
	}

	if err := govRecord.Validate(); err != nil {
		return decision.Request{}, &AdapterError{
			ReasonCode: decision.ReasonUnknownTool,
			Message:    err.Error(),
			Err:        err,
		}
	}

	// 6. Argument Resolution
	var resolvedArgs map[string]argdecl.ResolvedArg
	var declSet *argdecl.DeclarationSet
	if ds, ok := argDecls[toolID.String()]; ok {
		declSet = ds
	} else if ds, ok := argDecls[toolName]; ok && !byToolIDOnly {
		declSet = ds
	}

	if declSet != nil {
		rawArgs := make(map[string]argdecl.RawArgValue, len(toolParams.Arguments))
		for k, v := range toolParams.Arguments {
			rawArgs[k] = v
		}

		var rerr *argdecl.ResolutionError
		resolvedArgs, rerr = declSet.Resolve(rawArgs)
		if rerr != nil {
			return decision.Request{}, &AdapterError{
				ReasonCode: decision.ReasonMalformedRequest,
				Message:    rerr.Error(),
				Err:        rerr,
			}
		}
	}

	// 7. Context Assembly into frozen decision.Request
	assemblyIn := contextassembly.AssemblyInput{
		ExecutionID:      executionID,
		WorkspaceID:      workspaceID,
		MappedIdentity:   mappedIdentity,
		GovernanceRecord: govRecord,
		ResolvedArgs:     resolvedArgs,
	}

	decisionReq, assemErr := contextassembly.Assemble(assemblyIn)
	if assemErr != nil {
		return decision.Request{}, &AdapterError{
			ReasonCode: decision.ReasonMalformedRequest,
			Message:    assemErr.Error(),
			Err:        assemErr,
		}
	}

	return decisionReq, nil
}

func (a *Adapter) extractClaims(checkReq *authv3.CheckRequest) map[string]string {
	claims := make(map[string]string)

	// Read claims ONLY from Envoy JWT filter metadata populated after cryptographic verification
	if checkReq != nil && checkReq.Attributes != nil && checkReq.Attributes.MetadataContext != nil && checkReq.Attributes.MetadataContext.FilterMetadata != nil {
		if jwtMetadata, ok := checkReq.Attributes.MetadataContext.FilterMetadata["envoy.filters.http.jwt_authn"]; ok && jwtMetadata != nil {
			// The pinned agentgateway:v1.4.0 nests the actual JWT payload one
			// level deeper, under a "jwt_payload" field — found and confirmed
			// by live diagnostic (G7 Task A): a struct like
			// {"jwt_payload":{"sub":"...","roles":"...", ...}}, not the claims
			// flat at the top level as this code previously assumed. That
			// assumption was never exercised against a real JWT before —
			// deploy/g6/agentgateway.yaml had no jwtAuth block at all until
			// this same checkpoint added one, and the original manual G6
			// evidence capture used the always-allow probe-authz stub
			// (docs/PHASES/G6_WORKSTREAMS/G6_GATEWAY_CONTRACT_OBSERVED.json
			// shows "metadata": {} — empty), not a real JWT flow. Falls back
			// to reading fields flat, for forward-compatibility if agentgateway
			// or its config ever changes to emit claims at the top level.
			fields := jwtMetadata.Fields
			if payload, ok := fields["jwt_payload"]; ok && payload != nil {
				if payloadStruct := payload.GetStructValue(); payloadStruct != nil {
					fields = payloadStruct.Fields
				}
			}
			for k, v := range fields {
				claims[k] = structpbValueToString(v)
			}
		}
	}

	// NOTE (G6 Security Boundary): Unverified client headers (e.g. x-agent-id, x-roles, Authorization)
	// MUST NEVER be trusted as authenticated identity. Only gateway-authenticated JWT claims are authoritative.

	return claims
}

func structpbValueToString(v *structpb.Value) string {
	if v == nil {
		return ""
	}
	switch k := v.Kind.(type) {
	case *structpb.Value_StringValue:
		return k.StringValue
	case *structpb.Value_NumberValue:
		return fmt.Sprintf("%v", k.NumberValue)
	case *structpb.Value_BoolValue:
		if k.BoolValue {
			return "true"
		}
		return "false"
	case *structpb.Value_ListValue:
		if k.ListValue == nil {
			return ""
		}
		var parts []string
		for _, item := range k.ListValue.Values {
			if s := structpbValueToString(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, identity.RolesSeparator)
	default:
		return ""
	}
}

func getHeader(headers map[string]string, key string) string {
	if v, ok := headers[key]; ok {
		return v
	}
	lowKey := strings.ToLower(key)
	for k, v := range headers {
		if strings.ToLower(k) == lowKey {
			return v
		}
	}
	return ""
}

func generateExecutionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("exec-%d-%s", time.Now().UnixNano(), hex.EncodeToString(b))
}
