package govapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/Dynamisch-LLC/agentgate/internal/auditevents"
	"github.com/Dynamisch-LLC/agentgate/internal/oidcverify"
)

// Authentication outcomes returned by an AdminAuthenticator.
var (
	ErrUnauthorized    = errors.New("govapi: unauthorized")
	ErrForbidden       = errors.New("govapi: forbidden")
	ErrAuthUnavailable = errors.New("govapi: authentication backend unavailable")
)

// AdminPrincipal is the authenticated administrator behind a request.
type AdminPrincipal struct {
	Subject  string
	Username string
	Roles    []string
	Method   string // "oidc" | "static"
}

// ActorName is the identity recorded on mutations: the username, else the subject.
func (p AdminPrincipal) ActorName() string {
	if p.Username != "" {
		return p.Username
	}
	return p.Subject
}

// AdminAuthenticator authenticates an admin API request. It returns
// ErrUnauthorized, ErrForbidden or ErrAuthUnavailable on failure.
type AdminAuthenticator interface {
	Authenticate(r *http.Request) (AdminPrincipal, error)
}

// TokenVerifier verifies a bearer access token (satisfied by *oidcverify.Verifier).
type TokenVerifier interface {
	Verify(ctx context.Context, rawToken string) (oidcverify.Principal, error)
}

type principalCtxKey struct{}

// PrincipalFromContext returns the principal stored by AdminAuthMiddlewareWith.
func PrincipalFromContext(ctx context.Context) (AdminPrincipal, bool) {
	p, ok := ctx.Value(principalCtxKey{}).(AdminPrincipal)
	return p, ok
}

type staticAuthenticator struct{ token string }

// NewStaticTokenAuthenticator authenticates requests presenting a shared admin token.
func NewStaticTokenAuthenticator(token string) AdminAuthenticator {
	return staticAuthenticator{token: token}
}

func (a staticAuthenticator) Authenticate(r *http.Request) (AdminPrincipal, error) {
	token := extractToken(r)
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(a.token)) != 1 {
		return AdminPrincipal{}, ErrUnauthorized
	}
	return AdminPrincipal{Subject: "static-admin", Username: "admin", Method: "static"}, nil
}

type oidcAuthenticator struct {
	v    TokenVerifier
	role string
}

// NewOIDCAuthenticator verifies Bearer access tokens and requires requiredRole.
// It never falls back to any other credential.
func NewOIDCAuthenticator(v TokenVerifier, requiredRole string) AdminAuthenticator {
	return oidcAuthenticator{v: v, role: requiredRole}
}

func (a oidcAuthenticator) Authenticate(r *http.Request) (AdminPrincipal, error) {
	parts := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") || strings.TrimSpace(parts[1]) == "" {
		return AdminPrincipal{}, ErrUnauthorized
	}
	p, err := a.v.Verify(r.Context(), strings.TrimSpace(parts[1]))
	if err != nil {
		if errors.Is(err, oidcverify.ErrUnavailable) {
			return AdminPrincipal{}, ErrAuthUnavailable
		}
		return AdminPrincipal{}, ErrUnauthorized
	}
	if a.role == "" || !slices.Contains(p.Roles, a.role) {
		return AdminPrincipal{}, ErrForbidden
	}
	return AdminPrincipal{Subject: p.Subject, Username: p.Username, Roles: p.Roles, Method: "oidc"}, nil
}

// AdminAuthMiddleware validates a static admin token (see NewStaticTokenAuthenticator).
func AdminAuthMiddleware(adminToken string, next http.HandlerFunc) http.HandlerFunc {
	return AdminAuthMiddlewareWith(NewStaticTokenAuthenticator(adminToken), next)
}

// AdminAuthMiddlewareWith authenticates via auth and stores the principal (and
// its actor name for audit attribution) in the request context.
func AdminAuthMiddlewareWith(auth AdminAuthenticator, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := auth.Authenticate(r)
		if err != nil {
			switch {
			case errors.Is(err, ErrForbidden):
				writeError(w, http.StatusForbidden, "FORBIDDEN", "insufficient privileges")
			case errors.Is(err, ErrAuthUnavailable):
				writeError(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "authentication service unavailable")
			default:
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "valid admin credentials required")
			}
			return
		}
		ctx := context.WithValue(r.Context(), principalCtxKey{}, p)
		ctx = auditevents.WithActor(ctx, p.ActorName())
		next(w, r.WithContext(ctx))
	}
}

func extractToken(r *http.Request) string {
	if header := r.Header.Get("X-AgentGate-Admin-Key"); header != "" {
		return strings.TrimSpace(header)
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
		return strings.TrimSpace(parts[1])
	}

	return ""
}
