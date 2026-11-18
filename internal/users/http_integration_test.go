//go:build integration

package users_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/30blackbirds/resotone/internal/auth"
	"github.com/30blackbirds/resotone/internal/httpapi"
	"github.com/30blackbirds/resotone/internal/persistence"
	"github.com/30blackbirds/resotone/internal/testpg"
	"github.com/30blackbirds/resotone/internal/users"
)

type actorVerifier struct{ actor auth.Actor }

func (v actorVerifier) Verify(context.Context, string) (auth.Actor, error) { return v.actor, nil }

// The whole path: HTTP, role gate, handler, store, row level security. A
// tenant-A admin asks for tenant B's user by id and gets exactly what a
// nonexistent id gets.
func TestAdminAPIIsTenantIsolatedEndToEnd(t *testing.T) {
	ctx := context.Background()
	pool, dsn := testpg.PoolDSN(t)
	if _, err := persistence.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := users.NewPostgresStore(testpg.AppPool(t, pool, dsn, "users_http_app"))
	a, b := uuid.New(), uuid.New()
	adminA, err := store.GrantRole(ctx, a, "https://idp.example", "alice", auth.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	bUser, err := store.GrantRole(ctx, b, "https://idp.example", "bob", auth.RoleEditor)
	if err != nil {
		t.Fatal(err)
	}

	call := func(role auth.Role, method, path, body string) (int, string) {
		h := httpapi.NewRouter(httpapi.Deps{
			UserAdmin: store,
			Verifier:  actorVerifier{auth.Actor{Sub: "alice", Tenant: a.String(), Role: role}},
		})
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer x")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}

	code, body := call(auth.RoleAdmin, "GET", "/api/v1/users", "")
	if code != 200 || !strings.Contains(body, "alice") || strings.Contains(body, "bob") {
		t.Fatalf("tenant A's list = %d %s; want alice and no bob", code, body)
	}
	// An id that exists only in tenant B answers like an id that exists nowhere.
	ghost := uuid.NewString()
	for _, c := range []struct{ method, suffix, body string }{
		{"GET", "", ""},
		{"GET", "/export", ""},
		{"PUT", "/role", `{"role":"admin"}`},
		{"POST", "/disable", ""},
		{"POST", "/enable", ""},
		{"DELETE", "", ""},
	} {
		wantCode, wantBody := call(auth.RoleAdmin, c.method, "/api/v1/users/"+ghost+c.suffix, c.body)
		gotCode, gotBody := call(auth.RoleAdmin, c.method, "/api/v1/users/"+bUser.UserID.String()+c.suffix, c.body)
		if gotCode != 404 || gotCode != wantCode || gotBody != wantBody {
			t.Errorf("%s %s on tenant B's user = %d %s; want the same 404 as a nonexistent id (%d %s)",
				c.method, c.suffix, gotCode, gotBody, wantCode, wantBody)
		}
	}
	if u, _ := store.Get(ctx, b, bUser.UserID); u.Role != "editor" || u.Status != "active" || u.ErasedAt != nil {
		t.Errorf("tenant B's user was changed through tenant A's API: %+v", u)
	}
	// Positive control: the same verbs work on tenant A's own user.
	if code, body := call(auth.RoleAdmin, "POST", "/api/v1/users/"+adminA.UserID.String()+"/disable", ""); code != 409 {
		t.Errorf("disabling tenant A's only admin = %d %s; want 409 last-admin", code, body)
	}
	if code, _ := call(auth.RoleEditor, "GET", "/api/v1/users", ""); code != http.StatusForbidden {
		t.Errorf("an editor listing users = %d, want 403", code)
	}
}
