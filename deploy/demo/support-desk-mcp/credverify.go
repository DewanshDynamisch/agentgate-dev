package main

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	clockSkew          = 10 * time.Second
	maxCredLifetimeSec = 300
	jwksRefreshMin     = 30 * time.Second
	jwksHTTPTimeout    = 5 * time.Second
	jwksMaxBytes       = 1 << 20
	minRSABits         = 2048
)

var toolScopes = map[string]string{
	"list_tickets":         "tickets:read",
	"update_ticket_status": "tickets:write",
	"delete_ticket":        "tickets:delete",
}

// Claims is the verified subset of an AgentGate downstream credential.
type Claims struct {
	Sub, Act, Scope, Tool, Exec, JTI string
	Exp                              int64
}

// Verifier validates AgentGate-minted RS256 credentials. This logic is
// intentionally self-contained: this module models a third-party backend.
type Verifier struct {
	jwksURL, issuer, audience string
	client                    *http.Client
	now                       func() time.Time

	keyMu       sync.Mutex
	keys        map[string]*rsa.PublicKey
	lastAttempt time.Time

	mu   sync.Mutex
	seen map[string]int64
}

func NewVerifier(jwksURL, issuer, audience string, httpClient *http.Client) *Verifier {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: jwksHTTPTimeout}
	} else if httpClient.Timeout == 0 {
		c := *httpClient
		c.Timeout = jwksHTTPTimeout
		httpClient = &c
	}
	return &Verifier{
		jwksURL: jwksURL, issuer: issuer, audience: audience,
		client: httpClient, now: time.Now,
		keys: map[string]*rsa.PublicKey{}, seen: map[string]int64{},
	}
}

func decodeSegment(seg string, v any) error {
	raw, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

func (v *Verifier) Verify(ctx context.Context, authorizationHeader, toolName string) (Claims, error) {
	// 1. Bearer prefix.
	const prefix = "Bearer "
	if !strings.HasPrefix(authorizationHeader, prefix) {
		return Claims{}, errors.New("missing bearer credential")
	}
	token := strings.TrimSpace(authorizationHeader[len(prefix):])

	// 2. Structure and header.
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, errors.New("malformed token")
	}
	var hdr struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := decodeSegment(parts[0], &hdr); err != nil {
		return Claims{}, errors.New("malformed token header")
	}
	if hdr.Alg != "RS256" {
		return Claims{}, errors.New("unsupported algorithm")
	}
	if hdr.Kid == "" {
		return Claims{}, errors.New("missing kid")
	}

	// 3. Key lookup.
	pub, err := v.lookupKey(ctx, hdr.Kid)
	if err != nil {
		return Claims{}, err
	}

	// 4. Signature.
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, errors.New("malformed signature")
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		return Claims{}, errors.New("invalid signature")
	}

	var raw struct {
		Iss string `json:"iss"`
		Aud any    `json:"aud"`
		Sub string `json:"sub"`
		Act *struct {
			Sub string `json:"sub"`
		} `json:"act"`
		Scope string `json:"scope"`
		Tool  string `json:"tool"`
		Exec  string `json:"exec"`
		JTI   string `json:"jti"`
		Iat   int64  `json:"iat"`
		Nbf   int64  `json:"nbf"`
		Exp   int64  `json:"exp"`
	}
	if err := decodeSegment(parts[1], &raw); err != nil {
		return Claims{}, errors.New("malformed token claims")
	}

	// 5. Issuer and audience.
	if raw.Iss != v.issuer {
		return Claims{}, errors.New("invalid issuer")
	}
	if !audienceMatches(raw.Aud, v.audience) {
		return Claims{}, errors.New("invalid audience")
	}

	// 6. Time bounds.
	now := v.now()
	if raw.Iat == 0 || raw.Nbf == 0 || raw.Exp == 0 {
		return Claims{}, errors.New("missing time claims")
	}
	if raw.Nbf > now.Add(clockSkew).Unix() {
		return Claims{}, errors.New("credential not yet valid")
	}
	if raw.Exp <= now.Add(-clockSkew).Unix() {
		return Claims{}, errors.New("credential expired")
	}
	if raw.Exp-raw.Iat > maxCredLifetimeSec {
		return Claims{}, errors.New("credential lifetime too long")
	}

	// 7. Tool binding.
	if raw.Tool != toolName {
		return Claims{}, errors.New("credential not issued for this tool")
	}

	// 8. Scope.
	required, ok := toolScopes[toolName]
	if !ok {
		return Claims{}, errors.New("no scope defined for tool")
	}
	if !hasScope(raw.Scope, required) {
		return Claims{}, fmt.Errorf("missing scope %s", required)
	}

	// 9. Replay.
	if raw.JTI == "" {
		return Claims{}, errors.New("missing jti")
	}
	if !v.consumeJTI(raw.JTI, raw.Exp, now) {
		return Claims{}, errors.New("credential replayed")
	}

	c := Claims{Sub: raw.Sub, Scope: raw.Scope, Tool: raw.Tool, Exec: raw.Exec, JTI: raw.JTI, Exp: raw.Exp}
	if raw.Act != nil {
		c.Act = raw.Act.Sub
	}
	return c, nil
}

func audienceMatches(aud any, want string) bool {
	switch a := aud.(type) {
	case string:
		return a == want
	case []any:
		if len(a) != 1 {
			return false
		}
		s, _ := a[0].(string)
		return s == want
	}
	return false
}

func hasScope(scope, required string) bool {
	for _, s := range strings.Fields(scope) {
		if s == required {
			return true
		}
	}
	return false
}

func (v *Verifier) consumeJTI(jti string, exp int64, now time.Time) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	cutoff := now.Add(-clockSkew).Unix()
	for id, e := range v.seen {
		if e <= cutoff {
			delete(v.seen, id)
		}
	}
	if _, dup := v.seen[jti]; dup {
		return false
	}
	v.seen[jti] = exp
	return true
}

func (v *Verifier) lookupKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.keyMu.Lock()
	defer v.keyMu.Unlock()
	if k, ok := v.keys[kid]; ok {
		return k, nil
	}
	now := v.now()
	if !v.lastAttempt.IsZero() && now.Sub(v.lastAttempt) < jwksRefreshMin {
		return nil, errors.New("unknown signing key")
	}
	v.lastAttempt = now
	keys, err := v.fetchJWKS(ctx)
	if err != nil {
		return nil, fmt.Errorf("jwks unavailable: %w", err)
	}
	v.keys = keys
	if k, ok := v.keys[kid]; ok {
		return k, nil
	}
	return nil, errors.New("unknown signing key")
}

func (v *Verifier) fetchJWKS(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, jwksMaxBytes))
	if err != nil {
		return nil, err
	}
	var set struct {
		Keys []struct {
			Kty string `json:"kty"`
			Use string `json:"use"`
			Alg string `json:"alg"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, err
	}
	out := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || k.Kid == "" || (k.Use != "" && k.Use != "sig") || (k.Alg != "" && k.Alg != "RS256") {
			continue
		}
		nb, err1 := base64.RawURLEncoding.DecodeString(k.N)
		eb, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil {
			continue
		}
		n := new(big.Int).SetBytes(nb)
		e := new(big.Int).SetBytes(eb)
		if n.BitLen() < minRSABits || !e.IsInt64() || e.Int64() < 3 || e.Int64() > 1<<31-1 {
			continue
		}
		out[k.Kid] = &rsa.PublicKey{N: n, E: int(e.Int64())}
	}
	return out, nil
}
