package credential

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/Dynamisch-LLC/agentgate/internal/decision"
)

var testNow = time.Unix(1_800_000_000, 0)

func testKey(t *testing.T, bits int) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func newTestIssuer(t *testing.T) (*JWTIssuer, *rsa.PrivateKey) {
	t.Helper()
	k := testKey(t, 2048)
	j, err := NewJWTIssuer(JWTIssuerConfig{
		Issuer:      "agentgate",
		KeyID:       "k1",
		PrivateKey:  k,
		Lifetime:    time.Minute,
		AudienceFor: func(b string) string { return "aud-" + b },
		Now:         func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	return j, k
}

func allowReq(risk, onBehalfOf string) (decision.Request, decision.Result) {
	return decision.Request{
		ExecutionID:    "exec-1",
		Identity:       decision.Identity{AgentID: "agent-1", OnBehalfOf: onBehalfOf},
		Tool:           decision.ToolRef{BackendID: "support", Name: "create_ticket"},
		Classification: decision.ToolClassification{Known: true, Risk: risk},
	}, decision.Result{Decision: decision.Allow}
}

func verifyAndDecode(t *testing.T, pub *rsa.PublicKey, jwt string) (map[string]any, map[string]any) {
	t.Helper()
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("want 3 segments, got %d", len(parts))
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("signature: %v", err)
	}
	dec := func(s string) map[string]any {
		b, err := base64.RawURLEncoding.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]any{}
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	return dec(parts[0]), dec(parts[1])
}

func issueJWT(t *testing.T, j *JWTIssuer, req decision.Request, res decision.Result) (string, Credential) {
	t.Helper()
	c, err := j.Issue(context.Background(), req, res)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimPrefix(c.HeaderValue, "Bearer "), c
}

func TestIssue_AllowWriteClaims(t *testing.T) {
	j, k := newTestIssuer(t)
	req, res := allowReq("write", "alice")
	jwt, c := issueJWT(t, j, req, res)
	if c.HeaderName != "Authorization" || !strings.HasPrefix(c.HeaderValue, "Bearer ") {
		t.Fatalf("bad header: %q %q", c.HeaderName, c.HeaderValue)
	}
	hdr, cl := verifyAndDecode(t, &k.PublicKey, jwt)
	if hdr["alg"] != "RS256" || hdr["typ"] != "JWT" || hdr["kid"] != "k1" || len(hdr) != 3 {
		t.Fatalf("header: %v", hdr)
	}
	jti, _ := cl["jti"].(string)
	if len(jti) != 32 {
		t.Fatalf("jti: %q", jti)
	}
	now := float64(testNow.Unix())
	want := map[string]any{
		"iss": "agentgate", "aud": "aud-support", "sub": "agent-1",
		"act":   map[string]any{"sub": "alice"},
		"scope": "tickets:read tickets:write", "tool": "create_ticket",
		"backend": "support", "exec": "exec-1", "jti": jti,
		"iat": now, "nbf": now, "exp": now + 60,
	}
	if len(cl) != len(want) {
		t.Fatalf("claims: %v", cl)
	}
	for name, v := range want {
		gb, _ := json.Marshal(cl[name])
		wb, _ := json.Marshal(v)
		if string(gb) != string(wb) {
			t.Errorf("claim %s = %s, want %s", name, gb, wb)
		}
	}
}

func TestIssue_ActOnlyWhenOnBehalfOf(t *testing.T) {
	j, k := newTestIssuer(t)
	req, res := allowReq("read", "")
	jwt, _ := issueJWT(t, j, req, res)
	_, cl := verifyAndDecode(t, &k.PublicKey, jwt)
	if _, ok := cl["act"]; ok {
		t.Fatal("act present without OnBehalfOf")
	}
}

func TestIssue_ExpMinusIatIsLifetime(t *testing.T) {
	k := testKey(t, 2048)
	j, err := NewJWTIssuer(JWTIssuerConfig{
		Issuer: "i", KeyID: "k", PrivateKey: k, Lifetime: 90 * time.Second,
		AudienceFor: func(string) string { return "a" },
		Now:         func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	req, res := allowReq("read", "")
	jwt, _ := issueJWT(t, j, req, res)
	_, cl := verifyAndDecode(t, &k.PublicKey, jwt)
	if d := cl["exp"].(float64) - cl["iat"].(float64); d != 90 {
		t.Fatalf("exp-iat = %v", d)
	}
}

func TestIssue_DenyErrors(t *testing.T) {
	j, _ := newTestIssuer(t)
	req, _ := allowReq("read", "")
	if _, err := j.Issue(context.Background(), req, decision.Result{Decision: decision.Deny}); err == nil {
		t.Fatal("want error")
	}
}

func TestIssue_UnknownRiskErrors(t *testing.T) {
	j, _ := newTestIssuer(t)
	req, res := allowReq("weird", "")
	if _, err := j.Issue(context.Background(), req, res); err == nil {
		t.Fatal("want error")
	}
}

func TestIssue_EmptyAudienceErrors(t *testing.T) {
	k := testKey(t, 2048)
	j, err := NewJWTIssuer(JWTIssuerConfig{
		Issuer: "i", KeyID: "k", PrivateKey: k, Lifetime: time.Minute,
		AudienceFor: func(string) string { return "" },
	})
	if err != nil {
		t.Fatal(err)
	}
	req, res := allowReq("read", "")
	if _, err := j.Issue(context.Background(), req, res); err == nil {
		t.Fatal("want error")
	}
}

func TestIssue_DistinctJTI(t *testing.T) {
	j, k := newTestIssuer(t)
	req, res := allowReq("read", "")
	a, _ := issueJWT(t, j, req, res)
	b, _ := issueJWT(t, j, req, res)
	_, ca := verifyAndDecode(t, &k.PublicKey, a)
	_, cb := verifyAndDecode(t, &k.PublicKey, b)
	if ca["jti"] == cb["jti"] {
		t.Fatal("jti reused")
	}
}

func TestIssue_RefLeaksNoTokenMaterial(t *testing.T) {
	j, _ := newTestIssuer(t)
	req, res := allowReq("write", "alice")
	jwt, c := issueJWT(t, j, req, res)
	sig := jwt[strings.LastIndex(jwt, ".")+1:]
	if strings.Contains(c.Ref, jwt) {
		t.Fatal("ref contains token")
	}
	// Any 8-char window of the signature appearing in Ref would be a leak.
	for i := 0; i+8 <= len(sig); i++ {
		if strings.Contains(c.Ref, sig[i:i+8]) {
			t.Fatalf("ref contains signature substring %q", sig[i:i+8])
		}
	}
	if !strings.HasPrefix(c.Ref, "jti:") || !strings.HasSuffix(c.Ref, ";kid:k1") {
		t.Fatalf("ref format: %q", c.Ref)
	}
}

func TestJWKS_VerifiesToken(t *testing.T) {
	j, _ := newTestIssuer(t)
	req, res := allowReq("write", "alice")
	jwt, _ := issueJWT(t, j, req, res)

	var set struct {
		Keys []map[string]string `json:"keys"`
	}
	if err := json.Unmarshal(j.JWKS(), &set); err != nil {
		t.Fatal(err)
	}
	if len(set.Keys) != 1 {
		t.Fatalf("keys: %d", len(set.Keys))
	}
	jwk := set.Keys[0]
	if jwk["kty"] != "RSA" || jwk["use"] != "sig" || jwk["alg"] != "RS256" || jwk["kid"] != "k1" {
		t.Fatalf("jwk: %v", jwk)
	}
	nb, err := base64.RawURLEncoding.DecodeString(jwk["n"])
	if err != nil {
		t.Fatal(err)
	}
	eb, err := base64.RawURLEncoding.DecodeString(jwk["e"])
	if err != nil {
		t.Fatal(err)
	}
	pub := &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: int(new(big.Int).SetBytes(eb).Int64())}
	verifyAndDecode(t, pub, jwt)
}

func TestNewJWTIssuer_Validation(t *testing.T) {
	base := JWTIssuerConfig{
		Issuer: "i", KeyID: "k", PrivateKey: testKey(t, 2048), Lifetime: time.Minute,
		AudienceFor: func(string) string { return "a" },
	}
	cases := map[string]func(*JWTIssuerConfig){
		"1024-bit key":    func(c *JWTIssuerConfig) { c.PrivateKey = testKey(t, 1024) },
		"nil key":         func(c *JWTIssuerConfig) { c.PrivateKey = nil },
		"lifetime 10m":    func(c *JWTIssuerConfig) { c.Lifetime = 10 * time.Minute },
		"lifetime zero":   func(c *JWTIssuerConfig) { c.Lifetime = 0 },
		"empty issuer":    func(c *JWTIssuerConfig) { c.Issuer = "" },
		"empty kid":       func(c *JWTIssuerConfig) { c.KeyID = "" },
		"nil AudienceFor": func(c *JWTIssuerConfig) { c.AudienceFor = nil },
	}
	for name, mut := range cases {
		cfg := base
		mut(&cfg)
		if _, err := NewJWTIssuer(cfg); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, err := NewJWTIssuer(base); err != nil {
		t.Fatalf("valid config: %v", err)
	}
}

func TestDefaultScopeMapper(t *testing.T) {
	cases := map[string][]string{
		"read":        {"tickets:read"},
		"write":       {"tickets:read", "tickets:write"},
		"destructive": {"tickets:read", "tickets:delete"},
		"other":       nil,
	}
	for risk, want := range cases {
		got := DefaultScopeMapper("b", "t", risk)
		if strings.Join(got, ",") != strings.Join(want, ",") || (want == nil) != (got == nil) {
			t.Errorf("%s: got %v want %v", risk, got, want)
		}
	}
}
