// Package auth verifies JWTs from any OpenID Connect provider locally against
// a cached JWKS and derives the request Actor (subject, tenant, role).
// Tokens are verified locally against a cached JWKS, so a briefly unreachable
// provider does not take the API down; the tenant is a token claim; claim names
// and roles are provider-neutral (ADR 0001). The effective role then comes from
// Resotone's own user records (users.go, middleware.go).
package auth

import (
	"context"

	"github.com/google/uuid"
)

// Actor is the verified sender of a request.
//
// Derived from the JWT: Sub = the provider's user id, Tenant = the tenant claim
// (or the fixed tenant in single-tenant mode), Role = the role claim mapped to
// one of the four built-in roles. Handlers use Tenant to scope every content
// query — a consumer cannot spoof another tenant by setting a tenant in the
// request body/header, because it is only ever taken from the verified token or
// the server's own configuration. Tenant is "" when the claim is missing/empty;
// Role is RoleNone when the role claim is missing or unrecognized.
type Actor struct {
	Sub    string
	Tenant string
	Role   Role
	// Issuer is the provider the token came from (the verifier's configured
	// issuer, which the token was validated against). With Sub it identifies the
	// user. Empty for dev and service actors.
	Issuer string
	// DisplayName and Email are the optional profile claims (name or
	// preferred_username, and email). Informational only: never used to make an
	// access decision.
	DisplayName string
	Email       string
	// UserID is Resotone's own record of this user, set once the login resolver
	// has found or created it. The zero value means no record (dev, service).
	UserID uuid.UUID
}

// HasTenant reports whether a non-empty tenant was present on the JWT.
func (a Actor) HasTenant() bool { return a.Tenant != "" }

type actorContextKey struct{}

// WithActor returns a context carrying the verified Actor. Set by the JWT
// middleware, read by the HTTP handlers.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorContextKey{}, a)
}

// ActorFromContext returns the Actor and whether one was present. When no
// middleware ran (dev without JWT configured), ok is false.
func ActorFromContext(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorContextKey{}).(Actor)
	return a, ok
}
