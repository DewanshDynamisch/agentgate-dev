package audit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Dynamisch-LLC/agentgate/internal/auditevents"
	"github.com/Dynamisch-LLC/agentgate/internal/credential"
	"github.com/Dynamisch-LLC/agentgate/internal/decision"
)

// DecisionEvaluator evaluates a decision request against the active policy of a workspace.
type DecisionEvaluator interface {
	EvaluateWithActivePolicy(ctx context.Context, workspaceID string, req decision.Request) (decision.Result, error)
}

// ProvenanceProvider retrieves the authoritative policy version and content hash for a workspace.
type ProvenanceProvider interface {
	GetActiveProvenance(workspaceID string) (version string, hash string, err error)
}

// AuditedDecisionService intercepts every authorization decision and policy mutation,
// ensuring strict redaction, deterministic canonicalization, cryptographic hash chaining,
// and durable audit logging before an ALLOW decision can ever be returned to the client (O-002).
type AuditedDecisionService struct {
	evaluator DecisionEvaluator
	store     Store
	redactor  *Redactor
	issuer    credential.Issuer
	mu        sync.Mutex
}

// NewAuditedDecisionService constructs an AuditedDecisionService wrapping an evaluator and audit store.
func NewAuditedDecisionService(evaluator DecisionEvaluator, store Store, redactor *Redactor) *AuditedDecisionService {
	if redactor == nil {
		redactor = NewRedactor("", nil)
	}
	return &AuditedDecisionService{
		evaluator: evaluator,
		store:     store,
		redactor:  redactor,
	}
}

// Store returns the underlying audit Store.
func (s *AuditedDecisionService) Store() Store {
	return s.store
}

// SetEvaluator sets or updates the decision evaluator.
func (s *AuditedDecisionService) SetEvaluator(evaluator DecisionEvaluator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evaluator = evaluator
}

// SetIssuer sets the downstream credential issuer; nil disables issuance.
func (s *AuditedDecisionService) SetIssuer(issuer credential.Issuer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.issuer = issuer
}

// Evaluate evaluates the request and durably persists the audit record, discarding any issued credential.
func (s *AuditedDecisionService) Evaluate(ctx context.Context, req decision.Request) (decision.Result, error) {
	res, _, err := s.EvaluateWithCredential(ctx, req)
	return res, err
}

// EvaluateWithCredential evaluates the request, issues a downstream credential for an ALLOW
// before the audit append, and durably persists the audit record. It FAILS CLOSED: if issuance
// fails the decision becomes DENY; if the audit write fails the decision becomes DENY and the
// credential is discarded. A credential is returned only with a final ALLOW.
func (s *AuditedDecisionService) EvaluateWithCredential(ctx context.Context, req decision.Request) (decision.Result, *credential.Credential, error) {
	// 1. Evaluate request against active policy
	var res decision.Result
	if s.evaluator != nil {
		var err error
		res, err = s.evaluator.EvaluateWithActivePolicy(ctx, req.WorkspaceID, req)
		if err != nil {
			res = decision.Result{
				Decision:    decision.Deny,
				Reason:      decision.ReasonEvaluationError,
				Message:     fmt.Sprintf("policy evaluation error: %v", err),
				ExecutionID: req.ExecutionID,
			}
		}
	} else {
		noPolicy := decision.NewEngineWithoutPolicy()
		res = noPolicy.Evaluate(req)
	}

	// 2. Resolve authoritative policy version and policy hash
	var policyVersion string
	var policyHash string
	if res.PolicyVersion != "" {
		// Cedar was reached. In G1 decision.Engine, res.PolicyVersion is the SHA-256
		// hash of the evaluated policy source bytes (e.policy.Version()).
		policyHash = res.PolicyVersion
		policyVersion = res.PolicyVersion

		// If the evaluator implements ProvenanceProvider, retrieve the active policy version identifier
		if prov, ok := s.evaluator.(ProvenanceProvider); ok {
			if v, h, err := prov.GetActiveProvenance(req.WorkspaceID); err == nil && v != "" {
				policyVersion = v
				if h != "" {
					policyHash = h
				}
			}
		}
		// Reflect the active policy version identifier in the returned result
		res.PolicyVersion = policyVersion
	}

	s.mu.Lock()
	issuer := s.issuer
	s.mu.Unlock()

	var cred *credential.Credential
	if res.Decision == decision.Allow && issuer != nil {
		c, issueErr := issuer.Issue(ctx, req, res)
		if issueErr == nil && (c.Ref == "" || c.HeaderValue == "") {
			issueErr = errors.New("issuer returned credential with empty ref or value")
		}
		if issueErr != nil {
			slog.Warn("downstream credential issuance failed", "execution_id", req.ExecutionID, "workspace_id", req.WorkspaceID, "error", issueErr)
			res = decision.Result{
				Decision:      decision.Deny,
				Reason:        decision.ReasonCredentialIssuanceFailure,
				Message:       "downstream credential issuance failed",
				ExecutionID:   res.ExecutionID,
				PolicyVersion: res.PolicyVersion,
			}
		} else {
			cred = &c
		}
	}

	// 3. Redact arguments prior to canonicalization or persistence
	redactedArgs := s.redactor.RedactArguments(req.Tool.Name, req.Arguments)

	// 4. Serialize append operation to maintain sequential chain
	s.mu.Lock()
	defer s.mu.Unlock()

	latest, err := s.store.GetLatestRecord(ctx, req.WorkspaceID)
	prevHash := GenesisHash
	if err == nil && latest != nil {
		prevHash = latest.RowHash
	}

	record := DecisionRecord{
		WorkspaceID:         req.WorkspaceID,
		ExecutionID:         req.ExecutionID,
		Timestamp:           time.Now().UTC(),
		EventType:           EventTypeDecision,
		Decision:            string(res.Decision),
		Reason:              string(res.Reason),
		PrincipalAgentID:    req.Identity.AgentID,
		PrincipalRoles:      req.Identity.Roles,
		PrincipalOnBehalfOf: req.Identity.OnBehalfOf,
		ToolBackendID:       req.Tool.BackendID,
		ToolName:            req.Tool.Name,
		ToolRisk:            req.Classification.Risk,
		PolicyVersion:       policyVersion,
		PolicyHash:          policyHash,
		RedactedArguments:   redactedArgs,
		PrevHash:            prevHash,
	}
	if cred != nil {
		record.DownstreamCredentialRef = cred.Ref
	}

	canonical, err := ComputeCanonicalPayload(record)
	if err != nil {
		// Canonicalization error: fail closed on ALLOW
		if res.Decision == decision.Allow {
			return decision.Result{
				Decision:      decision.Deny,
				Reason:        decision.ReasonEvaluationError,
				Message:       fmt.Sprintf("audit canonicalization error: %v", err),
				ExecutionID:   req.ExecutionID,
				PolicyVersion: res.PolicyVersion,
			}, nil, nil
		}
		return res, nil, nil
	}

	record.CanonicalPayload = string(canonical)
	record.RowHash = ComputeRowHash(prevHash, canonical)

	// 4. Durably persist
	_, appendErr := s.store.AppendDecision(ctx, record)
	if appendErr != nil {
		// CRITICAL INVARIANT (O-002): An ALLOW decision must NEVER be returned
		// without confirmed durable audit write. If the audit write fails, fail closed!
		if res.Decision == decision.Allow {
			return decision.Result{
				Decision:      decision.Deny,
				Reason:        decision.ReasonEvaluationError,
				Message:       fmt.Sprintf("audit failure: unable to durably record ALLOW decision: %v", appendErr),
				ExecutionID:   req.ExecutionID,
				PolicyVersion: res.PolicyVersion,
			}, nil, nil
		}
		// If DENY: safe to return DENY since security is preserved
		return res, nil, nil
	}

	return res, cred, nil
}

// OnMutation records a policy mutation into the audit trail, implementing auditevents.MutationListener.
func (s *AuditedDecisionService) OnMutation(ctx context.Context, event auditevents.MutationEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	latest, err := s.store.GetLatestRecord(ctx, event.WorkspaceID)
	prevHash := GenesisHash
	if err == nil && latest != nil {
		prevHash = latest.RowHash
	}

	record := DecisionRecord{
		WorkspaceID:       event.WorkspaceID,
		ExecutionID:       event.CorrelationID,
		Timestamp:         event.Timestamp,
		EventType:         EventTypeMutation,
		Decision:          "MUTATION",
		Reason:            string(event.Action),
		PrincipalAgentID:  event.OperatorID,
		PolicyVersion:     event.NewVersion,
		PolicyHash:        event.PreviousVersion,
		RedactedArguments: make(map[string]string),
		PrevHash:          prevHash,
	}

	canonical, err := ComputeCanonicalPayload(record)
	if err == nil {
		record.CanonicalPayload = string(canonical)
		record.RowHash = ComputeRowHash(prevHash, canonical)
	}

	_, _ = s.store.AppendDecision(ctx, record)
}
