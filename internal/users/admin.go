package users

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/30blackbirds/resotone/internal/auth"
)

var (
	// ErrNotFound: no such user in this tenant. Another tenant's user is the same
	// answer, on purpose: row level security hides it, so the two are the same fact.
	ErrNotFound = errors.New("users: no such user")
	// ErrLastAdmin: the change would leave the tenant without an active admin, and
	// with nobody able to assign roles again.
	ErrLastAdmin = errors.New("users: this is the tenant's last active admin")
	// ErrErased: the user's data was erased; there is nothing left to change.
	ErrErased = errors.New("users: this user's data has been erased")
)

// User is one user as an admin of the same tenant sees them.
type User struct {
	ID          uuid.UUID  `json:"id"`
	Issuer      string     `json:"issuer"`
	Sub         string     `json:"sub"`
	DisplayName string     `json:"displayName"`
	Email       string     `json:"email,omitempty"`
	Status      string     `json:"status"`
	Role        string     `json:"role"` // "none" when no role has been assigned
	CreatedAt   time.Time  `json:"createdAt"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
	ErasedAt    *time.Time `json:"erasedAt,omitempty"`
}

// Admin is what the admin API needs. Every method is scoped to one tenant and
// reaches no row of another.
type Admin interface {
	List(ctx context.Context, tenant uuid.UUID) ([]User, error)
	// Get returns one user; it is also the export of that user's data.
	Get(ctx context.Context, tenant, id uuid.UUID) (User, error)
	SetRole(ctx context.Context, tenant, id uuid.UUID, role auth.Role) error
	SetDisabled(ctx context.Context, tenant, id uuid.UUID, disabled bool) error
	// Erase pseudonymises the user: issuer/sub/name/email are replaced, the role
	// is removed and the user is disabled for good. The row stays so references
	// to it keep resolving. Idempotent.
	Erase(ctx context.Context, tenant, id uuid.UUID) error
}

var _ Admin = (*PostgresStore)(nil)

func (s *PostgresStore) inTenantTx(ctx context.Context, tenant uuid.UUID, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("users: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "select set_config('app.tenant_id', $1, true)", tenant.String()); err != nil {
		return fmt.Errorf("users: set tenant: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("users: commit: %w", err)
	}
	return nil
}

const userCols = `u.id, u.issuer, u.sub, u.display_name, coalesce(u.email, ''), u.status,
	coalesce(r.role, 'none'), u.created_at, u.last_login_at, u.deleted_at`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Issuer, &u.Sub, &u.DisplayName, &u.Email, &u.Status, &u.Role, &u.CreatedAt, &u.LastLoginAt, &u.ErasedAt)
	return u, err
}

// List implements Admin.
func (s *PostgresStore) List(ctx context.Context, tenant uuid.UUID) ([]User, error) {
	out := []User{}
	err := s.inTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+userCols+`
			from resonans_app.users u
			left join resonans_app.user_roles r on r.tenant_id = u.tenant_id and r.user_id = u.id
			where u.tenant_id = $1 order by u.created_at, u.id`, tenant)
		if err != nil {
			return fmt.Errorf("users: list: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			u, err := scanUser(rows)
			if err != nil {
				return fmt.Errorf("users: list scan: %w", err)
			}
			out = append(out, u)
		}
		return rows.Err()
	})
	return out, err
}

// Get implements Admin.
func (s *PostgresStore) Get(ctx context.Context, tenant, id uuid.UUID) (User, error) {
	var u User
	err := s.inTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		var err error
		u, err = scanUser(tx.QueryRow(ctx, `select `+userCols+`
			from resonans_app.users u
			left join resonans_app.user_roles r on r.tenant_id = u.tenant_id and r.user_id = u.id
			where u.tenant_id = $1 and u.id = $2`, tenant, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	return u, err
}

// lockTarget locks the user's row and reports whether they are an active admin
// right now. Locking first, then the admin set, is the same order everywhere.
func lockTarget(ctx context.Context, tx pgx.Tx, tenant, id uuid.UUID) (activeAdmin, erased bool, err error) {
	var status, role string
	var deletedAt *time.Time
	err = tx.QueryRow(ctx, `
		select u.status, coalesce(r.role, ''), u.deleted_at
		from resonans_app.users u
		left join resonans_app.user_roles r on r.tenant_id = u.tenant_id and r.user_id = u.id
		where u.tenant_id = $1 and u.id = $2
		for update of u`, tenant, id).Scan(&status, &role, &deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, ErrNotFound
	}
	if err != nil {
		return false, false, fmt.Errorf("users: lock user: %w", err)
	}
	return status == "active" && role == "admin", deletedAt != nil, nil
}

// guardLastAdmin refuses when the tenant has no active admin besides the target.
// It locks every active admin's role row, so two concurrent demotions of two
// different admins queue instead of both seeing "one other admin left"; the
// second then re-reads the committed state and is refused.
func guardLastAdmin(ctx context.Context, tx pgx.Tx, tenant uuid.UUID) error {
	var n int
	if err := tx.QueryRow(ctx, `
		select count(*) from (
		  select 1 from resonans_app.user_roles r
		  join resonans_app.users u on u.tenant_id = r.tenant_id and u.id = r.user_id
		  where r.tenant_id = $1 and r.role = 'admin' and u.status = 'active'
		  for update of r
		) admins`, tenant).Scan(&n); err != nil {
		return fmt.Errorf("users: count admins: %w", err)
	}
	if n <= 1 {
		return ErrLastAdmin
	}
	return nil
}

// SetRole implements Admin.
func (s *PostgresStore) SetRole(ctx context.Context, tenant, id uuid.UUID, role auth.Role) error {
	if role == auth.RoleNone || role > auth.RoleAdmin {
		return errors.New("users: not a role that can be assigned")
	}
	return s.inTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		activeAdmin, erased, err := lockTarget(ctx, tx, tenant, id)
		if err != nil {
			return err
		}
		if erased {
			return ErrErased
		}
		if activeAdmin && role != auth.RoleAdmin {
			if err := guardLastAdmin(ctx, tx, tenant); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `
			insert into resonans_app.user_roles (tenant_id, user_id, role) values ($1, $2, $3)
			on conflict (tenant_id, user_id) do update set role = excluded.role, assigned_at = now()
			where resonans_app.user_roles.role <> excluded.role`, tenant, id, role.String()); err != nil {
			return fmt.Errorf("users: assign: %w", err)
		}
		return nil
	})
}

// SetDisabled implements Admin.
func (s *PostgresStore) SetDisabled(ctx context.Context, tenant, id uuid.UUID, disabled bool) error {
	return s.inTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		activeAdmin, erased, err := lockTarget(ctx, tx, tenant, id)
		if err != nil {
			return err
		}
		if erased {
			return ErrErased
		}
		if disabled && activeAdmin {
			if err := guardLastAdmin(ctx, tx, tenant); err != nil {
				return err
			}
		}
		status := "active"
		if disabled {
			status = "disabled"
		}
		if _, err := tx.Exec(ctx, `update resonans_app.users set status = $3 where tenant_id = $1 and id = $2`, tenant, id, status); err != nil {
			return fmt.Errorf("users: set status: %w", err)
		}
		return nil
	})
}

// Erase implements Admin.
func (s *PostgresStore) Erase(ctx context.Context, tenant, id uuid.UUID) error {
	return s.inTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		activeAdmin, erased, err := lockTarget(ctx, tx, tenant, id)
		if err != nil {
			return err
		}
		if erased {
			return nil
		}
		if activeAdmin {
			if err := guardLastAdmin(ctx, tx, tenant); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `delete from resonans_app.user_roles where tenant_id = $1 and user_id = $2`, tenant, id); err != nil {
			return fmt.Errorf("users: erase role: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			update resonans_app.users
			set issuer = 'erased', sub = 'erased:' || id::text, display_name = '', email = null,
			    status = 'disabled', deleted_at = now()
			where tenant_id = $1 and id = $2`, tenant, id); err != nil {
			return fmt.Errorf("users: erase: %w", err)
		}
		return nil
	})
}
