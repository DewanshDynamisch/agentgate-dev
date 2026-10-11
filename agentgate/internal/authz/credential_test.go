package authz

import (
	"context"
	"testing"

	"github.com/Dynamisch-LLC/agentgate/internal/audit"
	"github.com/Dynamisch-LLC/agentgate/internal/credential"
	"github.com/Dynamisch-LLC/agentgate/internal/decision"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	"google.golang.org/grpc/codes"
)

type fakeCredEvaluator struct {
	cred *credential.Credential
}

func (f *fakeCredEvaluator) Evaluate(ctx context.Context, req decision.Request) (decision.Result, error) {
	res, _, err := f.EvaluateWithCredential(ctx, req)
	return res, err
}

func (f *fakeCredEvaluator) EvaluateWithCredential(_ context.Context, req decision.Request) (decision.Result, *credential.Credential, error) {
	return decision.Result{Decision: decision.Allow, Reason: decision.ReasonPolicyAllow, ExecutionID: req.ExecutionID}, f.cred, nil
}

type plainEvaluator struct{}

func (plainEvaluator) Evaluate(_ context.Context, req decision.Request) (decision.Result, error) {
	return decision.Result{Decision: decision.Allow, Reason: decision.ReasonPolicyAllow, ExecutionID: req.ExecutionID}, nil
}

func credCheckReq(t *testing.T) *authv3.CheckRequest {
	t.Helper()
	return &authv3.CheckRequest{
		Attributes: &authv3.AttributeContext{
			Request: &authv3.AttributeContext_Request{
				Http: &authv3.AttributeContext_HttpRequest{
					Method: "POST",
					Path:   "/",
					Body:   `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_status","arguments":{"verbose":true}}}`,
				},
			},
			MetadataContext: newTestJWTMetadata(t, "agent-001", "reader"),
		},
	}
}

func TestCheck_InjectsCredentialHeader(t *testing.T) {
	srv := NewServerWithOptions(setupTestAdapter(t),
		&fakeCredEvaluator{cred: &credential.Credential{HeaderName: "authorization", HeaderValue: "Bearer minted", Ref: "r1"}},
		ServerOptions{RequireDownstreamCredential: true})
	resp, err := srv.Check(context.Background(), credCheckReq(t))
	if err != nil {
		t.Fatal(err)
	}
	ok := resp.GetOkResponse()
	if ok == nil {
		t.Fatalf("want OkResponse, got %v", resp)
	}
	found := false
	for _, h := range ok.Headers {
		if h.Header.Key == "authorization" {
			found = true
			if h.Header.Value != "Bearer minted" {
				t.Errorf("value = %q", h.Header.Value)
			}
			if h.AppendAction != corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD {
				t.Errorf("append action = %v", h.AppendAction)
			}
		}
	}
	if !found {
		t.Fatal("authorization header missing")
	}
	for _, r := range ok.HeadersToRemove {
		if r == "authorization" {
			t.Error("authorization must not be removed when credential injected")
		}
	}
}

func TestCheck_AllowNilCredentialRequiredDenies(t *testing.T) {
	srv := NewServerWithOptions(setupTestAdapter(t), &fakeCredEvaluator{}, ServerOptions{RequireDownstreamCredential: true})
	resp, err := srv.Check(context.Background(), credCheckReq(t))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status.Code != int32(codes.PermissionDenied) || resp.GetDeniedResponse() == nil {
		t.Fatalf("want deny, got %v", resp)
	}
	var reason string
	for _, h := range resp.GetDeniedResponse().Headers {
		if h.Header.Key == "x-agentgate-reason" {
			reason = h.Header.Value
		}
	}
	if reason != string(decision.ReasonCredentialIssuanceFailure) {
		t.Errorf("reason = %q", reason)
	}
}

func TestCheck_AllowNilCredentialNotRequiredStripsAuthorization(t *testing.T) {
	srv := NewServerWithOptions(setupTestAdapter(t), &fakeCredEvaluator{}, ServerOptions{})
	resp, err := srv.Check(context.Background(), credCheckReq(t))
	if err != nil {
		t.Fatal(err)
	}
	ok := resp.GetOkResponse()
	if ok == nil {
		t.Fatalf("want OkResponse, got %v", resp)
	}
	removed := false
	for _, r := range ok.HeadersToRemove {
		if r == "authorization" {
			removed = true
		}
	}
	if !removed {
		t.Error("authorization not in HeadersToRemove")
	}
}

func TestCheck_PlainEvaluatorBackwardCompatible(t *testing.T) {
	srv := NewServer(setupTestAdapter(t), plainEvaluator{})
	resp, err := srv.Check(context.Background(), credCheckReq(t))
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetOkResponse() == nil {
		t.Fatalf("want OkResponse, got %v", resp)
	}
}

func TestCheck_PlainEvaluatorRequiredCredentialDenies(t *testing.T) {
	srv := NewServerWithOptions(setupTestAdapter(t), plainEvaluator{}, ServerOptions{RequireDownstreamCredential: true})
	resp, err := srv.Check(context.Background(), credCheckReq(t))
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetDeniedResponse() == nil {
		t.Fatalf("want deny, got %v", resp)
	}
}

func TestCheck_MissingAudienceMappingDenies(t *testing.T) {
	key, err := credential.GenerateEphemeralKey()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := credential.NewJWTIssuer(credential.JWTIssuerConfig{
		Issuer: "agentgate", KeyID: "k1", PrivateKey: key, Lifetime: 60e9,
		AudienceFor: func(string) string { return "" },
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := audit.NewAuditedDecisionService(&mockEvaluator{decision: decision.Allow, reason: decision.ReasonPolicyAllow}, audit.NewMemoryStore(), audit.NewRedactor("", nil))
	svc.SetIssuer(issuer)
	srv := NewServerWithOptions(setupTestAdapter(t), svc, ServerOptions{RequireDownstreamCredential: true})
	resp, err := srv.Check(context.Background(), credCheckReq(t))
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetOkResponse() != nil || resp.Status.Code != int32(codes.PermissionDenied) {
		t.Fatalf("want deny, got %v", resp)
	}
}

func TestCheck_EmptyCredentialHeaderNameDenies(t *testing.T) {
	srv := NewServerWithOptions(setupTestAdapter(t),
		&fakeCredEvaluator{cred: &credential.Credential{HeaderName: "", HeaderValue: "Bearer x", Ref: "r"}},
		ServerOptions{})
	resp, err := srv.Check(context.Background(), credCheckReq(t))
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetOkResponse() != nil || resp.GetDeniedResponse() == nil {
		t.Fatalf("want deny, got %v", resp)
	}
	var reason string
	for _, h := range resp.GetDeniedResponse().Headers {
		if h.Header.Key == "x-agentgate-reason" {
			reason = h.Header.Value
		}
	}
	if reason != string(decision.ReasonCredentialIssuanceFailure) {
		t.Errorf("reason = %q", reason)
	}
}
