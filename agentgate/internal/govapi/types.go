package govapi

import (
	"time"

	"github.com/Dynamisch-LLC/agentgate/internal/policystore"
)

// CreateCandidateRequest represents the payload to submit a candidate policy.
type CreateCandidateRequest struct {
	Content     string `json:"content"`
	Description string `json:"description"`
}

// ValidateRequest represents the payload to validate raw Cedar syntax.
type ValidateRequest struct {
	Content string `json:"content"`
}

// ValidateResponse describes the validation outcome.
type ValidateResponse struct {
	Valid   bool     `json:"valid"`
	Version string   `json:"version,omitempty"`
	Errors  []string `json:"errors,omitempty"`
}

// ActivateResponse describes the outcome of an atomic activation.
type ActivateResponse struct {
	WorkspaceID     string `json:"workspace_id"`
	ActiveVersion   string `json:"active_version"`
	PreviousVersion string `json:"previous_version,omitempty"`
	ActivatedAt     string `json:"activated_at"`
}

// RollbackRequest targets a specific previous policy version.
type RollbackRequest struct {
	TargetVersion string `json:"target_version"`
}

// RollbackResponse describes the outcome of a rollback.
type RollbackResponse struct {
	WorkspaceID    string `json:"workspace_id"`
	ActiveVersion  string `json:"active_version"`
	RolledBackFrom string `json:"rolled_back_from"`
	ActivatedAt    string `json:"activated_at"`
}

// ListPoliciesResponse returns the list of all policy versions for a workspace.
type ListPoliciesResponse struct {
	WorkspaceID string                    `json:"workspace_id"`
	Policies    []policystore.PolicyRecord `json:"policies"`
}

// PreviewSample is one evaluation input for policy dry-run.
type PreviewSample struct {
	PrincipalID    string   `json:"principal_id"`
	PrincipalRoles []string `json:"principal_roles"`
	ResourceID     string   `json:"resource_id"`
	ResourceRisk   string   `json:"resource_risk"`
}

// PreviewRequest encapsulates sample inputs for dry-run evaluation.
type PreviewRequest struct {
	SampleRequests []PreviewSample `json:"sample_requests"`
}

// PreviewResult describes evaluation output for one sample request.
type PreviewResult struct {
	Allowed  bool `json:"allowed"`
	Matched  bool `json:"matched"`
	HadError bool `json:"had_error"`
}

// PreviewResponse aggregates preview evaluation results.
type PreviewResponse struct {
	Version string          `json:"version"`
	Results []PreviewResult `json:"results"`
}

// ErrorDetail contains structured error information.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorResponse is the standard error payload.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// DryRunSampleRequest is one decision request input for dry-run comparison.
type DryRunSampleRequest struct {
	ExecutionID    string   `json:"execution_id"`
	PrincipalID    string   `json:"principal_id"`
	PrincipalRoles []string `json:"principal_roles"`
	OnBehalfOf     string   `json:"on_behalf_of,omitempty"`
	BackendID      string   `json:"backend_id"`
	ToolName       string   `json:"tool_name"`
	Risk           string   `json:"risk"`
}

// DryRunCompareRequest carries sample decision requests for comparison.
type DryRunCompareRequest struct {
	SampleRequests []DryRunSampleRequest `json:"sample_requests"`
}

// DryRunCompareResult pairs the active and candidate outcomes.
type DryRunCompareResult struct {
	ActiveDecision    string `json:"active_decision"`
	ActiveReason      string `json:"active_reason"`
	ActiveVersion     string `json:"active_policy_version"`
	CandidateDecision string `json:"candidate_decision"`
	CandidateReason   string `json:"candidate_reason"`
	CandidateVersion  string `json:"candidate_policy_version"`
	Changed           bool   `json:"changed"`
}

// DryRunCompareResponse aggregates comparison results.
type DryRunCompareResponse struct {
	CandidateVersion string                `json:"candidate_version"`
	Results          []DryRunCompareResult  `json:"results"`
}

// AuditEventView is the operator-facing audit record shape — a read-only
// projection of audit.StoredRecord. canonical_payload, prev_hash, and
// row_hash are internal chain-verification artifacts, intentionally
// excluded per the reviewed contract (frontend/app/PROPOSED_G8_API_CONTRACTS.md):
// the UI must never reconstruct or expose raw arguments, only the
// already-redacted ones.
type AuditEventView struct {
	ID                  int64             `json:"id"`
	WorkspaceID         string            `json:"workspace_id"`
	SequenceNumber      int64             `json:"sequence_number"`
	ExecutionID         string            `json:"execution_id"`
	Timestamp           time.Time         `json:"timestamp"`
	EventType           string            `json:"event_type"`
	Decision            string            `json:"decision"`
	Reason              string            `json:"reason"`
	PrincipalAgentID    string            `json:"principal_agent_id"`
	PrincipalRoles      []string          `json:"principal_roles"`
	PrincipalOnBehalfOf string            `json:"principal_on_behalf_of"`
	ToolBackendID       string            `json:"tool_backend_id"`
	ToolName            string            `json:"tool_name"`
	ToolRisk            string            `json:"tool_risk"`
	PolicyVersion       string            `json:"policy_version"`
	PolicyHash          string            `json:"policy_hash"`
	RedactedArguments   map[string]string `json:"redacted_arguments"`
}

// AuditEventsResponse is the response for GET .../audit-events. NextBeforeSequence
// is present whenever at least one event was returned, letting the caller page
// backward by resending this value as the next request's before_sequence — an
// empty events list is the signal that there are no more pages.
type AuditEventsResponse struct {
	WorkspaceID        string           `json:"workspace_id"`
	Events             []AuditEventView `json:"events"`
	NextBeforeSequence *int64           `json:"next_before_sequence,omitempty"`
}

// ToolIDView mirrors toolregistry.ToolID for JSON purposes — govapi does not
// import toolregistry's own JSON tags (it has none; the struct is Go-only),
// so this is a small, deliberate, explicit projection rather than reuse.
type ToolIDView struct {
	BackendID string `json:"backend_id"`
	ToolName  string `json:"tool_name"`
}

// ToolView is the operator-facing tool inventory shape — a read-only
// projection of toolregistry.GovernanceRecord. DriftStatus is intentionally
// omitted: Registry.List() never computes it (no live fingerprint exists for
// a bulk listing), so surfacing it here would imply a check that never ran.
type ToolView struct {
	ToolID                ToolIDView `json:"tool_id"`
	Known                 bool       `json:"known"`
	Risk                  string     `json:"risk"`
	RegisteredFingerprint string     `json:"registered_fingerprint"`
}

// ToolsResponse is the response for GET .../tools. Source is "catalog" when
// backed by the runtime tool catalog, else "static_configuration".
type ToolsResponse struct {
	WorkspaceID string     `json:"workspace_id"`
	Source      string     `json:"source"`
	Tools       []ToolView `json:"tools"`
}
