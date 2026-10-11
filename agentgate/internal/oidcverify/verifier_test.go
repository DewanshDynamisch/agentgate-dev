package oidcverify

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var tb64 = base64.RawURLEncoding

type testKey struct {
	kid string
	rsa *rsa.PrivateKey
	ec  *ecdsa.PrivateKey
}

func newRSAKey(t *testing.T, kid string) testKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return testKey{kid: kid, rsa: k}
}

func newECKey(t *testing.T, kid string) testKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return testKey{kid: kid, ec: k}
}

func (k testKey) jwk() map[string]string {
	if k.rsa != nil {
		return map[string]string{
			"kty": "RSA", "kid": k.kid, "use": "sig", "alg": "RS256",
			"n": tb64.EncodeToString(k.rsa.N.Bytes()),
			"e": tb64.EncodeToString(big.NewInt(int64(k.rsa.E)).Bytes()),
		}
	}
	return map[string]string{
		"kty": "EC", "kid": k.kid, "use": "sig", "alg": "ES256", "crv": "P-256",
		"x": tb64.EncodeToString(k.ec.X.FillBytes(make([]byte, 32))),
		"y": tb64.EncodeToString(k.ec.Y.FillBytes(make([]byte, 32))),
	}
}

type idp struct {
	srv      *httptest.Server
	mu       sync.Mutex
	keys     []testKey
	jwksHits atomic.Int32
	discHits atomic.Int32
	extra    []map[string]string
	discIss  string
}

func newIDP(t *testing.T, keys ...testKey) *idp {
	t.Helper()
	p := &idp{keys: keys}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		p.discHits.Add(1)
		iss := p.srv.URL
		if p.discIss != "" {
			iss = p.discIss
		}
		doc := map[string]string{"issuer": iss, "jwks_uri": p.srv.URL + "/jwks"}
		_ = json.NewEncoder(w).Encode(doc)
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		p.jwksHits.Add(1)
		p.mu.Lock()
		defer p.mu.Unlock()
		var ks []map[string]string
		for _, k := range p.keys {
			ks = append(ks, k.jwk())
		}
		ks = append(ks, p.extra...)
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": ks})
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *idp) addKey(k testKey) {
	p.mu.Lock()
	p.keys = append(p.keys, k)
	p.mu.Unlock()
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newClock() *clock { return &clock{t: time.Unix(1_800_000_000, 0)} }

func sign(t *testing.T, k testKey, alg string, claims map[string]any) string {
	t.Helper()
	return signHdr(t, k, map[string]any{"alg": alg, "typ": "JWT", "kid": k.kid}, claims)
}

func signHdr(t *testing.T, k testKey, hdr, claims map[string]any) string {
	t.Helper()
	hb, _ := json.Marshal(hdr)
	cb, _ := json.Marshal(claims)
	in := tb64.EncodeToString(hb) + "." + tb64.EncodeToString(cb)
	var sig []byte
	h := sha256.Sum256([]byte(in))
	if k.rsa != nil {
		s, err := rsa.SignPKCS1v15(rand.Reader, k.rsa, crypto.SHA256, h[:])
		if err != nil {
			t.Fatal(err)
		}
		sig = s
	} else {
		r, s, err := ecdsa.Sign(rand.Reader, k.ec, h[:])
		if err != nil {
			t.Fatal(err)
		}
		sig = append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
	}
	return in + "." + tb64.EncodeToString(sig)
}

func baseClaims(p *idp, c *clock) map[string]any {
	return map[string]any{
		"iss": p.srv.URL, "aud": "agentgate-admin", "sub": "user-1",
		"preferred_username": "alice",
		"exp":                c.now().Add(5 * time.Minute).Unix(),
		"nbf":                c.now().Add(-time.Minute).Unix(),
		"realm_access":       map[string]any{"roles": []string{"admin", "viewer"}},
	}
}

func newVerifier(t *testing.T, p *idp, c *clock, mod func(*Config)) *Verifier {
	t.Helper()
	cfg := Config{IssuerURL: p.srv.URL, Audience: "agentgate-admin", Now: c.now, AllowInsecureHTTP: true}
	if mod != nil {
		mod(&cfg)
	}
	v, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return v
}

func TestValidRS256(t *testing.T) {
	k := newRSAKey(t, "r1")
	p, c := newIDP(t, k), newClock()
	v := newVerifier(t, p, c, nil)
	pr, err := v.Verify(context.Background(), sign(t, k, "RS256", baseClaims(p, c)))
	if err != nil {
		t.Fatal(err)
	}
	if pr.Subject != "user-1" || pr.Username != "alice" {
		t.Fatalf("principal %+v", pr)
	}
	if len(pr.Roles) != 2 || pr.Roles[0] != "admin" || pr.Roles[1] != "viewer" {
		t.Fatalf("roles %v", pr.Roles)
	}
}

func TestValidES256(t *testing.T) {
	k := newECKey(t, "e1")
	p, c := newIDP(t, k), newClock()
	v := newVerifier(t, p, c, nil)
	if _, err := v.Verify(context.Background(), sign(t, k, "ES256", baseClaims(p, c))); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitJWKSURLSkipsDiscovery(t *testing.T) {
	k := newRSAKey(t, "r1")
	p, c := newIDP(t, k), newClock()
	v := newVerifier(t, p, c, func(cfg *Config) { cfg.JWKSURL = p.srv.URL + "/jwks" })
	if p.discHits.Load() != 0 {
		t.Fatal("discovery used despite JWKSURL")
	}
	if _, err := v.Verify(context.Background(), sign(t, k, "RS256", baseClaims(p, c))); err != nil {
		t.Fatal(err)
	}
}

func TestRejections(t *testing.T) {
	k := newRSAKey(t, "r1")
	ek := newECKey(t, "e1")
	p, c := newIDP(t, k, ek), newClock()
	v := newVerifier(t, p, c, nil)
	ctx := context.Background()

	mod := func(f func(m map[string]any)) string {
		m := baseClaims(p, c)
		f(m)
		return sign(t, k, "RS256", m)
	}
	good := sign(t, k, "RS256", baseClaims(p, c))
	other := sign(t, k, "RS256", map[string]any{"iss": p.srv.URL, "aud": "agentgate-admin", "sub": "root", "exp": c.now().Add(time.Hour).Unix()})
	gp, op := strings.Split(good, "."), strings.Split(other, ".")
	noneHdr, _ := json.Marshal(map[string]any{"alg": "none", "kid": "r1"})
	noneBody, _ := json.Marshal(baseClaims(p, c))

	cases := map[string]string{
		"expired":              mod(func(m map[string]any) { m["exp"] = c.now().Add(-time.Hour).Unix() }),
		"nbf future":           mod(func(m map[string]any) { m["nbf"] = c.now().Add(time.Hour).Unix() }),
		"wrong iss":            mod(func(m map[string]any) { m["iss"] = "https://evil.example" }),
		"wrong aud":            mod(func(m map[string]any) { m["aud"] = "other" }),
		"aud array":            mod(func(m map[string]any) { m["aud"] = []string{"x", "y"} }),
		"no exp":               mod(func(m map[string]any) { delete(m, "exp") }),
		"no sub":               mod(func(m map[string]any) { delete(m, "sub") }),
		"alg none":             tb64.EncodeToString(noneHdr) + "." + tb64.EncodeToString(noneBody) + ".",
		"alg HS256":            signHdr(t, k, map[string]any{"alg": "HS256", "kid": "r1"}, baseClaims(p, c)),
		"alg PS256":            signHdr(t, k, map[string]any{"alg": "PS256", "kid": "r1"}, baseClaims(p, c)),
		"ES256 hdr on RSA key": signHdr(t, k, map[string]any{"alg": "ES256", "kid": "r1"}, baseClaims(p, c)),
		"RS256 hdr on EC key":  signHdr(t, ek, map[string]any{"alg": "RS256", "kid": "e1"}, baseClaims(p, c)),
		"garbage":              "not-a-jwt",
		"two parts":            "a.b",
		"four parts":           "a.b.c.d",
		"empty":                "",
		"padded sig":           good + "=",
		"tampered":             gp[0] + "." + op[1] + "." + gp[2],
		"wrong signing key":    sign(t, newRSAKey(t, "r1"), "RS256", baseClaims(p, c)),
	}
	for name, tok := range cases {
		if _, err := v.Verify(ctx, tok); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSkew(t *testing.T) {
	k := newRSAKey(t, "r1")
	p, c := newIDP(t, k), newClock()
	v := newVerifier(t, p, c, nil)
	ctx := context.Background()
	m := baseClaims(p, c)
	m["exp"] = c.now().Add(-10 * time.Second).Unix() // inside 30s skew
	if _, err := v.Verify(ctx, sign(t, k, "RS256", m)); err != nil {
		t.Fatalf("within skew rejected: %v", err)
	}
	m["exp"] = c.now().Add(-60 * time.Second).Unix()
	if _, err := v.Verify(ctx, sign(t, k, "RS256", m)); err == nil {
		t.Fatal("beyond skew accepted")
	}
	m = baseClaims(p, c)
	m["nbf"] = c.now().Add(10 * time.Second).Unix()
	if _, err := v.Verify(ctx, sign(t, k, "RS256", m)); err != nil {
		t.Fatalf("nbf within skew rejected: %v", err)
	}
}

func TestAudienceArrayAndAZP(t *testing.T) {
	k := newRSAKey(t, "r1")
	p, c := newIDP(t, k), newClock()
	v := newVerifier(t, p, c, nil)
	ctx := context.Background()

	m := baseClaims(p, c)
	m["aud"] = []string{"account", "agentgate-admin"}
	if _, err := v.Verify(ctx, sign(t, k, "RS256", m)); err != nil {
		t.Fatalf("aud array: %v", err)
	}
	m = baseClaims(p, c)
	m["aud"] = "account"
	m["azp"] = "agentgate-admin"
	if _, err := v.Verify(ctx, sign(t, k, "RS256", m)); err != nil {
		t.Fatalf("azp fallback: %v", err)
	}
	m["azp"] = "someone-else"
	if _, err := v.Verify(ctx, sign(t, k, "RS256", m)); err == nil {
		t.Fatal("wrong azp accepted")
	}
}

func TestRoles(t *testing.T) {
	k := newRSAKey(t, "r1")
	p, c := newIDP(t, k), newClock()
	ctx := context.Background()
	v := newVerifier(t, p, c, nil)

	m := baseClaims(p, c)
	delete(m, "realm_access")
	pr, err := v.Verify(ctx, sign(t, k, "RS256", m))
	if err != nil || len(pr.Roles) != 0 {
		t.Fatalf("missing path: %v %v", pr, err)
	}
	m["realm_access"] = map[string]any{"roles": "admin"} // wrong type -> empty
	pr, err = v.Verify(ctx, sign(t, k, "RS256", m))
	if err != nil || len(pr.Roles) != 0 {
		t.Fatalf("wrong type: %v %v", pr, err)
	}
	m["realm_access"] = map[string]any{"roles": []any{"admin", 5, "viewer"}} // non-strings dropped
	pr, _ = v.Verify(ctx, sign(t, k, "RS256", m))
	if len(pr.Roles) != 2 {
		t.Fatalf("mixed types: %v", pr.Roles)
	}

	v2 := newVerifier(t, p, c, func(cfg *Config) { cfg.RoleClaimPath = "groups" })
	m = baseClaims(p, c)
	m["groups"] = []string{"g1"}
	pr, err = v2.Verify(ctx, sign(t, k, "RS256", m))
	if err != nil || len(pr.Roles) != 1 || pr.Roles[0] != "g1" {
		t.Fatalf("custom path: %v %v", pr, err)
	}
}

func TestUnknownKidRefetchesOnce(t *testing.T) {
	k1, k2 := newRSAKey(t, "r1"), newRSAKey(t, "r2")
	p, c := newIDP(t, k1), newClock()
	v := newVerifier(t, p, c, nil)
	ctx := context.Background()
	startHits := p.jwksHits.Load()

	p.addKey(k2) // rotation
	// Inside the throttle window an unknown kid must not trigger a refetch.
	if _, err := v.Verify(ctx, sign(t, k2, "RS256", baseClaims(p, c))); err == nil {
		t.Fatal("unknown kid accepted inside throttle window")
	}
	if p.jwksHits.Load() != startHits {
		t.Fatal("refetched inside throttle window")
	}
	c.advance(31 * time.Second)
	if _, err := v.Verify(ctx, sign(t, k2, "RS256", baseClaims(p, c))); err != nil {
		t.Fatalf("after refetch: %v", err)
	}
	if got := p.jwksHits.Load(); got != startHits+1 {
		t.Fatalf("jwks hits %d want %d", got, startHits+1)
	}
	if _, err := v.Verify(ctx, sign(t, k2, "RS256", baseClaims(p, c))); err != nil {
		t.Fatal(err)
	}
	if got := p.jwksHits.Load(); got != startHits+1 {
		t.Fatalf("known kid refetched: %d", got)
	}
}

func TestUnknownKidIdPDown(t *testing.T) {
	k1, k2 := newRSAKey(t, "r1"), newRSAKey(t, "r2")
	p, c := newIDP(t, k1), newClock()
	v := newVerifier(t, p, c, nil)
	ctx := context.Background()
	p.srv.Close()
	c.advance(31 * time.Second)

	_, err := v.Verify(ctx, sign(t, k2, "RS256", baseClaims(p, c)))
	if err == nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	// Known kid still verifies from cache while the IdP is down.
	if _, err := v.Verify(ctx, sign(t, k1, "RS256", baseClaims(p, c))); err != nil {
		t.Fatalf("cached key: %v", err)
	}
}

func TestBadTokenIsNotUnavailable(t *testing.T) {
	k := newRSAKey(t, "r1")
	p, c := newIDP(t, k), newClock()
	v := newVerifier(t, p, c, nil)
	_, err := v.Verify(context.Background(), "garbage")
	if err == nil || errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestNewFailsWhenIdPUnreachable(t *testing.T) {
	p := newIDP(t, newRSAKey(t, "r1"))
	url := p.srv.URL
	p.srv.Close()
	if _, err := New(context.Background(), Config{IssuerURL: url, Audience: "a", AllowInsecureHTTP: true}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := New(context.Background(), Config{IssuerURL: url, Audience: "a", JWKSURL: url + "/jwks", AllowInsecureHTTP: true}); err == nil {
		t.Fatal("expected error with explicit JWKS URL")
	}
}

func TestNewValidatesConfig(t *testing.T) {
	if _, err := New(context.Background(), Config{Audience: "a"}); err == nil {
		t.Fatal("missing issuer accepted")
	}
	if _, err := New(context.Background(), Config{IssuerURL: "https://x"}); err == nil {
		t.Fatal("missing audience accepted")
	}
}

func TestOversizedJWKSRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"keys":[],"pad":"`))
		_, _ = w.Write([]byte(strings.Repeat("a", 1<<20)))
		_, _ = w.Write([]byte(`"}`))
	}))
	defer srv.Close()
	if _, err := New(context.Background(), Config{IssuerURL: "https://x", Audience: "a", JWKSURL: srv.URL, AllowInsecureHTTP: true}); err == nil {
		t.Fatal("oversized JWKS accepted")
	}
}

func TestWeakRSAKeyIgnored(t *testing.T) {
	weak, _ := rsa.GenerateKey(rand.Reader, 1024)
	wk := testKey{kid: "w", rsa: weak}
	p, c := newIDP(t, newRSAKey(t, "r1"), wk), newClock()
	v := newVerifier(t, p, c, nil)
	if _, err := v.Verify(context.Background(), sign(t, wk, "RS256", baseClaims(p, c))); err == nil {
		t.Fatal("1024-bit key accepted")
	}
}
