package users

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/30blackbirds/resotone/internal/auth"
)

// GrantResult says what GrantRole changed, so the operator's command can report
// it and a repeat run can be seen to be a no-op.
type GrantResult struct {
	UserID      uuid.UUID
	UserCreated bool
	RoleChanged bool
	// Disabled is true when the user exists and is disabled: the role is
	// recorded but they still cannot log in until someone enables them.
	Disabled bool
}

// GrantRole assigns role to the user (issuer, sub) in the tenant, creating the
// user record first when it does not exist yet. It is the operator's path to a
// first administrator: no one has to have logged in before, and nothing is open
// to anyone else. Idempotent: running it again with the same arguments changes
// nothing. It never changes a user's status.
func (s *PostgresStore) GrantRole(ctx context.Context, tenant uuid.UUID, issuer, sub string, role auth.Role) (GrantResult, error) {
	issuer, sub = strings.TrimSpace(issuer), strings.TrimSpace(sub)
	if issuer == "" || sub == "" {
		return GrantResult{}, errors.New("users: issuer and sub are both required")
	}
	if tenant == uuid.Nil {
		return GrantResult{}, errors.New("users: a tenant is required")
	}
	if role == auth.RoleNone || role > auth.RoleAdmin {
		return GrantResult{}, errors.New("users: not a role that can be granted")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return GrantResult{}, fmt.Errorf("users: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "select set_config('app.tenant_id', $1, true)", tenant.String()); err != nil {
		return GrantResult{}, fmt.Errorf("users: set tenant: %w", err)
	}

	var res GrantResult
	var status string
	err = tx.QueryRow(ctx, `select id, status from resonans_app.users where tenant_id = $1 and issuer = $2 and sub = $3`,
		tenant, issuer, sub).Scan(&res.UserID, &status)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// (xmax = 0 is true only for a freshly inserted row, not the conflict path.)
		// ON CONFLICT covers a login that created the user since the read above.
		if err := tx.QueryRow(ctx, `
			insert into resonans_app.users (tenant_id, issuer, sub) values ($1, $2, $3)
			on conflict (tenant_id, issuer, sub) do update set tenant_id = excluded.tenant_id
			returning id, status, (xmax = 0)`, tenant, issuer, sub).Scan(&res.UserID, &status, &res.UserCreated); err != nil {
			return GrantResult{}, fmt.Errorf("users: create: %w", err)
		}
	case err != nil:
		return GrantResult{}, fmt.Errorf("users: read: %w", err)
	}
	res.Disabled = status != "active"

	tag, err := tx.Exec(ctx, `
		insert into resonans_app.user_roles (tenant_id, user_id, role) values ($1, $2, $3)
		on conflict (tenant_id, user_id) do update set role = excluded.role, assigned_at = now()
		where resonans_app.user_roles.role <> excluded.role`, tenant, res.UserID, role.String())
	if err != nil {
		return GrantResult{}, fmt.Errorf("users: assign: %w", err)
	}
	res.RoleChanged = tag.RowsAffected() > 0
	if err := tx.Commit(ctx); err != nil {
		return GrantResult{}, fmt.Errorf("users: commit: %w", err)
	}
	return res, nil
}
