package oidcverify

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func (p *idp) removeKey(kid string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []testKey
	for _, k := range p.keys {
		if k.kid != kid {
			out = append(out, k)
		}
	}
	p.keys = out
}

func tokenWith(t *testing.T, k testKey, p *idp, c *clock, hdrTyp string, mod func(map[string]any)) string {
	t.Helper()
	m := baseClaims(p, c)
	if mod != nil {
		mod(m)
	}
	h := map[string]any{"alg": "RS256", "kid": k.kid}
	if hdrTyp != "" {
		h["typ"] = hdrTyp
	}
	return signHdr(t, k, h, m)
}

func TestTokenTypeChecks(t *testing.T) {
	k := newRSAKey(t, "r1")
	p, c := newIDP(t, k), newClock()
	v := newVerifier(t, p, c, nil)
	ctx := context.Background()
	for _, typ := range []string{"ID", "Refresh", "Offline", "Logout", "id", "refresh", "OFFLINE"} {
		tok := tokenWith(t, k, p, c, "", func(m map[string]any) { m["typ"] = typ })
		if _, err := v.Verify(ctx, tok); err == nil {
			t.Errorf("payload typ %q accepted", typ)
		}
	}
	if _, err := v.Verify(ctx, tokenWith(t, k, p, c, "JOSE+foo", nil)); err == nil {
		t.Error("header typ JOSE+foo accepted")
	}
	for _, ht := range []string{"", "JWT", "jwt", "at+jwt", "AT+JWT"} {
		if _, err := v.Verify(ctx, tokenWith(t, k, p, c, ht, nil)); err != nil {
			t.Errorf("header typ %q rejected: %v", ht, err)
		}
	}
	for _, pt := range []string{"Bearer", "bearer"} {
		tok := tokenWith(t, k, p, c, "", func(m map[string]any) { m["typ"] = pt })
		if _, err := v.Verify(ctx, tok); err != nil {
			t.Errorf("payload typ %q rejected: %v", pt, err)
		}
	}
	// A refresh token with azp == audience must not slip in via the azp fallback.
	tok := tokenWith(t, k, p, c, "", func(m map[string]any) {
		m["typ"] = "Refresh"
		m["aud"] = "account"
		m["azp"] = "agentgate-admin"
	})
	if _, err := v.Verify(ctx, tok); err == nil {
		t.Error("refresh token via azp accepted")
	}
}

func TestJWKSExpiryRotatedOutKeyRejected(t *testing.T) {
	k1, k2 := newRSAKey(t, "r1"), newRSAKey(t, "r2")
	p, c := newIDP(t, k1, k2), newClock()
	v := newVerifier(t, p, c, nil) // default max age 1h
	ctx := context.Background()
	if _, err := v.Verify(ctx, sign(t, k2, "RS256", baseClaims(p, c))); err != nil {
		t.Fatal(err)
	}
	p.removeKey("r2") // revoked at the IdP
	c.advance(30 * time.Minute)
	if _, err := v.Verify(ctx, sign(t, k2, "RS256", baseClaims(p, c))); err != nil {
		t.Fatalf("within max age should still use cache: %v", err)
	}
	c.advance(31 * time.Minute)
	if _, err := v.Verify(ctx, sign(t, k2, "RS256", baseClaims(p, c))); err == nil {
		t.Fatal("revoked key accepted after JWKS expiry")
	}
	if _, err := v.Verify(ctx, sign(t, k1, "RS256", baseClaims(p, c))); err != nil {
		t.Fatalf("surviving key: %v", err)
	}
}

func TestJWKSExpiryIdPDown(t *testing.T) {
	k1 := newRSAKey(t, "r1")
	p, c := newIDP(t, k1), newClock()
	v := newVerifier(t, p, c, func(cfg *Config) { cfg.JWKSMaxAge = time.Hour })
	ctx := context.Background()
	p.srv.Close()
	c.advance(61 * time.Minute)
	if _, err := v.Verify(ctx, sign(t, k1, "RS256", baseClaims(p, c))); err != nil {
		t.Fatalf("stale-but-within-2x should verify: %v", err)
	}
	c.advance(60 * time.Minute) // 121m since last good fetch
	_, err := v.Verify(ctx, sign(t, k1, "RS256", baseClaims(p, c)))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable past 2x max age, got %v", err)
	}
}

func TestCancelledContextDoesNotPoisonRefresh(t *testing.T) {
	k1, k2 := newRSAKey(t, "r1"), newRSAKey(t, "r2")
	p, c := newIDP(t, k1), newClock()
	v := newVerifier(t, p, c, nil)
	p.addKey(k2)
	c.advance(31 * time.Second)
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := v.Verify(cctx, sign(t, k2, "RS256", baseClaims(p, c))); err != nil {
		t.Fatalf("cancelled caller ctx broke refresh: %v", err)
	}
}

func TestDuplicateKidFailsFetchKeepsOldKeys(t *testing.T) {
	k1 := newRSAKey(t, "r1")
	p, c := newIDP(t, k1), newClock()
	v := newVerifier(t, p, c, nil)
	ctx := context.Background()
	p.addKey(newRSAKey(t, "r1")) // duplicate kid
	r9 := newRSAKey(t, "r9")
	p.addKey(r9)
	c.advance(31 * time.Second)
	_, err := v.Verify(ctx, sign(t, r9, "RS256", baseClaims(p, c)))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if _, err := v.Verify(ctx, sign(t, k1, "RS256", baseClaims(p, c))); err != nil {
		t.Fatalf("previous keys lost: %v", err)
	}
	p2 := newIDP(t, newRSAKey(t, "d"), newRSAKey(t, "d"))
	if _, err := New(ctx, Config{IssuerURL: p2.srv.URL, Audience: "a", AllowInsecureHTTP: true}); err == nil {
		t.Fatal("duplicate kid accepted at startup")
	}
}

func TestEncAndP384KeysIgnored(t *testing.T) {
	good := newRSAKey(t, "r1")
	enc := newRSAKey(t, "enc1")
	p, c := newIDP(t, good), newClock()
	ej := enc.jwk()
	ej["use"] = "enc"
	p.extra = append(p.extra, ej)
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	p.extra = append(p.extra, map[string]string{
		"kty": "EC", "kid": "p384", "crv": "P-384",
		"x": tb64.EncodeToString(p384.X.FillBytes(make([]byte, 48))),
		"y": tb64.EncodeToString(p384.Y.FillBytes(make([]byte, 48))),
	})
	v := newVerifier(t, p, c, nil)
	if _, err := v.Verify(context.Background(), sign(t, enc, "RS256", baseClaims(p, c))); err == nil {
		t.Error("use=enc key accepted")
	}
	fake := newECKey(t, "p384")
	if _, err := v.Verify(context.Background(), sign(t, fake, "ES256", baseClaims(p, c))); err == nil {
		t.Error("P-384 kid accepted")
	}
}

func TestEmptyAudArrayAndKidless(t *testing.T) {
	k := newRSAKey(t, "r1")
	p, c := newIDP(t, k), newClock()
	v := newVerifier(t, p, c, nil)
	ctx := context.Background()
	m := baseClaims(p, c)
	m["aud"] = []string{}
	if _, err := v.Verify(ctx, sign(t, k, "RS256", m)); err == nil {
		t.Error("empty aud without azp accepted")
	}
	m["azp"] = "agentgate-admin"
	if _, err := v.Verify(ctx, sign(t, k, "RS256", m)); err != nil {
		t.Errorf("empty aud with matching azp rejected: %v", err)
	}
	tok := signHdr(t, k, map[string]any{"alg": "RS256"}, baseClaims(p, c))
	if _, err := v.Verify(ctx, tok); err == nil {
		t.Error("kid-less token accepted")
	}
}

func TestDiscoveryIssuerMismatch(t *testing.T) {
	p := newIDP(t, newRSAKey(t, "r1"))
	p.discIss = "https://evil.example"
	if _, err := New(context.Background(), Config{IssuerURL: p.srv.URL, Audience: "a", AllowInsecureHTTP: true}); err == nil {
		t.Fatal("issuer mismatch accepted")
	}
}

func TestRequireHTTPS(t *testing.T) {
	p := newIDP(t, newRSAKey(t, "r1"))
	if _, err := New(context.Background(), Config{IssuerURL: p.srv.URL, Audience: "a"}); err == nil {
		t.Error("http issuer accepted without AllowInsecureHTTP")
	}
	if _, err := New(context.Background(), Config{IssuerURL: "https://idp.example", Audience: "a", JWKSURL: p.srv.URL + "/jwks"}); err == nil {
		t.Error("http JWKS URL accepted without AllowInsecureHTTP")
	}
}

func TestCrossHostRedirectRefused(t *testing.T) {
	target := newIDP(t, newRSAKey(t, "r1"))
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.srv.URL+"/jwks", http.StatusFound)
	}))
	defer redir.Close()
	// 127.0.0.1:port differs by port, so the host differs.
	_, err := New(context.Background(), Config{IssuerURL: "https://x", Audience: "a", JWKSURL: redir.URL, AllowInsecureHTTP: true})
	if err == nil {
		t.Fatal("cross-host redirect followed")
	}
	if target.jwksHits.Load() != 0 {
		t.Fatal("redirect target was contacted")
	}
}
