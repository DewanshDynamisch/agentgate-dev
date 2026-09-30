package credential

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJWKSHandler(t *testing.T) {
	body := []byte(`{"keys":[]}`)
	h := JWKSHandler(body)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil))
	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/jwk-set+json" {
		t.Errorf("content-type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=300" {
		t.Errorf("cache-control = %q", cc)
	}
	if rec.Body.String() != string(body) {
		t.Errorf("body = %q", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/.well-known/jwks.json", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST code = %d", rec.Code)
	}
}
