package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestResolveRolePrecedence(t *testing.T) {
	cases := []struct {
		name        string
		local       Role
		claim       Role
		claimGrants bool
		want        Role
	}{
		{"new user, claim ignored", RoleNone, RoleAdmin, false, RoleNone},
		{"higher claim ignored", RoleEditor, RoleAdmin, false, RoleEditor},
		{"lower claim ignored", RoleEditor, RoleViewer, false, RoleEditor},
		{"no claim", RoleEditor, RoleNone, false, RoleEditor},
		{"opt-in: higher claim raises", RoleEditor, RoleAdmin, true, RoleAdmin},
		{"opt-in: local wins when higher", RoleAdmin, RoleViewer, true, RoleAdmin},
		{"opt-in: claim grants with no local role", RoleNone, RoleEditor, true, RoleEditor},
		{"opt-in: nothing anywhere", RoleNone, RoleNone, true, RoleNone},
	}
	for _, c := range cases {
		if got := ResolveRole(c.local, c.claim, c.claimGrants); got != c.want {
			t.Errorf("%s: ResolveRole(%v, %v, %v) = %v, want %v", c.name, c.local, c.claim, c.claimGrants, got, c.want)
		}
	}
}

// fakeUsers records what Login saw and answers with a fixed record.
type fakeUsers struct {
	rec   UserRecord
	err   error
	calls []Identity
}

func (f *fakeUsers) Login(_ context.Context, id Identity) (UserRecord, error) {
	f.calls = append(f.calls, id)
	return f.rec, f.err
}

const tenantUUID = "6f1c2a9e-2b0d-4c53-9d0a-0d7a1f0a9b11"

// run sends one request with a valid token whose role claim is claimRole through
// the middleware and returns the status and the actor the handler saw.
func run(t *testing.T, users UserStore, claimGrants bool, claimRole string) (int, Actor, bool) {
	t.Helper()
	priv, set := testKeys(t)
	v := verifierWith(t, set, VerifierConfig{})
	claims := map[string]any{DefaultTenantClaim: tenantUUID, "name": "Ada", "email": "ada@example.org"}
	if claimRole != "" {
		claims[DefaultRoleClaim] = claimRole
	}
	var seen Actor
	var ok bool
	h := NewMiddleware(v, Options{Users: users, ClaimGrants: claimGrants})(capture(&seen, &ok))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+mint(t, priv, claims))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, seen, ok
}

func TestLoginNewUserHasNoRoleEvenWithAdminClaim(t *testing.T) {
	store := &fakeUsers{rec: UserRecord{ID: uuid.New()}}
	code, actor, ok := run(t, store, false, "admin")
	if code != http.StatusNoContent || !ok {
		t.Fatalf("status %d ok=%v", code, ok)
	}
	if actor.Role != RoleNone {
		t.Fatalf("new user got role %v from the claim; want none", actor.Role)
	}
	// A route that needs any role refuses it with 403: today's behavior.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(WithActor(context.Background(), actor))
	RequireRole(RoleViewer, func(http.ResponseWriter, *http.Request) { t.Error("handler ran for a user without a role") })(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("RequireRole status = %d, want 403", rec.Code)
	}
	// The identity reached the store as the token stated it.
	got := store.calls[0]
	if got.Tenant.String() != tenantUUID || got.Issuer != testIssuer || got.Sub != "user-1" || got.DisplayName != "Ada" || got.Email != "ada@example.org" {
		t.Fatalf("Login saw %+v", got)
	}
}

func TestLoginDisabledUserIs403AndNeverReachesHandler(t *testing.T) {
	// Even holding the highest local role and the opt-in claim.
	store := &fakeUsers{rec: UserRecord{ID: uuid.New(), Disabled: true, Role: RoleAdmin}}
	code, _, ok := run(t, store, true, "admin")
	if code != http.StatusForbidden || ok {
		t.Fatalf("disabled user: status %d, handler reached = %v; want 403 and not reached", code, ok)
	}
}

func TestLoginLocalRoleWinsAndClaimNeedsOptIn(t *testing.T) {
	store := &fakeUsers{rec: UserRecord{ID: uuid.New(), Role: RoleEditor}}
	if _, a, _ := run(t, store, false, "admin"); a.Role != RoleEditor {
		t.Errorf("without opt-in a higher claim changed the role to %v", a.Role)
	}
	if _, a, _ := run(t, store, true, "admin"); a.Role != RoleAdmin {
		t.Errorf("with opt-in a higher claim gave %v, want admin (positive control)", a.Role)
	}
	if _, a, _ := run(t, store, true, "viewer"); a.Role != RoleEditor {
		t.Errorf("with opt-in a lower claim gave %v, want editor", a.Role)
	}
	if _, a, _ := run(t, store, false, ""); a.UserID != store.rec.ID {
		t.Errorf("actor.UserID = %v, want the stored id", a.UserID)
	}
}

func TestLoginStoreFailureFailsClosed(t *testing.T) {
	store := &fakeUsers{err: errors.New("db down"), rec: UserRecord{Role: RoleAdmin}}
	code, _, ok := run(t, store, true, "admin")
	if code != http.StatusServiceUnavailable || ok {
		t.Fatalf("store failure: status %d, handler reached = %v; want 503 and not reached", code, ok)
	}
}

func TestLoginWithoutUsableTenantGetsNoRole(t *testing.T) {
	priv, set := testKeys(t)
	v := verifierWith(t, set, VerifierConfig{})
	store := &fakeUsers{rec: UserRecord{Role: RoleAdmin}}
	var seen Actor
	var ok bool
	h := NewMiddleware(v, Options{Users: store, ClaimGrants: true})(capture(&seen, &ok))
	for name, org := range map[string]any{"missing": nil, "not a uuid": "org-9"} {
		claims := map[string]any{DefaultRoleClaim: "admin"}
		if org != nil {
			claims[DefaultTenantClaim] = org
		}
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Authorization", "Bearer "+mint(t, priv, claims))
		h.ServeHTTP(httptest.NewRecorder(), req)
		if !ok || seen.Role != RoleNone {
			t.Errorf("%s tenant: role %v, reached %v; want none", name, seen.Role, ok)
		}
	}
	if len(store.calls) != 0 {
		t.Errorf("store consulted %d times without a tenant", len(store.calls))
	}
}

func TestVerifierProfileClaimsAreBounded(t *testing.T) {
	priv, set := testKeys(t)
	v := verifierWith(t, set, VerifierConfig{})
	long := make([]rune, 1000)
	for i := range long {
		long[i] = 'å'
	}
	a, err := v.Verify(context.Background(), mint(t, priv, map[string]any{"preferred_username": string(long)}))
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(a.DisplayName)); n != maxProfileRunes {
		t.Errorf("display name kept %d runes, want %d", n, maxProfileRunes)
	}
	if a.Issuer != testIssuer {
		t.Errorf("issuer = %q", a.Issuer)
	}
}
