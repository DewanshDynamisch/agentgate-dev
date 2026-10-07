package oidcverify

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
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
	jwksRefreshMin  = 30 * time.Second
	httpTimeout     = 5 * time.Second
	maxResponseSize = 1 << 20 // 1 MiB cap on discovery and JWKS bodies
	minRSABits      = 2048
	maxJWKSKeys     = 64
)

// keySet caches the IdP's verification keys. The mutex is never held across a
// network call.
type keySet struct {
	jwksURL string
	client  *http.Client
	now     func() time.Time

	mu          sync.Mutex
	keys        map[string]any // kid -> *rsa.PublicKey | *ecdsa.PublicKey
	lastAttempt time.Time
	lastErr     error
}

func newKeySet(jwksURL string, client *http.Client, now func() time.Time) *keySet {
	return &keySet{jwksURL: jwksURL, client: client, now: now}
}

// get returns the key for kid. An unknown kid triggers at most one JWKS
// refetch per jwksRefreshMin. ErrUnavailable is returned when the key cannot
// be had because the IdP could not be reached.
func (ks *keySet) get(ctx context.Context, kid string) (any, error) {
	ks.mu.Lock()
	if k, ok := ks.keys[kid]; ok {
		ks.mu.Unlock()
		return k, nil
	}
	now := ks.now()
	if !ks.lastAttempt.IsZero() && now.Sub(ks.lastAttempt) < jwksRefreshMin {
		lastErr := ks.lastErr
		ks.mu.Unlock()
		if lastErr != nil {
			return nil, ErrUnavailable
		}
		return nil, ErrInvalidToken
	}
	ks.lastAttempt = now // claim the refresh slot before releasing the lock
	ks.mu.Unlock()

	keys, err := ks.fetch(ctx)

	ks.mu.Lock()
	defer ks.mu.Unlock()
	ks.lastErr = err
	if err != nil {
		return nil, ErrUnavailable
	}
	ks.keys = keys
	if k, ok := keys[kid]; ok {
		return k, nil
	}
	return nil, ErrInvalidToken
}

// refresh performs the mandatory startup fetch.
func (ks *keySet) refresh(ctx context.Context) error {
	keys, err := ks.fetch(ctx)
	ks.mu.Lock()
	defer ks.mu.Unlock()
	ks.lastAttempt = ks.now()
	ks.lastErr = err
	if err != nil {
		return err
	}
	ks.keys = keys
	return nil
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func (ks *keySet) fetch(ctx context.Context) (map[string]any, error) {
	body, err := getBounded(ctx, ks.client, ks.jwksURL)
	if err != nil {
		return nil, fmt.Errorf("oidcverify: fetch jwks: %w", err)
	}
	var doc struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, errors.New("oidcverify: malformed jwks")
	}
	if len(doc.Keys) > maxJWKSKeys {
		return nil, errors.New("oidcverify: jwks has too many keys")
	}
	out := make(map[string]any, len(doc.Keys))
	for _, j := range doc.Keys {
		if j.Kid == "" || (j.Use != "" && j.Use != "sig") {
			continue
		}
		if k := j.publicKey(); k != nil {
			out[j.Kid] = k
		}
	}
	return out, nil
}

// publicKey converts a JWK to a verification key, or nil when the key is
// unsupported or weak.
func (j jwk) publicKey() any {
	switch j.Kty {
	case "RSA":
		if j.Alg != "" && j.Alg != "RS256" {
			return nil
		}
		nb, err1 := b64.DecodeString(j.N)
		eb, err2 := b64.DecodeString(j.E)
		if err1 != nil || err2 != nil || len(nb) == 0 || len(eb) == 0 || len(eb) > 4 {
			return nil
		}
		n := new(big.Int).SetBytes(nb)
		e := new(big.Int).SetBytes(eb)
		if n.BitLen() < minRSABits || !e.IsInt64() || e.Int64() < 3 || e.Int64()%2 == 0 {
			return nil
		}
		return &rsa.PublicKey{N: n, E: int(e.Int64())}
	case "EC":
		if (j.Alg != "" && j.Alg != "ES256") || j.Crv != "P-256" {
			return nil
		}
		xb, err1 := b64.DecodeString(j.X)
		yb, err2 := b64.DecodeString(j.Y)
		if err1 != nil || err2 != nil || len(xb) != 32 || len(yb) != 32 {
			return nil
		}
		raw := append([]byte{0x04}, append(xb, yb...)...)
		k, err := ecdsa.ParseUncompressedPublicKey(ellipticP256(), raw)
		if err != nil {
			return nil
		}
		return k
	}
	return nil
}

// discoverJWKSURL reads the issuer's OIDC discovery document.
func discoverJWKSURL(ctx context.Context, client *http.Client, issuer string) (string, error) {
	url := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	body, err := getBounded(ctx, client, url)
	if err != nil {
		return "", fmt.Errorf("oidcverify: discovery: %w", err)
	}
	var doc struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", errors.New("oidcverify: malformed discovery document")
	}
	if doc.Issuer != issuer {
		return "", errors.New("oidcverify: discovery issuer mismatch")
	}
	if doc.JWKSURI == "" {
		return "", errors.New("oidcverify: discovery document has no jwks_uri")
	}
	return doc.JWKSURI, nil
}

func getBounded(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseSize {
		return nil, errors.New("response too large")
	}
	return body, nil
}
