//go:build integration

package persistence

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/30blackbirds/resotone/internal/testpg"
)

// asTenant runs fn in a transaction whose tenant GUC is set, as the store does.
func asTenant(ctx context.Context, t *testing.T, pool *pgxpool.Pool, tenant uuid.UUID, fn func(pgx.Tx) error) error {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "select set_config('app.tenant_id', $1, true)", tenant.String()); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func addUser(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, issuer, sub string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx,
		`insert into resonans_app.users (tenant_id, issuer, sub) values ($1, $2, $3) returning id`,
		tenant, issuer, sub).Scan(&id)
	return id, err
}

func usersApp(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, dsn := testpg.PoolDSN(t)
	if _, err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// The app role, not the migrator: the migrator is a superuser and would
	// measure the superuser instead of the policy.
	return testpg.AppPool(t, pool, dsn, "users_it_app")
}

// Tenant B can neither see nor change tenant A's users or roles, and the same
// query that proves it returns the rows to tenant A (the positive control).
func TestUsersAndRolesAreTenantIsolated(t *testing.T) {
	ctx := context.Background()
	app := usersApp(t)
	a, b := uuid.New(), uuid.New()

	var aUser uuid.UUID
	if err := asTenant(ctx, t, app, a, func(tx pgx.Tx) error {
		id, err := addUser(ctx, tx, a, "https://idp.example", "alice")
		if err != nil {
			return err
		}
		aUser = id
		_, err = tx.Exec(ctx, `insert into resonans_app.user_roles (tenant_id, user_id, role) values ($1, $2, 'admin')`, a, id)
		return err
	}); err != nil {
		t.Fatalf("tenant A setup: %v", err)
	}

	count := func(tenant uuid.UUID, table string) int {
		var n int
		if err := asTenant(ctx, t, app, tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `select count(*) from resonans_app.`+table).Scan(&n)
		}); err != nil {
			t.Fatalf("count %s as %s: %v", table, tenant, err)
		}
		return n
	}
	for _, table := range []string{"users", "user_roles"} {
		if got := count(a, table); got != 1 { // positive control
			t.Fatalf("tenant A sees %d rows in %s, want 1 — the instrument is broken", got, table)
		}
		if got := count(b, table); got != 0 {
			t.Errorf("tenant B sees %d rows of tenant A in %s", got, table)
		}
	}

	// B cannot modify or delete A's rows (RLS filters them: 0 rows affected).
	_ = asTenant(ctx, t, app, b, func(tx pgx.Tx) error {
		for _, q := range []string{
			`update resonans_app.users set status = 'disabled' where id = '` + aUser.String() + `'`,
			`update resonans_app.user_roles set role = 'viewer' where user_id = '` + aUser.String() + `'`,
			`delete from resonans_app.user_roles where user_id = '` + aUser.String() + `'`,
			`delete from resonans_app.users where id = '` + aUser.String() + `'`,
		} {
			if tag, err := tx.Exec(ctx, q); err != nil || tag.RowsAffected() != 0 {
				t.Errorf("tenant B ran %q: affected %d, err %v; want 0 rows", q, tag.RowsAffected(), err)
			}
		}
		return nil
	})
	if got := count(a, "users"); got != 1 {
		t.Errorf("tenant A lost its user: %d rows", got)
	}

	// B cannot write rows that claim tenant A, nor point a role at A's user.
	err := asTenant(ctx, t, app, b, func(tx pgx.Tx) error {
		_, err := addUser(ctx, tx, a, "https://idp.example", "mallory")
		return err
	})
	if err == nil {
		t.Error("tenant B inserted a user row owned by tenant A")
	}
	err = asTenant(ctx, t, app, b, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `insert into resonans_app.user_roles (tenant_id, user_id, role) values ($1, $2, 'admin')`, b, aUser)
		return err
	})
	if err == nil {
		t.Error("tenant B assigned a role to tenant A's user (composite foreign key must refuse)")
	}
}

// Without the tenant GUC every query raises: fail closed, not "no rows".
func TestUsersFailClosedWithoutTenant(t *testing.T) {
	ctx := context.Background()
	app := usersApp(t)
	for _, table := range []string{"users", "user_roles"} {
		var n int
		err := app.QueryRow(ctx, `select count(*) from resonans_app.`+table).Scan(&n)
		if err == nil || !strings.Contains(err.Error(), "app.tenant_id") {
			t.Errorf("%s without a tenant GUC: n=%d err=%v; want an unrecognized-parameter error", table, n, err)
		}
	}
}

// The data model refuses what the login rules never allow.
func TestUsersConstraints(t *testing.T) {
	ctx := context.Background()
	app := usersApp(t)
	tenant := uuid.New()

	cases := []struct {
		name string
		sql  string
	}{
		{"empty issuer", `insert into resonans_app.users (tenant_id, issuer, sub) values ('%T', '', 'x')`},
		{"empty sub", `insert into resonans_app.users (tenant_id, issuer, sub) values ('%T', 'i', '')`},
		{"unknown status", `insert into resonans_app.users (tenant_id, issuer, sub, status) values ('%T', 'i', 's', 'banned')`},
		{"deleted but active", `insert into resonans_app.users (tenant_id, issuer, sub, deleted_at) values ('%T', 'i', 's', now())`},
	}
	for _, c := range cases {
		err := asTenant(ctx, t, app, tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, strings.ReplaceAll(c.sql, "%T", tenant.String()))
			return err
		})
		if err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}

	// Duplicate identity in one tenant is refused; the same identity in another
	// tenant is a separate row.
	if err := asTenant(ctx, t, app, tenant, func(tx pgx.Tx) error {
		if _, err := addUser(ctx, tx, tenant, "i", "s"); err != nil {
			return err
		}
		_, err := addUser(ctx, tx, tenant, "i", "s")
		return err
	}); err == nil {
		t.Error("duplicate (tenant, issuer, sub) accepted")
	}
	other := uuid.New()
	for _, tn := range []uuid.UUID{tenant, other} {
		if err := asTenant(ctx, t, app, tn, func(tx pgx.Tx) error {
			_, err := addUser(ctx, tx, tn, "i", "s")
			return err
		}); err != nil {
			t.Errorf("same identity in tenant %s refused: %v", tn, err)
		}
	}

	// An unknown role is refused, and a default is not invented: no row, no role.
	err := asTenant(ctx, t, app, tenant, func(tx pgx.Tx) error {
		id, err := addUser(ctx, tx, tenant, "i", "other")
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `insert into resonans_app.user_roles (tenant_id, user_id, role) values ($1, $2, 'owner')`, tenant, id)
		return err
	})
	if err == nil {
		t.Error("role 'owner' accepted")
	}
	if err := asTenant(ctx, t, app, tenant, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `select count(*) from resonans_app.user_roles`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("%d role rows exist for users that were never assigned one", n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Deleting a user removes its role rows with it.
func TestUserRolesCascadeOnUserDelete(t *testing.T) {
	ctx := context.Background()
	app := usersApp(t)
	tenant := uuid.New()
	if err := asTenant(ctx, t, app, tenant, func(tx pgx.Tx) error {
		id, err := addUser(ctx, tx, tenant, "i", "s")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into resonans_app.user_roles (tenant_id, user_id, role) values ($1, $2, 'editor')`, tenant, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `delete from resonans_app.users where id = $1`, id); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `select count(*) from resonans_app.user_roles`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("%d orphan role rows after deleting the user", n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
