package authz

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Dynamisch-LLC/agentgate/internal/credential"
	"github.com/Dynamisch-LLC/agentgate/internal/decision"
	"github.com/Dynamisch-LLC/agentgate/internal/metrics"
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

	// Metrics instrumentation options (optional)
	DecisionsTotal             *metrics.CounterVec
	DecisionDurationSeconds    *metrics.HistogramVec
	CredentialIssuanceFailures *metrics.Counter
}

// Server implements Envoy's v3 ext_authz AuthorizationServer interface.
type Server struct {
	authv3.UnimplementedAuthorizationServer
	adapter     *Adapter
	decisionSvc DecisionEvaluator

	requireCredential bool

	decisionsTotal             *metrics.CounterVec
	decisionDurationSeconds    *metrics.HistogramVec
	credentialIssuanceFailures *metrics.Counter
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
		adapter:                    adapter,
		decisionSvc:                decisionSvc,
		requireCredential:          opts.RequireDownstreamCredential,
		decisionsTotal:             opts.DecisionsTotal,
		decisionDurationSeconds:    opts.DecisionDurationSeconds,
		credentialIssuanceFailures: opts.CredentialIssuanceFailures,
	}
}

// SetMetrics configures Prometheus metrics on the Server.
func (s *Server) SetMetrics(decisions *metrics.CounterVec, duration *metrics.HistogramVec, credFailures *metrics.Counter) {
	s.decisionsTotal = decisions
	s.decisionDurationSeconds = duration
	s.credentialIssuanceFailures = credFailures
}

// Check evaluates an incoming Envoy v3 ext_authz CheckRequest.
func (s *Server) Check(ctx context.Context, req *authv3.CheckRequest) (resp *authv3.CheckResponse, _ error) {
	start := time.Now()
	defer func() {
		if resp != nil {
			dec, reason := extractResponseDecision(resp)
			if dec != "" {
				if s.decisionsTotal != nil {
					s.decisionsTotal.Inc(dec, reason)
				}
				if s.decisionDurationSeconds != nil {
					s.decisionDurationSeconds.Observe(time.Since(start).Seconds(), dec)
				}
			}
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

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

	type evalResult struct {
		res  decision.Result
		cred *credential.Credential
		err  error
	}

	evalCh := make(chan evalResult, 1)
	go func() {
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
		evalCh <- evalResult{res: res, cred: cred, err: err}
	}()

	var (
		res  decision.Result
		cred *credential.Credential
		err  error
	)
	select {
	case out := <-evalCh:
		res, cred, err = out.res, out.cred, out.err
	case <-ctx.Done():
		return s.buildDeniedResponse(decision.ReasonEvaluationError, ctx.Err().Error(), decisionReq.ExecutionID), nil
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

	// Verified live (deploy/demo/run-credential-e2e.ps1): agentgateway v1.4.0 honours OVERWRITE on `authorization`, so the backend sees only the minted credential.
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
	if s.credentialIssuanceFailures != nil && reason == decision.ReasonCredentialIssuanceFailure {
		s.credentialIssuanceFailures.Inc()
	}

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

func extractResponseDecision(resp *authv3.CheckResponse) (string, string) {
	if resp == nil {
		return "", ""
	}
	if ok := resp.GetOkResponse(); ok != nil {
		reason := getOptionHeader(ok.GetHeaders(), "x-agentgate-reason")
		if reason == "" {
			reason = string(decision.ReasonPolicyAllow)
		}
		return "ALLOW", reason
	}
	if denied := resp.GetDeniedResponse(); denied != nil {
		reason := getOptionHeader(denied.GetHeaders(), "x-agentgate-reason")
		if reason == "" {
			reason = string(decision.ReasonEvaluationError)
		}
		return "DENY", reason
	}
	return "DENY", string(decision.ReasonEvaluationError)
}

func getOptionHeader(headers []*corev3.HeaderValueOption, key string) string {
	for _, h := range headers {
		if h != nil && h.Header != nil && strings.EqualFold(h.Header.Key, key) {
			return h.Header.Value
		}
	}
	return ""
}
