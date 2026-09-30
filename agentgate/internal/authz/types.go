package authz

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Dynamisch-LLC/agentgate/internal/argdecl"
	"github.com/Dynamisch-LLC/agentgate/internal/decision"
	"github.com/Dynamisch-LLC/agentgate/internal/identity"
	"github.com/Dynamisch-LLC/agentgate/internal/toolcatalog"
	"github.com/Dynamisch-LLC/agentgate/internal/toolregistry"
	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
)

// JSONRPCRequest models an incoming JSON-RPC 2.0 request payload from the MCP client.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// ToolCallParams models the parameters of an MCP "tools/call" method.
type ToolCallParams struct {
	Name      string                     `json:"name"`
	Arguments map[string]json.RawMessage `json:"arguments,omitempty"`
}

// WorkspaceResolver resolves a trusted workspace ID from verified JWT claims or gateway routing context.
type WorkspaceResolver interface {
	ResolveWorkspace(ctx context.Context, checkReq *authv3.CheckRequest, claims map[string]string) (string, error)
}

// ToolCatalog supplies the live tool snapshot. Adapt takes exactly one
// Current() per request.
type ToolCatalog interface {
	Current() *toolcatalog.Snapshot
}

// AdapterConfig configures the ext_authz to decision.Request adapter.
type AdapterConfig struct {
	DefaultWorkspaceID   string
	AllowStaticWorkspace bool // If true, allows fallback to DefaultWorkspaceID when JWT/route context omits workspace.
	WorkspaceResolver    WorkspaceResolver
	DefaultBackendID     string
	IdentityMapper       *identity.Mapper
	ToolRegistry         *toolregistry.Registry
	ArgDeclarations      map[string]*argdecl.DeclarationSet
	// Catalog, when non-nil, takes precedence over ToolRegistry and
	// ArgDeclarations; declarations are keyed backendID/toolName.
	Catalog ToolCatalog
}

// AdapterError describes why an ext_authz check cannot be translated into a valid decision.Request.
type AdapterError struct {
	ReasonCode decision.ReasonCode
	Message    string
	Err        error
}

func (e *AdapterError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("authz adapter error (%s): %s: %v", e.ReasonCode, e.Message, e.Err)
	}
	return fmt.Sprintf("authz adapter error (%s): %s", e.ReasonCode, e.Message)
}

func (e *AdapterError) Unwrap() error {
	return e.Err
}
