package users

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/30blackbirds/resotone/internal/auth"
)

func TestMemoryStoreCreatesUserWithoutRoleAndKeepsTenantsApart(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	a, b := uuid.New(), uuid.New()
	id := auth.Identity{Tenant: a, Issuer: "https://idp.example", Sub: "ada"}

	first, err := m.Login(ctx, id)
	if err != nil || first.Role != auth.RoleNone || first.Disabled {
		t.Fatalf("first login = %+v, %v; want an active user without a role", first, err)
	}
	if again, _ := m.Login(ctx, id); again.ID != first.ID {
		t.Fatalf("second login gave a different user")
	}
	id.Tenant = b
	if other, _ := m.Login(ctx, id); other.ID == first.ID {
		t.Fatalf("the same identity in another tenant resolved to the same user")
	}
}
