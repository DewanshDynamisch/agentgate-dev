package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testIssuer   = "agentgate"
	testAudience = "support-desk"
	testKid      = "kid-1"
)

type testEnv struct {
	key      *rsa.PrivateKey
	srv      *httptest.Server
	hits     *int32
	verifier *Verifier
	clock    *time.Time
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func jwkFor(kid string, pub *rsa.PublicKey) map[string]string {
	return map[string]string{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": kid,
		"n": b64(pub.N.Bytes()),
		"e": b64(big.NewInt(int64(pub.E)).Bytes()),
	}
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var hits int32
	keys := []map[string]string{jwkFor(testKid, &key.PublicKey)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	}))
	t.Cleanup(srv.Close)
	v := NewVerifier(srv.URL, testIssuer, testAudience, srv.Client())
	clock := time.Now()
	v.now = func() time.Time { return clock }
	return &testEnv{key: key, srv: srv, hits: &hits, verifier: v, clock: &clock}
}

func signWith(t *testing.T, key *rsa.PrivateKey, header, claims map[string]any) string {
	t.Helper()
	hb, _ := json.Marshal(header)
	cb, _ := json.Marshal(claims)
	signingInput := b64(hb) + "." + b64(cb)
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signingInput + "." + b64(sig)
}

var jtiCounter int64

func validClaims(now time.Time) map[string]any {
	return map[string]any{
		"iss": testIssuer, "aud": testAudience, "sub": "user-1",
		"act":   map[string]any{"sub": "agent-1"},
		"scope": "tickets:read tickets:write tickets:delete",
		"tool":  "delete_ticket", "backend": "support-desk", "exec": "exec-1",
		"jti": "jti-" + string(rune('a'+atomic.AddInt64(&jtiCounter, 1)%26)) + now.Format(time.RFC3339Nano),
		"iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(60 * time.Second).Unix(),
	}
}

func rs256Header(kid string) map[string]any {
	return map[string]any{"alg": "RS256", "typ": "JWT", "kid": kid}
}

func (e *testEnv) verify(t *testing.T, token, tool string) (Claims, error) {
	t.Helper()
	return e.verifier.Verify(context.Background(), "Bearer "+token, tool)
}

func TestVerify_ValidToken(t *testing.T) {
	e := newTestEnv(t)
	c := validClaims(*e.clock)
	got, err := e.verify(t, signWith(t, e.key, rs256Header(testKid), c), "delete_ticket")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Sub != "user-1" || got.Act != "agent-1" || got.Tool != "delete_ticket" || got.Exec != "exec-1" ||
		got.JTI != c["jti"] || got.Exp != c["exp"].(int64) || !strings.Contains(got.Scope, "tickets:delete") {
		t.Fatalf("unexpected claims: %+v", got)
	}
}

func TestVerify_RejectsBadHeaderOrShape(t *testing.T) {
	e := newTestEnv(t)
	tok := signWith(t, e.key, rs256Header(testKid), validClaims(*e.clock))
	if _, err := e.verifier.Verify(context.Background(), tok, "delete_ticket"); err == nil {
		t.Fatal("expected error for missing Bearer prefix")
	}
	if _, err := e.verify(t, "a.b", "delete_ticket"); err == nil {
		t.Fatal("expected error for 2 segments")
	}
	if _, err := e.verify(t, signWith(t, e.key, rs256Header(""), validClaims(*e.clock)), "delete_ticket"); err == nil {
		t.Fatal("expected error for empty kid")
	}
}

func TestVerify_WrongSignature(t *testing.T) {
	e := newTestEnv(t)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	tok := signWith(t, other, rs256Header(testKid), validClaims(*e.clock))
	if _, err := e.verify(t, tok, "delete_ticket"); err == nil {
		t.Fatal("expected signature failure")
	}
}

func TestVerify_AlgNoneAndHS256(t *testing.T) {
	e := newTestEnv(t)
	c := validClaims(*e.clock)
	for _, alg := range []string{"none", "HS256"} {
		h, _ := json.Marshal(map[string]any{"alg": alg, "typ": "JWT", "kid": testKid})
		cb, _ := json.Marshal(c)
		tok := b64(h) + "." + b64(cb) + "."
		if alg == "HS256" {
			tok += b64([]byte("sig"))
		}
		if _, err := e.verify(t, tok, "delete_ticket"); err == nil {
			t.Fatalf("expected rejection of alg %s", alg)
		}
	}
}

func TestVerify_ClaimFailures(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name   string
		mutate func(c map[string]any)
		tool   string
	}{
		{"wrong iss", func(c map[string]any) { c["iss"] = "evil" }, "delete_ticket"},
		{"wrong aud", func(c map[string]any) { c["aud"] = "other" }, "delete_ticket"},
		{"expired", func(c map[string]any) {
			c["iat"] = now.Add(-200 * time.Second).Unix()
			c["nbf"] = now.Add(-200 * time.Second).Unix()
			c["exp"] = now.Add(-100 * time.Second).Unix()
		}, "delete_ticket"},
		{"not yet valid", func(c map[string]any) { c["nbf"] = now.Add(120 * time.Second).Unix() }, "delete_ticket"},
		{"lifetime 600", func(c map[string]any) { c["exp"] = now.Add(600 * time.Second).Unix() }, "delete_ticket"},
		{"wrong tool", func(c map[string]any) { c["tool"] = "list_tickets" }, "delete_ticket"},
		{"missing scope", func(c map[string]any) { c["scope"] = "tickets:read tickets:write" }, "delete_ticket"},
		{"unknown tool", func(c map[string]any) { c["tool"] = "drop_db" }, "drop_db"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			c := validClaims(*e.clock)
			tc.mutate(c)
			if _, err := e.verify(t, signWith(t, e.key, rs256Header(testKid), c), tc.tool); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestVerify_ScopeMapping(t *testing.T) {
	e := newTestEnv(t)
	for tool, scope := range map[string]string{
		"list_tickets": "tickets:read", "update_ticket_status": "tickets:write", "delete_ticket": "tickets:delete",
	} {
		c := validClaims(*e.clock)
		c["tool"], c["scope"] = tool, scope
		if _, err := e.verify(t, signWith(t, e.key, rs256Header(testKid), c), tool); err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
	}
}

func TestVerify_ReplayedJTI(t *testing.T) {
	e := newTestEnv(t)
	tok := signWith(t, e.key, rs256Header(testKid), validClaims(*e.clock))
	if _, err := e.verify(t, tok, "delete_ticket"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.verify(t, tok, "delete_ticket"); err == nil {
		t.Fatal("expected replay rejection")
	}
}

func TestVerify_FailedVerificationDoesNotConsumeJTI(t *testing.T) {
	e := newTestEnv(t)
	c := validClaims(*e.clock)
	c["scope"] = "tickets:read"
	if _, err := e.verify(t, signWith(t, e.key, rs256Header(testKid), c), "delete_ticket"); err == nil {
		t.Fatal("expected scope failure")
	}
	c["scope"] = "tickets:delete"
	if _, err := e.verify(t, signWith(t, e.key, rs256Header(testKid), c), "delete_ticket"); err != nil {
		t.Fatalf("jti consumed by a failed verification: %v", err)
	}
}

func TestVerify_PrunesExpiredJTIs(t *testing.T) {
	e := newTestEnv(t)
	if _, err := e.verify(t, signWith(t, e.key, rs256Header(testKid), validClaims(*e.clock)), "delete_ticket"); err != nil {
		t.Fatal(err)
	}
	*e.clock = e.clock.Add(10 * time.Minute)
	c := validClaims(*e.clock)
	if _, err := e.verify(t, signWith(t, e.key, rs256Header(testKid), c), "delete_ticket"); err != nil {
		t.Fatal(err)
	}
	e.verifier.mu.Lock()
	n := len(e.verifier.seen)
	e.verifier.mu.Unlock()
	if n != 1 {
		t.Fatalf("expected expired jti pruned, have %d entries", n)
	}
}

func TestVerify_UnknownKidRefetchThrottled(t *testing.T) {
	e := newTestEnv(t)
	if _, err := e.verify(t, signWith(t, e.key, rs256Header(testKid), validClaims(*e.clock)), "delete_ticket"); err != nil {
		t.Fatal(err)
	}
	if h := atomic.LoadInt32(e.hits); h != 1 {
		t.Fatalf("expected 1 initial fetch, got %d", h)
	}
	// Unknown kid within 30s of the last fetch: no refetch.
	if _, err := e.verify(t, signWith(t, e.key, rs256Header("unknown"), validClaims(*e.clock)), "delete_ticket"); err == nil {
		t.Fatal("expected unknown kid failure")
	}
	if h := atomic.LoadInt32(e.hits); h != 1 {
		t.Fatalf("expected refetch to be throttled, hits=%d", h)
	}
	// After 30s: exactly one refetch.
	*e.clock = e.clock.Add(31 * time.Second)
	if _, err := e.verify(t, signWith(t, e.key, rs256Header("unknown"), validClaims(*e.clock)), "delete_ticket"); err == nil {
		t.Fatal("expected unknown kid failure")
	}
	if h := atomic.LoadInt32(e.hits); h != 2 {
		t.Fatalf("expected exactly one refetch, hits=%d", h)
	}
}

func TestVerify_UnknownKidRefetchPicksUpRotatedKey(t *testing.T) {
	key1, _ := rsa.GenerateKey(rand.Reader, 2048)
	key2, _ := rsa.GenerateKey(rand.Reader, 2048)
	var hits int32
	var rotated atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		keys := []map[string]string{jwkFor("k1", &key1.PublicKey)}
		if rotated.Load() {
			keys = append(keys, jwkFor("k2", &key2.PublicKey))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	}))
	defer srv.Close()
	v := NewVerifier(srv.URL, testIssuer, testAudience, srv.Client())
	clock := time.Now()
	v.now = func() time.Time { return clock }

	if _, err := v.Verify(context.Background(), "Bearer "+signWith(t, key1, rs256Header("k1"), validClaims(clock)), "delete_ticket"); err != nil {
		t.Fatal(err)
	}
	rotated.Store(true)
	clock = clock.Add(31 * time.Second)
	if _, err := v.Verify(context.Background(), "Bearer "+signWith(t, key2, rs256Header("k2"), validClaims(clock)), "delete_ticket"); err != nil {
		t.Fatalf("rotated key not picked up: %v", err)
	}
	if h := atomic.LoadInt32(&hits); h != 2 {
		t.Fatalf("expected exactly 2 fetches, got %d", h)
	}
}

func TestVerify_JWKSFetchFailureFailsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	v := NewVerifier(srv.URL, testIssuer, testAudience, srv.Client())
	tok := signWith(t, key, rs256Header(testKid), validClaims(time.Now()))
	if _, err := v.Verify(context.Background(), "Bearer "+tok, "delete_ticket"); err == nil {
		t.Fatal("expected failure when JWKS unavailable")
	}
}
