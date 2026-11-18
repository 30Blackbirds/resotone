package auth

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/google/uuid"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	tenantOTelAttr  = "tenant.id"
	subjectOTelAttr = "enduser.id"
	// devTenantHeader lets local dev pass a tenant without a real JWT. It is
	// ONLY honored when no verifier is configured (dev/test) — app.Run makes
	// production boot-fail unless a verifier is wired, so this header is never
	// trusted in production (anti-spoof).
	devTenantHeader = "X-Dev-Tenant"
)

// bearerRe matches "Bearer <token>" case-insensitively with surrounding space.
var bearerRe = regexp.MustCompile(`(?i)^bearer\s+(\S+)\s*$`)

// Middleware returns an HTTP middleware that authenticates the request and puts
// the verified Actor on the context.
//
//   - verifier != nil: an Authorization: Bearer <jwt> is required; the token is
//     verified against the JWKS and the Actor (sub + org-claim tenant) is set.
//     Missing/invalid tokens → 401.
//   - verifier == nil (dev/test only): the request passes through; if an
//     X-Dev-Tenant header is present, a dev Actor is set from it so local work
//     can exercise tenant scoping without minting JWTs.
//
// OTel spans get enduser.id + tenant.id attributes (per-span, never on the
// resource), so traces cannot leak a tenant list.
func Middleware(verifier Verifier, logger *slog.Logger) func(http.Handler) http.Handler {
	return NewMiddleware(verifier, Options{Logger: logger})
}

// Options configures NewMiddleware.
type Options struct {
	Logger *slog.Logger
	// Users is Resotone's own user store. With a verifier AND a store, every
	// request resolves the user (created at first login), refuses a disabled one
	// and takes the role from the local assignment (see ResolveRole). Without a
	// store the actor keeps the role the token claim gave it — that is for tests
	// of the verifier only; app.Run always passes a store.
	Users UserStore
	// ClaimGrants lets a role claim that is higher than the local assignment, or
	// that has no local assignment at all, raise the role. Off by default.
	ClaimGrants bool
}

// NewMiddleware is Middleware with the user store and the claim policy.
func NewMiddleware(verifier Verifier, opts Options) func(http.Handler) http.Handler {
	logger := opts.Logger
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if verifier == nil {
				// Dev/test: optional X-Dev-Tenant header.
				// The dev actor is admin: dev auth is already the explicit,
				// never-in-production opt-in to "no checks", and a dev
				// environment that could not reach settings would only teach
				// people to work around the role table.
				if tenant := r.Header.Get(devTenantHeader); tenant != "" {
					dev := Actor{Sub: "dev-user", Tenant: tenant, Role: RoleAdmin}
					ctx := WithActor(r.Context(), dev)
					setSpanActor(ctx, dev)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			m := bearerRe.FindStringSubmatch(r.Header.Get("Authorization"))
			if m == nil {
				unauthorized(w, "missing bearer token")
				return
			}
			actor, err := verifier.Verify(r.Context(), m[1])
			if err != nil {
				if logger != nil {
					logger.Warn("jwt_verify_failed", "err", err)
				}
				unauthorized(w, "invalid token")
				return
			}
			if opts.Users != nil {
				var ok bool
				if actor, ok = resolveUser(w, r, opts, actor); !ok {
					return
				}
			}
			ctx := WithActor(r.Context(), actor)
			setSpanActor(ctx, actor)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// resolveUser finds or creates the user record, refuses a disabled user and
// sets the effective role. It writes the response and returns false on refusal.
//
// Identity comes from ONE place: the Actor the verifier built from the verified
// ACCESS token (issuer, sub, and the optional name and email claims). Nothing the
// caller adds to the request, and nothing the editor's server forwards from an ID
// token, is read here: a header, query parameter or body naming another subject
// changes nothing, because the store is only ever asked about this Actor.
func resolveUser(w http.ResponseWriter, r *http.Request, opts Options, actor Actor) (Actor, bool) {
	claim := actor.Role
	actor.Role = RoleNone // nothing is granted until the rules below say so
	tenant, err := uuid.Parse(actor.Tenant)
	if err != nil {
		// No usable tenant: there is nowhere to look up a user or an assignment.
		// The handlers already answer 403 for a tenantless actor.
		return actor, true
	}
	rec, err := opts.Users.Login(r.Context(), Identity{
		Tenant: tenant, Issuer: actor.Issuer, Sub: actor.Sub,
		DisplayName: actor.DisplayName, Email: actor.Email,
	})
	if err != nil {
		if opts.Logger != nil {
			opts.Logger.Error("user_store_failed", "err", err)
		}
		writeJSONError(w, http.StatusServiceUnavailable, "user store unavailable")
		return actor, false
	}
	if rec.Disabled {
		if opts.Logger != nil {
			opts.Logger.Warn("login_refused_disabled", "tenant", actor.Tenant, "user", rec.ID.String())
		}
		writeJSONError(w, http.StatusForbidden, "account disabled")
		return actor, false
	}
	actor.UserID = rec.ID
	actor.Role = ResolveRole(rec.Role, claim, opts.ClaimGrants)
	return actor, true
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}

func setSpanActor(ctx context.Context, actor Actor) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.SetAttributes(attribute.String(subjectOTelAttr, actor.Sub))
	if actor.HasTenant() {
		span.SetAttributes(attribute.String(tenantOTelAttr, actor.Tenant))
	}
}

func unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}
