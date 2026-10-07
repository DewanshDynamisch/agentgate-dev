// Package oidcverify verifies OIDC access tokens (RS256 / ES256 JWTs) against
// an issuer's JWKS. Only the standard library is used.
package oidcverify

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	// ErrUnavailable means verification could not complete because the IdP's
	// discovery/JWKS endpoint could not be reached for an unknown key id.
	// Callers should map it to a 5xx (503), not an authentication failure.
	ErrUnavailable = errors.New("oidcverify: identity provider unavailable")
	// ErrInvalidToken is returned for every token-level rejection. The reason
	// is intentionally not disclosed to callers.
	ErrInvalidToken = errors.New("oidcverify: invalid token")
)

var b64 = base64.RawURLEncoding.Strict()

func ellipticP256() elliptic.Curve { return elliptic.P256() }

const (
	defaultRolePath   = "realm_access.roles"
	defaultClockSkew  = 30 * time.Second
	defaultJWKSMaxAge = time.Hour
	maxTokenBytes     = 16 << 10
)

// Config configures a Verifier.
type Config struct {
	IssuerURL string
	Audience  string
	// JWKSURL is optional; when empty it is discovered from
	// IssuerURL + "/.well-known/openid-configuration".
	JWKSURL string
	// RoleClaimPath is a dot path to a string array; default "realm_access.roles".
	RoleClaimPath string
	HTTPClient    *http.Client
	Now           func() time.Time
	ClockSkew     time.Duration // default 30s
	// JWKSMaxAge is how long cached keys are trusted before a refetch is
	// attempted; default 1h. If refetching fails, keys keep being served until
	// 2*JWKSMaxAge, after which verification fails closed (ErrUnavailable).
	JWKSMaxAge time.Duration
	// AllowInsecureHTTP permits http:// issuer/JWKS URLs. Tests only.
	AllowInsecureHTTP bool
}

// Principal is the verified identity extracted from a token.
type Principal struct {
	Subject  string
	Username string // preferred_username
	Roles    []string
}

// Verifier validates access tokens.
type Verifier struct {
	issuer, audience string
	rolePath         []string
	skew             time.Duration
	now              func() time.Time
	keys             *keySet
}

// New builds a Verifier. It fails if discovery or the initial JWKS fetch fails.
func New(ctx context.Context, cfg Config) (*Verifier, error) {
	if cfg.IssuerURL == "" {
		return nil, errors.New("oidcverify: IssuerURL is required")
	}
	if cfg.Audience == "" {
		return nil, errors.New("oidcverify: Audience is required")
	}
	if err := requireHTTPS(cfg.IssuerURL, cfg.AllowInsecureHTTP); err != nil {
		return nil, err
	}
	if cfg.JWKSURL != "" {
		if err := requireHTTPS(cfg.JWKSURL, cfg.AllowInsecureHTTP); err != nil {
			return nil, err
		}
	}
	c := http.Client{Timeout: httpTimeout}
	if cfg.HTTPClient != nil {
		c = *cfg.HTTPClient
		if c.Timeout == 0 {
			c.Timeout = httpTimeout
		}
	}
	// Never follow a redirect to another host or scheme.
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || req.URL.Host != via[0].URL.Host || req.URL.Scheme != via[0].URL.Scheme {
			return errors.New("oidcverify: redirect refused")
		}
		return nil
	}
	client := &c
	maxAge := cfg.JWKSMaxAge
	if maxAge <= 0 {
		maxAge = defaultJWKSMaxAge
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	skew := cfg.ClockSkew
	if skew <= 0 {
		skew = defaultClockSkew
	}
	path := cfg.RoleClaimPath
	if path == "" {
		path = defaultRolePath
	}
	jwksURL := cfg.JWKSURL
	if jwksURL == "" {
		u, err := discoverJWKSURL(ctx, client, cfg.IssuerURL)
		if err != nil {
			return nil, err
		}
		if err := requireHTTPS(u, cfg.AllowInsecureHTTP); err != nil {
			return nil, err
		}
		jwksURL = u
	}
	ks := newKeySet(jwksURL, client, now, maxAge)
	if err := ks.refresh(ctx); err != nil {
		return nil, err
	}
	return &Verifier{
		issuer: cfg.IssuerURL, audience: cfg.Audience,
		rolePath: strings.Split(path, "."), skew: skew, now: now, keys: ks,
	}, nil
}

// Verify validates rawToken and returns its Principal. Token-level failures
// return ErrInvalidToken; an unreachable IdP (unknown kid) returns ErrUnavailable.
func (v *Verifier) Verify(ctx context.Context, rawToken string) (Principal, error) {
	if rawToken == "" || len(rawToken) > maxTokenBytes {
		return Principal{}, ErrInvalidToken
	}
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 {
		return Principal{}, ErrInvalidToken
	}
	hdrB, err1 := b64.DecodeString(parts[0])
	payB, err2 := b64.DecodeString(parts[1])
	sig, err3 := b64.DecodeString(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return Principal{}, ErrInvalidToken
	}
	var hdr struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ"`
	}
	if json.Unmarshal(hdrB, &hdr) != nil {
		return Principal{}, ErrInvalidToken
	}
	// Algorithm allow-list is enforced before any key is looked up or used.
	if (hdr.Alg != "RS256" && hdr.Alg != "ES256") || hdr.Kid == "" {
		return Principal{}, ErrInvalidToken
	}
	switch strings.ToLower(hdr.Typ) {
	case "", "jwt", "at+jwt":
	default:
		return Principal{}, ErrInvalidToken
	}
	key, err := v.keys.get(ctx, hdr.Kid)
	if err != nil {
		return Principal{}, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	switch pub := key.(type) {
	case *rsa.PublicKey:
		if hdr.Alg != "RS256" || rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig) != nil {
			return Principal{}, ErrInvalidToken
		}
	case *ecdsa.PublicKey:
		// ES256 signatures are raw r||s (JWS), not ASN.1.
		if hdr.Alg != "ES256" || len(sig) != 64 {
			return Principal{}, ErrInvalidToken
		}
		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])
		if !ecdsa.Verify(pub, digest[:], r, s) {
			return Principal{}, ErrInvalidToken
		}
	default:
		return Principal{}, ErrInvalidToken
	}

	dec := json.NewDecoder(strings.NewReader(string(payB)))
	dec.UseNumber()
	var claims map[string]any
	if dec.Decode(&claims) != nil || claims == nil {
		return Principal{}, ErrInvalidToken
	}
	if iss, _ := claims["iss"].(string); iss != v.issuer {
		return Principal{}, ErrInvalidToken
	}
	// Access tokens only: Keycloak ID/refresh/offline/logout tokens carry a
	// payload typ other than Bearer and must not be usable as credentials.
	if typ, present := claims["typ"]; present {
		ts, ok := typ.(string)
		if !ok || !strings.EqualFold(ts, "Bearer") {
			return Principal{}, ErrInvalidToken
		}
	}
	if !v.audienceOK(claims) {
		return Principal{}, ErrInvalidToken
	}
	now := v.now()
	exp, ok := numericDate(claims["exp"])
	if !ok || !now.Add(-v.skew).Before(exp) {
		return Principal{}, ErrInvalidToken
	}
	if raw, present := claims["nbf"]; present {
		nbf, ok := numericDate(raw)
		if !ok || now.Add(v.skew).Before(nbf) {
			return Principal{}, ErrInvalidToken
		}
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return Principal{}, ErrInvalidToken
	}
	username, _ := claims["preferred_username"].(string)
	return Principal{Subject: sub, Username: username, Roles: v.roles(claims)}, nil
}

func (v *Verifier) audienceOK(claims map[string]any) bool {
	switch a := claims["aud"].(type) {
	case string:
		if a == v.audience {
			return true
		}
	case []any:
		for _, e := range a {
			if s, ok := e.(string); ok && s == v.audience {
				return true
			}
		}
	}
	// Keycloak public clients: audience may be absent, authorized party set.
	azp, _ := claims["azp"].(string)
	return azp != "" && azp == v.audience
}

func numericDate(v any) (time.Time, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return time.Time{}, false
	}
	f, err := n.Float64()
	if err != nil || f < 0 || f > 1e12 {
		return time.Time{}, false
	}
	sec := int64(f)
	return time.Unix(sec, int64((f-float64(sec))*1e9)), true
}

func (v *Verifier) roles(claims map[string]any) []string {
	var cur any = claims
	for _, seg := range v.rolePath {
		m, ok := cur.(map[string]any)
		if !ok {
			return []string{}
		}
		if cur, ok = m[seg]; !ok {
			return []string{}
		}
	}
	arr, ok := cur.([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func requireHTTPS(raw string, allowHTTP bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return errors.New("oidcverify: invalid URL")
	}
	if u.Scheme == "https" || (allowHTTP && u.Scheme == "http") {
		return nil
	}
	return errors.New("oidcverify: https is required")
}
