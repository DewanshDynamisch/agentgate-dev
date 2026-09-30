package govapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Dynamisch-LLC/agentgate/internal/argdecl"
	"github.com/Dynamisch-LLC/agentgate/internal/auditevents"
	"github.com/Dynamisch-LLC/agentgate/internal/toolcatalog"
	"github.com/Dynamisch-LLC/agentgate/internal/toolregistry"
)

const (
	toolBodyLimit = 1 << 20

	// catalogAuditWorkspace is the audit chain that receives tool catalog
	// mutation events; the catalog is deployment-wide, not workspace-scoped.
	catalogAuditWorkspace = "default"
	catalogOperatorID     = "admin"
)

// ToolCatalogService bundles the runtime tool catalog dependencies.
// Listener may be nil, in which case mutations are not audited.
type ToolCatalogService struct {
	Store    toolcatalog.Store
	Catalog  *toolcatalog.Catalog
	Listener auditevents.MutationListener
}

func (s ToolCatalogService) configured() bool {
	return s.Store != nil && s.Catalog != nil
}

// ToolArgumentView is one policy-relevant argument declaration.
type ToolArgumentView struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// ToolDefinitionView is the operator-facing catalog entry.
type ToolDefinitionView struct {
	BackendID   string             `json:"backend_id"`
	ToolName    string             `json:"tool_name"`
	Risk        string             `json:"risk"`
	Schema      json.RawMessage    `json:"schema"`
	Arguments   []ToolArgumentView `json:"arguments"`
	Description string             `json:"description"`
	UpdatedAt   time.Time          `json:"updated_at"`
	UpdatedBy   string             `json:"updated_by"`
}

// PutToolRequest is the body of PUT /api/v1/tools/{backend_id}/{tool_name}.
// Identity comes from the path only.
type PutToolRequest struct {
	Risk        string             `json:"risk"`
	Schema      json.RawMessage    `json:"schema"`
	Arguments   []ToolArgumentView `json:"arguments"`
	Description string             `json:"description"`
}

// ToolDefinitionsResponse is the response for GET /api/v1/tools.
type ToolDefinitionsResponse struct {
	Tools []ToolDefinitionView `json:"tools"`
}

func toDefinitionView(d toolcatalog.ToolDefinition) ToolDefinitionView {
	args := make([]ToolArgumentView, 0, len(d.Arguments))
	for _, a := range d.Arguments {
		args = append(args, ToolArgumentView{Name: a.Name, Type: string(a.Type), Required: a.Required})
	}
	return ToolDefinitionView{
		BackendID:   d.BackendID,
		ToolName:    d.ToolName,
		Risk:        string(d.Risk),
		Schema:      d.SchemaJSON,
		Arguments:   args,
		Description: d.Description,
		UpdatedAt:   d.UpdatedAt,
		UpdatedBy:   d.UpdatedBy,
	}
}

func (h *Handler) catalogReady(w http.ResponseWriter) bool {
	if !h.toolCatalog.configured() {
		writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "tool catalog not configured")
		return false
	}
	return true
}

// validatePathIdentity checks the path parameters with the same rules the
// catalog applies to stored definitions.
func validatePathIdentity(backendID, toolName string) error {
	probe := toolcatalog.ToolDefinition{
		BackendID:  backendID,
		ToolName:   toolName,
		Risk:       toolregistry.RiskRead,
		SchemaJSON: json.RawMessage(`{}`),
	}
	return probe.Validate()
}

func (h *Handler) handleCatalogList(w http.ResponseWriter, r *http.Request) {
	if !h.catalogReady(w) {
		return
	}
	defs, err := h.toolCatalog.Store.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list tools")
		return
	}
	views := make([]ToolDefinitionView, 0, len(defs))
	for _, d := range defs {
		views = append(views, toDefinitionView(d))
	}
	writeJSON(w, http.StatusOK, ToolDefinitionsResponse{Tools: views})
}

func (h *Handler) handleCatalogGet(w http.ResponseWriter, r *http.Request) {
	if !h.catalogReady(w) {
		return
	}
	backendID, toolName := r.PathValue("backend_id"), r.PathValue("tool_name")
	if err := validatePathIdentity(backendID, toolName); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TOOL", err.Error())
		return
	}
	def, err := h.toolCatalog.Store.Get(r.Context(), backendID, toolName)
	if err != nil {
		if errors.Is(err, toolcatalog.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "tool not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get tool")
		return
	}
	writeJSON(w, http.StatusOK, toDefinitionView(*def))
}

func (h *Handler) handleCatalogPut(w http.ResponseWriter, r *http.Request) {
	if !h.catalogReady(w) {
		return
	}
	backendID, toolName := r.PathValue("backend_id"), r.PathValue("tool_name")
	if err := validatePathIdentity(backendID, toolName); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TOOL", err.Error())
		return
	}

	var req PutToolRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, toolBodyLimit)).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "request body exceeds 1 MiB")
			return
		}
		writeError(w, http.StatusBadRequest, "INVALID_TOOL", "malformed request body")
		return
	}

	args := make([]argdecl.Declaration, 0, len(req.Arguments))
	for _, a := range req.Arguments {
		args = append(args, argdecl.Declaration{Name: a.Name, Type: argdecl.ArgType(a.Type), Required: a.Required})
	}
	def := toolcatalog.ToolDefinition{
		BackendID:   backendID,
		ToolName:    toolName,
		Risk:        toolregistry.RiskLevel(req.Risk),
		SchemaJSON:  req.Schema,
		Arguments:   args,
		Description: req.Description,
		UpdatedAt:   time.Now().UTC(),
		UpdatedBy:   catalogOperatorID,
	}
	if err := def.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TOOL", err.Error())
		return
	}

	ctx := r.Context()
	prev, err := h.toolCatalog.Store.Get(ctx, backendID, toolName)
	if err != nil && !errors.Is(err, toolcatalog.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to read existing tool")
		return
	}
	if err := h.toolCatalog.Store.Upsert(ctx, def); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to store tool")
		return
	}
	if err := h.toolCatalog.Catalog.Reload(ctx, h.toolCatalog.Store); err != nil {
		h.rollbackTool(ctx, prev, backendID, toolName)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to reload tool catalog")
		return
	}

	h.emitToolMutation(ctx, auditevents.ActionToolUpserted, backendID, toolName)
	writeJSON(w, http.StatusOK, toDefinitionView(def))
}

func (h *Handler) handleCatalogDelete(w http.ResponseWriter, r *http.Request) {
	if !h.catalogReady(w) {
		return
	}
	backendID, toolName := r.PathValue("backend_id"), r.PathValue("tool_name")
	if err := validatePathIdentity(backendID, toolName); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TOOL", err.Error())
		return
	}

	ctx := r.Context()
	prev, err := h.toolCatalog.Store.Get(ctx, backendID, toolName)
	if err != nil && !errors.Is(err, toolcatalog.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to read existing tool")
		return
	}
	if err := h.toolCatalog.Store.Delete(ctx, backendID, toolName); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete tool")
		return
	}
	if err := h.toolCatalog.Catalog.Reload(ctx, h.toolCatalog.Store); err != nil {
		h.rollbackTool(ctx, prev, backendID, toolName)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to reload tool catalog")
		return
	}

	h.emitToolMutation(ctx, auditevents.ActionToolDeleted, backendID, toolName)
	w.WriteHeader(http.StatusNoContent)
}

// rollbackTool restores the pre-mutation state of one definition. It uses a
// context detached from request cancellation so a client disconnect cannot
// leave the store diverged from the live snapshot.
func (h *Handler) rollbackTool(ctx context.Context, prev *toolcatalog.ToolDefinition, backendID, toolName string) {
	rctx := context.WithoutCancel(ctx)
	if prev != nil {
		_ = h.toolCatalog.Store.Upsert(rctx, *prev)
		return
	}
	_ = h.toolCatalog.Store.Delete(rctx, backendID, toolName)
}

func (h *Handler) emitToolMutation(ctx context.Context, action auditevents.MutationAction, backendID, toolName string) {
	if h.toolCatalog.Listener == nil {
		return
	}
	var corr [8]byte
	_, _ = rand.Read(corr[:])
	h.toolCatalog.Listener.OnMutation(ctx, auditevents.MutationEvent{
		WorkspaceID:   catalogAuditWorkspace,
		Action:        action,
		NewVersion:    backendID + "/" + toolName,
		CorrelationID: "tool-" + hex.EncodeToString(corr[:]),
		Timestamp:     time.Now().UTC(),
		OperatorID:    catalogOperatorID,
	})
}
