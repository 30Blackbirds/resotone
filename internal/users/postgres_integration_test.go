//go:build integration

package users

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/30blackbirds/resotone/internal/auth"
	"github.com/30blackbirds/resotone/internal/persistence"
	"github.com/30blackbirds/resotone/internal/testpg"
)

// stores returns the store on the app role (RLS applies) and the migrator pool
// for setting up and inspecting rows the app role may not see.
func stores(t *testing.T) (*PostgresStore, *pgxpool.Pool) {
	t.Helper()
	pool, dsn := testpg.PoolDSN(t)
	if _, err := persistence.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewPostgresStore(testpg.AppPool(t, pool, dsn, "users_store_app")), pool
}

func ident(tenant uuid.UUID, sub string) auth.Identity {
	return auth.Identity{Tenant: tenant, Issuer: "https://idp.example", Sub: sub, DisplayName: "Ada", Email: "ada@example.org"}
}

func TestLoginCreatesUserWithoutRoleAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s, admin := stores(t)
	tenant := uuid.New()

	first, err := s.Login(ctx, ident(tenant, "ada"))
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	if first.Role != auth.RoleNone || first.Disabled || first.ID == uuid.Nil {
		t.Fatalf("first login = %+v; want an active user with no role", first)
	}
	second, err := s.Login(ctx, ident(tenant, "ada"))
	if err != nil || second.ID != first.ID {
		t.Fatalf("second login = %+v, %v; want the same user %s", second, err, first.ID)
	}
	var n int
	_ = admin.QueryRow(ctx, `select count(*) from resonans_app.users`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d user rows after two logins, want 1", n)
	}
	_ = admin.QueryRow(ctx, `select count(*) from resonans_app.user_roles`).Scan(&n)
	if n != 0 {
		t.Fatalf("login created %d role rows; it must never assign one", n)
	}
	var name, email string
	_ = admin.QueryRow(ctx, `select display_name, email from resonans_app.users`).Scan(&name, &email)
	if name != "Ada" || email != "ada@example.org" {
		t.Fatalf("stored profile %q %q", name, email)
	}
}

func TestLoginReadsAssignedRoleAndDisabledStatus(t *testing.T) {
	ctx := context.Background()
	s, admin := stores(t)
	tenant := uuid.New()
	rec, err := s.Login(ctx, ident(tenant, "ada"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `insert into resonans_app.user_roles (tenant_id, user_id, role) values ($1, $2, 'publisher')`, tenant, rec.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Login(ctx, ident(tenant, "ada")); got.Role != auth.RolePublisher || got.Disabled {
		t.Fatalf("after assignment = %+v; want publisher, active", got)
	}
	if _, err := admin.Exec(ctx, `update resonans_app.users set status = 'disabled' where id = $1`, rec.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Login(ctx, ident(tenant, "ada")); !got.Disabled {
		t.Fatalf("after disabling = %+v; want disabled", got)
	}
}

// A disabled user's login must not rewrite their record.
func TestLoginDoesNotTouchDisabledUser(t *testing.T) {
	ctx := context.Background()
	s, admin := stores(t)
	tenant := uuid.New()
	rec, _ := s.Login(ctx, ident(tenant, "ada"))
	if _, err := admin.Exec(ctx, `update resonans_app.users set status = 'disabled', last_login_at = now() - interval '2 days' where id = $1`, rec.ID); err != nil {
		t.Fatal(err)
	}
	changed := ident(tenant, "ada")
	changed.DisplayName = "Someone Else"
	if _, err := s.Login(ctx, changed); err != nil {
		t.Fatal(err)
	}
	var name string
	var stale bool
	_ = admin.QueryRow(ctx, `select display_name, last_login_at < now() - interval '1 day' from resonans_app.users where id = $1`, rec.ID).Scan(&name, &stale)
	if name != "Ada" || !stale {
		t.Fatalf("disabled user was updated: name=%q stale=%v", name, stale)
	}
}

// The same person in two tenants is two users; neither login sees the other.
func TestLoginKeepsTenantsApart(t *testing.T) {
	ctx := context.Background()
	s, admin := stores(t)
	a, b := uuid.New(), uuid.New()
	ra, _ := s.Login(ctx, ident(a, "ada"))
	if _, err := admin.Exec(ctx, `insert into resonans_app.user_roles (tenant_id, user_id, role) values ($1, $2, 'admin')`, a, ra.ID); err != nil {
		t.Fatal(err)
	}
	rb, err := s.Login(ctx, ident(b, "ada"))
	if err != nil {
		t.Fatal(err)
	}
	if rb.ID == ra.ID || rb.Role != auth.RoleNone {
		t.Fatalf("tenant B login = %+v; must be a new user without tenant A's admin role", rb)
	}
	if got, _ := s.Login(ctx, ident(a, "ada")); got.Role != auth.RoleAdmin { // positive control
		t.Fatalf("tenant A lost its assignment: %+v", got)
	}
}

func TestConcurrentFirstLoginsCreateOneUser(t *testing.T) {
	ctx := context.Background()
	s, admin := stores(t)
	tenant := uuid.New()
	var wg sync.WaitGroup
	ids := make([]uuid.UUID, 8)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec, err := s.Login(ctx, ident(tenant, "ada"))
			if err != nil {
				t.Errorf("login %d: %v", i, err)
			}
			ids[i] = rec.ID
		}()
	}
	wg.Wait()
	var n int
	_ = admin.QueryRow(ctx, `select count(*) from resonans_app.users`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d rows after concurrent first logins, want 1", n)
	}
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("logins disagree on the user id: %v", ids)
		}
	}
}

func TestGrantRoleCreatesTheUserAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s, admin := stores(t)
	tenant := uuid.New()

	// Nobody has logged in yet; the operator grants the first admin.
	first, err := s.GrantRole(ctx, tenant, "https://idp.example", "ada", auth.RoleAdmin)
	if err != nil || !first.UserCreated || !first.RoleChanged || first.Disabled {
		t.Fatalf("first grant = %+v, %v; want a created user and a changed role", first, err)
	}
	// The same command again changes nothing.
	again, err := s.GrantRole(ctx, tenant, "https://idp.example", "ada", auth.RoleAdmin)
	if err != nil || again.UserCreated || again.RoleChanged || again.UserID != first.UserID {
		t.Fatalf("second grant = %+v, %v; want a no-op on the same user", again, err)
	}
	var users, roles int
	_ = admin.QueryRow(ctx, `select count(*) from resonans_app.users`).Scan(&users)
	_ = admin.QueryRow(ctx, `select count(*) from resonans_app.user_roles`).Scan(&roles)
	if users != 1 || roles != 1 {
		t.Fatalf("%d users, %d roles after two grants; want 1 and 1", users, roles)
	}

	// The login path now resolves that user as admin, and fills in the profile.
	rec, err := s.Login(ctx, ident(tenant, "ada"))
	if err != nil || rec.ID != first.UserID || rec.Role != auth.RoleAdmin {
		t.Fatalf("login after grant = %+v, %v; want the same user as admin", rec, err)
	}
	var name string
	_ = admin.QueryRow(ctx, `select display_name from resonans_app.users`).Scan(&name)
	if name != "Ada" {
		t.Errorf("display name after the first login = %q, want Ada", name)
	}
}

// A grant is for one tenant and one identity only.
func TestGrantRoleStaysInItsTenantAndIdentity(t *testing.T) {
	ctx := context.Background()
	s, _ := stores(t)
	a, b := uuid.New(), uuid.New()
	if _, err := s.GrantRole(ctx, a, "https://idp.example", "ada", auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if rec, _ := s.Login(ctx, ident(b, "ada")); rec.Role != auth.RoleNone {
		t.Errorf("tenant B login of the same person = %v; the grant leaked across tenants", rec.Role)
	}
	other := ident(a, "ada")
	other.Issuer = "https://other-idp.example"
	if rec, _ := s.Login(ctx, other); rec.Role != auth.RoleNone {
		t.Errorf("same sub at another issuer = %v; the grant leaked across issuers", rec.Role)
	}
	if rec, _ := s.Login(ctx, ident(a, "ada")); rec.Role != auth.RoleAdmin { // positive control
		t.Errorf("the granted user lost the role: %v", rec.Role)
	}
}

func TestGrantRoleKeepsADisabledUserDisabled(t *testing.T) {
	ctx := context.Background()
	s, admin := stores(t)
	tenant := uuid.New()
	rec, _ := s.Login(ctx, ident(tenant, "ada"))
	if _, err := admin.Exec(ctx, `update resonans_app.users set status = 'disabled' where id = $1`, rec.ID); err != nil {
		t.Fatal(err)
	}
	res, err := s.GrantRole(ctx, tenant, "https://idp.example", "ada", auth.RoleAdmin)
	if err != nil || !res.Disabled || !res.RoleChanged {
		t.Fatalf("grant on a disabled user = %+v, %v; want recorded and reported disabled", res, err)
	}
	if got, _ := s.Login(ctx, ident(tenant, "ada")); !got.Disabled {
		t.Errorf("the grant re-enabled a disabled user")
	}
}

func TestGrantRoleRefusesWhatItCannotGrant(t *testing.T) {
	ctx := context.Background()
	s, _ := stores(t)
	tenant := uuid.New()
	for name, c := range map[string]struct {
		tenant      uuid.UUID
		issuer, sub string
		role        auth.Role
	}{
		"no sub":    {tenant, "https://idp.example", " ", auth.RoleAdmin},
		"no issuer": {tenant, "", "ada", auth.RoleAdmin},
		"no tenant": {uuid.Nil, "https://idp.example", "ada", auth.RoleAdmin},
		"no role":   {tenant, "https://idp.example", "ada", auth.RoleNone},
		"bogus":     {tenant, "https://idp.example", "ada", auth.Role(99)},
	} {
		if _, err := s.GrantRole(ctx, c.tenant, c.issuer, c.sub, c.role); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
