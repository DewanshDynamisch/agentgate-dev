package credential

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/Dynamisch-LLC/agentgate/internal/decision"
)

const (
	minKeyBits  = 2048
	maxLifetime = 5 * time.Minute
)

// ScopeMapper maps a tool's backend, name and risk to downstream scopes.
// An empty result means no credential may be issued.
type ScopeMapper func(backendID, toolName, risk string) []string

// DefaultScopeMapper maps risk levels to ticket scopes; unknown risk yields nil.
func DefaultScopeMapper(_, _, risk string) []string {
	switch risk {
	case "read":
		return []string{"tickets:read"}
	case "write":
		return []string{"tickets:read", "tickets:write"}
	case "destructive":
		return []string{"tickets:read", "tickets:delete"}
	default:
		return nil
	}
}

// JWTIssuerConfig configures a [JWTIssuer].
type JWTIssuerConfig struct {
	Issuer      string
	KeyID       string
	PrivateKey  *rsa.PrivateKey
	Lifetime    time.Duration
	AudienceFor func(backendID string) string
	Scopes      ScopeMapper
	Now         func() time.Time
}

// JWTIssuer mints per-call, short-lived RS256 JWTs as downstream credentials.
type JWTIssuer struct {
	cfg  JWTIssuerConfig
	jwks []byte
}

var _ Issuer = (*JWTIssuer)(nil)

// NewJWTIssuer validates cfg and returns an issuer.
func NewJWTIssuer(cfg JWTIssuerConfig) (*JWTIssuer, error) {
	switch {
	case cfg.PrivateKey == nil:
		return nil, errors.New("credential: private key required")
	case cfg.PrivateKey.N.BitLen() < minKeyBits:
		return nil, fmt.Errorf("credential: RSA key must be at least %d bits", minKeyBits)
	case cfg.Issuer == "":
		return nil, errors.New("credential: issuer required")
	case cfg.KeyID == "":
		return nil, errors.New("credential: key id required")
	case cfg.Lifetime <= 0 || cfg.Lifetime > maxLifetime:
		return nil, fmt.Errorf("credential: lifetime must be in (0, %s]", maxLifetime)
	case cfg.AudienceFor == nil:
		return nil, errors.New("credential: AudienceFor required")
	}
	if cfg.Scopes == nil {
		cfg.Scopes = DefaultScopeMapper
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	pub := &cfg.PrivateKey.PublicKey
	jwks, err := json.Marshal(map[string]any{"keys": []map[string]string{{
		"kty": "RSA",
		"use": "sig",
		"alg": "RS256",
		"kid": cfg.KeyID,
		"n":   b64(pub.N.Bytes()),
		"e":   b64(big.NewInt(int64(pub.E)).Bytes()),
	}}})
	if err != nil {
		return nil, fmt.Errorf("credential: build JWKS: %w", err)
	}
	return &JWTIssuer{cfg: cfg, jwks: jwks}, nil
}

// JWKS returns a JWK Set containing the issuer's public key.
func (j *JWTIssuer) JWKS() []byte {
	return append([]byte(nil), j.jwks...)
}

// Issue mints a JWT for an already-allowed call. It never logs the token.
func (j *JWTIssuer) Issue(_ context.Context, req decision.Request, res decision.Result) (Credential, error) {
	if res.Decision != decision.Allow {
		return Credential{}, errors.New("credential: issue called on non-allow")
	}
	scopes := j.cfg.Scopes(req.Tool.BackendID, req.Tool.Name, req.Classification.Risk)
	if len(scopes) == 0 {
		return Credential{}, errors.New("credential: no scopes for tool")
	}
	aud := j.cfg.AudienceFor(req.Tool.BackendID)
	if aud == "" {
		return Credential{}, errors.New("credential: no audience for backend")
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return Credential{}, fmt.Errorf("credential: jti: %w", err)
	}
	jti := hex.EncodeToString(raw)

	now := j.cfg.Now()
	claims := map[string]any{
		"iss":     j.cfg.Issuer,
		"aud":     aud,
		"sub":     req.Identity.AgentID,
		"scope":   strings.Join(scopes, " "),
		"tool":    req.Tool.Name,
		"backend": req.Tool.BackendID,
		"exec":    req.ExecutionID,
		"jti":     jti,
		"iat":     now.Unix(),
		"nbf":     now.Unix(),
		"exp":     now.Add(j.cfg.Lifetime).Unix(),
	}
	if req.Identity.OnBehalfOf != "" {
		claims["act"] = map[string]string{"sub": req.Identity.OnBehalfOf}
	}
	hb, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": j.cfg.KeyID})
	if err != nil {
		return Credential{}, fmt.Errorf("credential: header: %w", err)
	}
	cb, err := json.Marshal(claims)
	if err != nil {
		return Credential{}, fmt.Errorf("credential: claims: %w", err)
	}
	signingInput := b64(hb) + "." + b64(cb)
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, j.cfg.PrivateKey, crypto.SHA256, sum[:])
	if err != nil {
		return Credential{}, fmt.Errorf("credential: sign: %w", err)
	}
	return Credential{
		HeaderName:  "Authorization",
		HeaderValue: "Bearer " + signingInput + "." + b64(sig),
		Ref:         "jti:" + jti + ";kid:" + j.cfg.KeyID,
	}, nil
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
