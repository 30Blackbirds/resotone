// Package users is Resotone's own user store: who has logged in, and the role
// each was assigned. It holds no credentials; the identity provider
// authenticates. Tenant isolation is row level security, set per transaction the
// same way as the other stores.
package users

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/30blackbirds/resotone/internal/auth"
)

// loginRefresh is how stale last_login_at may get before an authenticated
// request writes it again. Every API request passes through Login, so writing
// each time would turn reads into writes.
const loginRefresh = 15 * time.Minute

// PostgresStore is the persistent auth.UserStore.
type PostgresStore struct{ pool *pgxpool.Pool }

// NewPostgresStore wraps a pool that connects as the (non-bypass) app role.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

var _ auth.UserStore = (*PostgresStore)(nil)

func parseRole(s string) auth.Role {
	r, _ := auth.ParseRole(s) // "" and anything unknown stay RoleNone
	return r
}

// Login implements auth.UserStore.
func (s *PostgresStore) Login(ctx context.Context, id auth.Identity) (auth.UserRecord, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return auth.UserRecord{}, fmt.Errorf("users: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "select set_config('app.tenant_id', $1, true)", id.Tenant.String()); err != nil {
		return auth.UserRecord{}, fmt.Errorf("users: set tenant: %w", err)
	}

	const sel = `
		select u.id, u.status, coalesce(r.role, ''), u.last_login_at, u.display_name, coalesce(u.email, '')
		from resonans_app.users u
		left join resonans_app.user_roles r on r.tenant_id = u.tenant_id and r.user_id = u.id
		where u.tenant_id = $1 and u.issuer = $2 and u.sub = $3`
	var (
		rec       auth.UserRecord
		status    string
		role      string
		lastLogin *time.Time
		name      string
		email     string
	)
	err = tx.QueryRow(ctx, sel, id.Tenant, id.Issuer, id.Sub).Scan(&rec.ID, &status, &role, &lastLogin, &name, &email)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// First login: create the user with no role. A concurrent first login
		// loses the insert race and reads the winner's row.
		if _, err := tx.Exec(ctx, `
			insert into resonans_app.users (tenant_id, issuer, sub, display_name, email, last_login_at)
			values ($1, $2, $3, $4, nullif($5, ''), now())
			on conflict (tenant_id, issuer, sub) do nothing`,
			id.Tenant, id.Issuer, id.Sub, id.DisplayName, id.Email); err != nil {
			return auth.UserRecord{}, fmt.Errorf("users: create: %w", err)
		}
		if err := tx.QueryRow(ctx, sel, id.Tenant, id.Issuer, id.Sub).Scan(&rec.ID, &status, &role, &lastLogin, &name, &email); err != nil {
			return auth.UserRecord{}, fmt.Errorf("users: read after create: %w", err)
		}
	case err != nil:
		return auth.UserRecord{}, fmt.Errorf("users: read: %w", err)
	default:
		stale := lastLogin == nil || time.Since(*lastLogin) > loginRefresh
		changed := (id.DisplayName != "" && id.DisplayName != name) || (id.Email != "" && id.Email != email)
		if status == "active" && (stale || changed) {
			if _, err := tx.Exec(ctx, `
				update resonans_app.users set last_login_at = now(),
				  display_name = case when $2 <> '' then $2 else display_name end,
				  email = coalesce(nullif($3, ''), email)
				where id = $1 and tenant_id = $4 and status = 'active'`,
				rec.ID, id.DisplayName, id.Email, id.Tenant); err != nil {
				return auth.UserRecord{}, fmt.Errorf("users: record login: %w", err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return auth.UserRecord{}, fmt.Errorf("users: commit: %w", err)
	}
	rec.Disabled = status != "active"
	rec.Role = parseRole(role)
	return rec, nil
}

// MemoryStore is the in-process store for runs without a database. It forgets
// everything on restart.
type MemoryStore struct {
	mu    sync.Mutex
	users map[key]*memUser
}

type key struct {
	tenant      uuid.UUID
	issuer, sub string
}

type memUser struct {
	id       uuid.UUID
	disabled bool
	role     auth.Role
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore { return &MemoryStore{users: map[key]*memUser{}} }

var _ auth.UserStore = (*MemoryStore)(nil)

// Login implements auth.UserStore.
func (m *MemoryStore) Login(_ context.Context, id auth.Identity) (auth.UserRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := key{id.Tenant, id.Issuer, id.Sub}
	u, ok := m.users[k]
	if !ok {
		u = &memUser{id: uuid.New()}
		m.users[k] = u
	}
	return auth.UserRecord{ID: u.id, Disabled: u.disabled, Role: u.role}, nil
}
