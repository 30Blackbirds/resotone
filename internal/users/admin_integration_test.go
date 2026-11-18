//go:build integration

package users

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/30blackbirds/resotone/internal/auth"
)

// seed creates a user in a tenant through the operator path and returns its id.
func seed(ctx context.Context, t *testing.T, s *PostgresStore, tenant uuid.UUID, sub string, role auth.Role) uuid.UUID {
	t.Helper()
	res, err := s.GrantRole(ctx, tenant, "https://idp.example", sub, role)
	if err != nil {
		t.Fatalf("seed %s: %v", sub, err)
	}
	return res.UserID
}

func roleOf(ctx context.Context, t *testing.T, s *PostgresStore, tenant, id uuid.UUID) string {
	t.Helper()
	u, err := s.Get(ctx, tenant, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	return u.Role
}

// A tenant-A admin can never see or change a tenant-B user. Each refusal is
// paired with the same call succeeding in the right tenant, so a store that
// refuses everything cannot pass.
func TestAdminCannotReachAnotherTenantsUsers(t *testing.T) {
	ctx := context.Background()
	s, _ := stores(t)
	a, b := uuid.New(), uuid.New()
	seed(ctx, t, s, a, "alice-admin", auth.RoleAdmin)
	seed(ctx, t, s, a, "alice-second-admin", auth.RoleAdmin)
	bAdmin := seed(ctx, t, s, b, "bob-admin", auth.RoleAdmin)
	bEditor := seed(ctx, t, s, b, "bob-editor", auth.RoleEditor)

	listA, err := s.List(ctx, a)
	if err != nil || len(listA) != 2 {
		t.Fatalf("tenant A lists %d users (%v), want its own 2", len(listA), err)
	}
	for _, u := range listA {
		if u.Sub == "bob-admin" || u.Sub == "bob-editor" {
			t.Errorf("tenant A's list contains tenant B's user %q", u.Sub)
		}
	}

	// Tenant A's identity used against tenant B's ids: all not-found, no change.
	if _, err := s.Get(ctx, a, bEditor); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get across tenants = %v, want ErrNotFound", err)
	}
	if err := s.SetRole(ctx, a, bEditor, auth.RoleAdmin); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetRole across tenants = %v, want ErrNotFound", err)
	}
	if err := s.SetDisabled(ctx, a, bEditor, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetDisabled across tenants = %v, want ErrNotFound", err)
	}
	if err := s.Erase(ctx, a, bEditor); !errors.Is(err, ErrNotFound) {
		t.Errorf("Erase across tenants = %v, want ErrNotFound", err)
	}
	got, err := s.Get(ctx, b, bEditor)
	if err != nil || got.Role != "editor" || got.Status != "active" || got.ErasedAt != nil || got.Sub != "bob-editor" {
		t.Errorf("tenant B's user changed by tenant A: %+v, %v", got, err)
	}
	// Positive control: the same calls work inside the owning tenant.
	if err := s.SetRole(ctx, b, bEditor, auth.RolePublisher); err != nil {
		t.Errorf("SetRole in the owning tenant: %v", err)
	}
	// And tenant B's last-admin count is its own, not tenant A's two admins.
	if err := s.SetRole(ctx, b, bAdmin, auth.RoleViewer); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("demoting tenant B's only admin = %v, want ErrLastAdmin (tenant A's admins must not count)", err)
	}
}

func TestLastAdminCannotBeDemotedDisabledOrErased(t *testing.T) {
	ctx := context.Background()
	s, _ := stores(t)
	tenant := uuid.New()
	only := seed(ctx, t, s, tenant, "only", auth.RoleAdmin)
	other := seed(ctx, t, s, tenant, "viewer", auth.RoleViewer)

	if err := s.SetRole(ctx, tenant, only, auth.RoleEditor); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("demote the only admin = %v", err)
	}
	if err := s.SetDisabled(ctx, tenant, only, true); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("disable the only admin = %v", err)
	}
	if err := s.Erase(ctx, tenant, only); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("erase the only admin = %v", err)
	}
	if u, _ := s.Get(ctx, tenant, only); u.Role != "admin" || u.Status != "active" || u.ErasedAt != nil {
		t.Errorf("the refused changes still altered the admin: %+v", u)
	}
	// Re-assigning admin to the admin is not a demotion.
	if err := s.SetRole(ctx, tenant, only, auth.RoleAdmin); err != nil {
		t.Errorf("assign admin to the admin: %v", err)
	}

	// With a second admin the first can go; then the second is the last one.
	if err := s.SetRole(ctx, tenant, other, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRole(ctx, tenant, only, auth.RoleEditor); err != nil {
		t.Errorf("demote one of two admins: %v", err)
	}
	if err := s.SetDisabled(ctx, tenant, other, true); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("disable the remaining admin = %v", err)
	}
}

// A disabled admin cannot act, so it does not count as an admin to keep.
func TestDisabledAdminsDoNotCountTowardsTheLastAdmin(t *testing.T) {
	ctx := context.Background()
	s, _ := stores(t)
	tenant := uuid.New()
	a1 := seed(ctx, t, s, tenant, "a1", auth.RoleAdmin)
	a2 := seed(ctx, t, s, tenant, "a2", auth.RoleAdmin)
	if err := s.SetDisabled(ctx, tenant, a1, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRole(ctx, tenant, a2, auth.RoleViewer); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("demote the only ACTIVE admin (the other is disabled) = %v, want ErrLastAdmin", err)
	}
	// Re-enabling the disabled one makes the demotion possible again.
	if err := s.SetDisabled(ctx, tenant, a1, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRole(ctx, tenant, a2, auth.RoleViewer); err != nil {
		t.Errorf("demote with two active admins: %v", err)
	}
}

// Two admins demoting each other at the same moment must not leave the tenant
// with none: exactly one wins.
func TestConcurrentDemotionsLeaveOneAdmin(t *testing.T) {
	ctx := context.Background()
	s, _ := stores(t)
	for round := 0; round < 5; round++ {
		tenant := uuid.New()
		ids := []uuid.UUID{seed(ctx, t, s, tenant, "a", auth.RoleAdmin), seed(ctx, t, s, tenant, "b", auth.RoleAdmin)}
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i := range ids {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[i] = s.SetRole(ctx, tenant, ids[i], auth.RoleViewer)
			}()
		}
		wg.Wait()
		admins := 0
		for _, id := range ids {
			if roleOf(ctx, t, s, tenant, id) == "admin" {
				admins++
			}
		}
		refused := 0
		for _, e := range errs {
			if errors.Is(e, ErrLastAdmin) {
				refused++
			}
		}
		if admins != 1 || refused != 1 {
			t.Fatalf("round %d: %d admins left, %d refusals (%v); want exactly 1 and 1", round, admins, refused, errs)
		}
	}
}

func TestEraseDisablesStripsAndPseudonymises(t *testing.T) {
	ctx := context.Background()
	s, admin := stores(t)
	tenant := uuid.New()
	seed(ctx, t, s, tenant, "keeper", auth.RoleAdmin)
	id := seed(ctx, t, s, tenant, "ada", auth.RoleEditor)
	if _, err := s.Login(ctx, ident(tenant, "ada")); err != nil { // gives it a name and email
		t.Fatal(err)
	}

	if err := s.Erase(ctx, tenant, id); err != nil {
		t.Fatalf("erase: %v", err)
	}
	u, err := s.Get(ctx, tenant, id)
	if err != nil {
		t.Fatal(err)
	}
	if u.Sub == "ada" || u.DisplayName != "" || u.Email != "" || u.Issuer == "https://idp.example" ||
		u.Status != "disabled" || u.ErasedAt == nil || u.Role != "none" {
		t.Errorf("after erase: %+v", u)
	}
	// Nothing of the person is left anywhere in the tables.
	var leftovers int
	_ = admin.QueryRow(ctx, `select count(*) from resonans_app.users where sub = 'ada' or email = 'ada@example.org' or display_name = 'Ada'`).Scan(&leftovers)
	if leftovers != 0 {
		t.Errorf("%d rows still hold the erased person's data", leftovers)
	}
	// Idempotent, and nothing can be changed on an erased user.
	if err := s.Erase(ctx, tenant, id); err != nil {
		t.Errorf("second erase: %v", err)
	}
	if err := s.SetRole(ctx, tenant, id, auth.RoleAdmin); !errors.Is(err, ErrErased) {
		t.Errorf("SetRole on an erased user = %v, want ErrErased", err)
	}
	if err := s.SetDisabled(ctx, tenant, id, false); !errors.Is(err, ErrErased) {
		t.Errorf("enable an erased user = %v, want ErrErased", err)
	}
	// Logging in again at the provider creates a NEW user with no role; erasure
	// is from Resotone, not from the provider.
	again, err := s.Login(ctx, ident(tenant, "ada"))
	if err != nil || again.ID == id || again.Role != auth.RoleNone || again.Disabled {
		t.Errorf("login after erase = %+v, %v; want a new user without a role", again, err)
	}
}

func TestListAndGetReportRoleAndStatus(t *testing.T) {
	ctx := context.Background()
	s, _ := stores(t)
	tenant := uuid.New()
	seed(ctx, t, s, tenant, "admin", auth.RoleAdmin)
	norole, _ := s.Login(ctx, ident(tenant, "norole"))
	list, err := s.List(ctx, tenant)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %d users, %v", len(list), err)
	}
	u, err := s.Get(ctx, tenant, norole.ID)
	if err != nil || u.Role != "none" || u.Status != "active" || u.LastLoginAt == nil || u.Email != "ada@example.org" {
		t.Errorf("get = %+v, %v; want an active user with role none and a login time", u, err)
	}
	empty, err := s.List(ctx, uuid.New())
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("list of an empty tenant = %v, %v; want an empty non-nil slice (JSON [] not null)", empty, err)
	}
}
