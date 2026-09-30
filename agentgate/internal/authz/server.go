package authz

import (
	"context"
	"fmt"

	"github.com/Dynamisch-LLC/agentgate/internal/credential"
	"github.com/Dynamisch-LLC/agentgate/internal/decision"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
)

// DecisionEvaluator evaluates an authorization request and returns a durably audited decision.
type DecisionEvaluator interface {
	Evaluate(ctx context.Context, req decision.Request) (decision.Result, error)
}

// CredentialedEvaluator is implemented by evaluators that also mint a downstream credential.
type CredentialedEvaluator interface {
	EvaluateWithCredential(ctx context.Context, req decision.Request) (decision.Result, *credential.Credential, error)
}

// ServerOptions configures optional Server behaviour.
type ServerOptions struct {
	// RequireDownstreamCredential denies Allow decisions that carry no minted credential.
	RequireDownstreamCredential bool
}

// Server implements Envoy's v3 ext_authz AuthorizationServer interface.
type Server struct {
	authv3.UnimplementedAuthorizationServer
	adapter     *Adapter
	decisionSvc DecisionEvaluator

	requireCredential bool
}

// NewServer constructs an ext_authz gRPC Server.
func NewServer(adapter *Adapter, decisionSvc DecisionEvaluator) *Server {
	return &Server{
		adapter:     adapter,
		decisionSvc: decisionSvc,
	}
}

// NewServerWithOptions constructs an ext_authz gRPC Server with explicit options.
func NewServerWithOptions(adapter *Adapter, decisionSvc DecisionEvaluator, opts ServerOptions) *Server {
	return &Server{
		adapter:           adapter,
		decisionSvc:       decisionSvc,
		requireCredential: opts.RequireDownstreamCredential,
	}
}

// Check evaluates an incoming Envoy v3 ext_authz CheckRequest.
func (s *Server) Check(ctx context.Context, req *authv3.CheckRequest) (*authv3.CheckResponse, error) {
	if s.adapter == nil {
		return s.buildDeniedResponse(decision.ReasonEvaluationError, "adapter not configured", ""), nil
	}

	decisionReq, aerr := s.adapter.Adapt(ctx, req)
	if aerr != nil {
		// Adaptation failed (e.g. malformed body, invalid identity, unknown tool).
		// Durably audit denial via decision service when possible.
		execID := ""
		if req != nil && req.Attributes != nil && req.Attributes.Request != nil && req.Attributes.Request.Http != nil {
			execID = getHeader(req.Attributes.Request.Http.Headers, "x-execution-id")
		}
		if execID == "" {
			execID = generateExecutionID()
		}

		if s.decisionSvc != nil {
			fallbackReq := decision.Request{
				ExecutionID: execID,
				WorkspaceID: s.adapter.cfg.DefaultWorkspaceID,
				Identity: decision.Identity{
					AgentID: "unauthenticated",
					Roles:   []string{"unauthenticated"},
				},
				Classification: decision.ToolClassification{
					Known: false,
				},
			}
			_, _ = s.decisionSvc.Evaluate(ctx, fallbackReq)
		}

		return s.buildDeniedResponse(aerr.ReasonCode, aerr.Message, execID), nil
	}

	if s.decisionSvc == nil {
		return s.buildDeniedResponse(decision.ReasonEvaluationError, "decision service not configured", decisionReq.ExecutionID), nil
	}

	var (
		res  decision.Result
		cred *credential.Credential
		err  error
	)
	if ce, ok := s.decisionSvc.(CredentialedEvaluator); ok {
		res, cred, err = ce.EvaluateWithCredential(ctx, decisionReq)
	} else {
		res, err = s.decisionSvc.Evaluate(ctx, decisionReq)
	}
	if err != nil {
		return s.buildDeniedResponse(decision.ReasonEvaluationError, err.Error(), decisionReq.ExecutionID), nil
	}

	if res.Decision == decision.Allow {
		if cred != nil && cred.HeaderName == "" {
			return s.buildDeniedResponse(decision.ReasonCredentialIssuanceFailure, "downstream credential malformed", res.ExecutionID), nil
		}
		if cred == nil && s.requireCredential {
			return s.buildDeniedResponse(decision.ReasonCredentialIssuanceFailure, "downstream credential unavailable", res.ExecutionID), nil
		}
		return s.buildOkResponse(res, cred), nil
	}

	return s.buildDeniedResponse(res.Reason, res.Message, res.ExecutionID), nil
}

func (s *Server) buildOkResponse(res decision.Result, cred *credential.Credential) *authv3.CheckResponse {
	headers := []*corev3.HeaderValueOption{
		{
			Header: &corev3.HeaderValue{
				Key:   "x-agentgate-decision",
				Value: "allow",
			},
		},
		{
			Header: &corev3.HeaderValue{
				Key:   "x-agentgate-reason",
				Value: string(res.Reason),
			},
		},
		{
			Header: &corev3.HeaderValue{
				Key:   "x-agentgate-execution-id",
				Value: res.ExecutionID,
			},
		},
	}

	if res.PolicyVersion != "" {
		headers = append(headers, &corev3.HeaderValueOption{
			Header: &corev3.HeaderValue{
				Key:   "x-agentgate-policy-version",
				Value: res.PolicyVersion,
			},
		})
	}

	var toRemove []string
	if cred != nil {
		headers = append(headers, &corev3.HeaderValueOption{
			Header:       &corev3.HeaderValue{Key: cred.HeaderName, Value: cred.HeaderValue},
			AppendAction: corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
		})
	} else {
		toRemove = []string{"authorization"}
	}

	return &authv3.CheckResponse{
		Status: &status.Status{
			Code:    int32(codes.OK),
			Message: "allowed by agentgate",
		},
		HttpResponse: &authv3.CheckResponse_OkResponse{
			OkResponse: &authv3.OkHttpResponse{
				Headers:         headers,
				HeadersToRemove: toRemove,
			},
		},
	}
}

func (s *Server) buildDeniedResponse(reason decision.ReasonCode, msg, execID string) *authv3.CheckResponse {
	headers := []*corev3.HeaderValueOption{
		{
			Header: &corev3.HeaderValue{
				Key:   "x-agentgate-decision",
				Value: "deny",
			},
		},
		{
			Header: &corev3.HeaderValue{
				Key:   "x-agentgate-reason",
				Value: string(reason),
			},
		},
	}

	if execID != "" {
		headers = append(headers, &corev3.HeaderValueOption{
			Header: &corev3.HeaderValue{
				Key:   "x-agentgate-execution-id",
				Value: execID,
			},
		})
	}

	body := fmt.Sprintf("AgentGate Authorization Denied: %s", reason)
	if msg != "" {
		body = fmt.Sprintf("AgentGate Authorization Denied: %s (%s)", reason, msg)
	}

	return &authv3.CheckResponse{
		Status: &status.Status{
			Code:    int32(codes.PermissionDenied),
			Message: body,
		},
		HttpResponse: &authv3.CheckResponse_DeniedResponse{
			DeniedResponse: &authv3.DeniedHttpResponse{
				Status: &typev3.HttpStatus{
					Code: typev3.StatusCode_Forbidden,
				},
				Body:    body,
				Headers: headers,
			},
		},
	}
}
