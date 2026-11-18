-- Resotone's own users and role assignments.
--
-- A user is whoever logged in through the configured OpenID Connect provider,
-- identified by the provider's issuer and the token's `sub`. The row is created
-- at the first successful login (just in time) and holds NO credentials: the
-- provider authenticates, Resotone only remembers who it has seen and what that
-- person may do here. Personal-data fields: sub, display_name, email,
-- last_login_at (see the README's privacy section).
--
-- Both tables are tenant-owned and follow the same fail-closed RLS pattern as
-- documents: the app.tenant_id GUC must be set, or the query raises. A person who
-- works in two tenants has one users row per tenant, so a tenant's administrator
-- can only ever see and change the rows of their own tenant.
--
-- No grants here on purpose: the default privileges the setup scripts attach to
-- the migrator (deploy/initdb, deploy/manual-setup.sql) give the app role
-- select/insert/update/delete on every table this migrator creates, exactly as
-- for the tables before it.

create table if not exists resonans_app.users (
  id            uuid        primary key default gen_random_uuid(),
  tenant_id     uuid        not null,
  issuer        text        not null check (issuer <> ''),
  sub           text        not null check (sub <> ''),
  display_name  text        not null default '',
  email         text,
  status        text        not null default 'active'
                            check (status in ('active', 'disabled')),
  created_at    timestamptz not null default now(),
  last_login_at timestamptz,
  -- Set when the person's data was erased: the row stays (authorship and audit
  -- references keep resolving) but issuer/sub/name/email are replaced. An erased
  -- user is always disabled.
  deleted_at    timestamptz,
  constraint users_identity_unique unique (tenant_id, issuer, sub),
  -- Target of user_roles' composite foreign key, so a role row can never point
  -- at a user of another tenant.
  constraint users_tenant_id_id_unique unique (tenant_id, id),
  constraint users_deleted_is_disabled check (deleted_at is null or status = 'disabled')
);

-- The role a user holds in a tenant. Roles are Resotone's own four; the identity
-- provider's claim is only an optional input (see the README, "Login and roles").
-- No row = no role = every request is 403. There is no default role.
create table if not exists resonans_app.user_roles (
  tenant_id   uuid        not null,
  user_id     uuid        not null,
  role        text        not null check (role in ('viewer', 'editor', 'publisher', 'admin')),
  assigned_at timestamptz not null default now(),
  primary key (tenant_id, user_id),
  constraint user_roles_user_fk foreign key (tenant_id, user_id)
    references resonans_app.users (tenant_id, id) on delete cascade
);

-- "Who are this tenant's admins" is asked on every demotion and disable (last-admin
-- protection), so index it.
create index if not exists user_roles_tenant_role_idx
  on resonans_app.user_roles (tenant_id, role);

alter table resonans_app.users enable row level security;
alter table resonans_app.users force row level security;
create policy users_tenant_isolation on resonans_app.users
  for all
  using      (tenant_id = current_setting('app.tenant_id', false)::uuid)
  with check (tenant_id = current_setting('app.tenant_id', false)::uuid);

alter table resonans_app.user_roles enable row level security;
alter table resonans_app.user_roles force row level security;
create policy user_roles_tenant_isolation on resonans_app.user_roles
  for all
  using      (tenant_id = current_setting('app.tenant_id', false)::uuid)
  with check (tenant_id = current_setting('app.tenant_id', false)::uuid);
