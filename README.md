# Resotone

> A self-hosted, multi-tenant headless CMS written in Go. *Formerly Resonans.*

[![CI](https://github.com/30Blackbirds/resotone/actions/workflows/ci-go.yml/badge.svg?branch=main)](https://github.com/30Blackbirds/resotone/actions/workflows/ci-go.yml)
[![License: AGPL-3.0](https://img.shields.io/github/license/30Blackbirds/resotone)](LICENSE)
[![Latest release](https://img.shields.io/github/v/release/30Blackbirds/resotone)](https://github.com/30Blackbirds/resotone/releases)
![Status: beta](https://img.shields.io/badge/status-beta-orange)

One content engine behind many surfaces: a learning platform, a blog, marketing pages, a
knowledge base. Content types are declared in YAML, so adding one takes no Go code. Tenants
are isolated by the database itself. One Go binary and Postgres; no Redis, no queue, no
search cluster.

> **Name note.** The project was called Resonans. The name in this README is now
> **Resotone**, but the container images, the Go binary, the environment variables and
> the database objects still carry the old name (`resotone`, `RESONANS_*`), and this
> document uses them exactly as they are.

**Status: beta.** The API (`/api/v1`), the configuration and the content model can still
change in ways that need you to act. The major version number does not mean a stable API:
it comes from the release tooling, which bumps the major only for changes marked as
breaking. Pin an exact version and read [CHANGELOG.md](CHANGELOG.md) for every version you
skip; operator steps are listed there under **ACTION REQUIRED**.

## Contents

- [Quickstart](#quickstart): [try it in 60 seconds](#try-it)
- [Features](#features)
- [Is it ready?](#is-it-ready)
- [Self-hosting in production](#self-hosting-in-production)
- [Configuration](#configuration)
- [Error monitoring (Sentry, optional)](#error-monitoring-sentry-optional)
- [Login and roles](#login-and-roles)
- [Upgrading](#upgrading)
- [Reference](#reference): [tenant isolation](#tenant-isolation), [personal data](#personal-data), [content model](#content-model), [HTTP surfaces](#http-surfaces), [deleting content](#deleting-content), [asset storage](#asset-storage)
- [Development](#development)
- [Contributing](#contributing) · [Security](#security) · [License](#copyright-and-license) · [Links](#links)

## Quickstart

You need Docker with Compose 2.24 or newer. Each option below builds the images from your
checkout unless it says otherwise, so no registry account is needed. The first build takes a
few minutes.

### Try it

No login provider of your own needed:

```bash
git clone https://github.com/30blackbirds/resotone.git
cd resotone
docker compose --env-file deploy/try/env -f compose.yml -f compose.try.yml up -d --build
```

That one command starts the whole product from this repository: Postgres, the database
migrations, the content API on <http://localhost:3000> and the editor on
<http://localhost:8080>. Nothing else needs to be installed or cloned. (The clone URL is
the repository's original address, which GitHub redirects to
<https://github.com/30Blackbirds/resotone>; CI runs this block as written.)

Open <http://localhost:8080> and press log in. You land as an administrator without a
password: [Dex](https://dexidp.io), an independent OpenID Connect provider, runs alongside
with a connector that approves everyone. The passwords in `deploy/try/env` are public, so
this stack only listens on 127.0.0.1 and is for your own machine. CI boots this exact stack
from a clean clone on every pull request. If your browser cannot resolve `dex.localhost`,
add `127.0.0.1 dex.localhost` to `/etc/hosts`.

To remove it and its data, run the same command with `down -v` in place of `up -d --build`.

### Run it with your own settings

```bash
cp .env.example .env
$EDITOR .env          # fill in the passwords and point the login settings at your provider
docker compose up -d --build
```

Editor at <http://localhost:8080>, content API at <http://localhost:3000>.

### Without Docker

```bash
RESONANS_DEV_AUTH=1 go run ./cmd/resotone
```

That runs with an in-memory store and no login, taking the tenant from an `X-Dev-Tenant`
header in the editor API and from `X-Scope-OrgID` in the delivery API, from any caller.
It is for development only, and `RESONANS_DEV_AUTH=1` together with
`NODE_ENV=production` is refused rather than honoured.

## Features

- **Headless and content-type agnostic.** Content types and a shared block library are
  YAML ([content model](#content-model)); documents are validated JSONB.
- **Editor and API.** A web editor with rich text, blocks and media, draft, preview,
  publish, history, rollback and a bin; a REST editor API and a read-only delivery API for
  your own front-ends, with locales, ETags and preview. OpenAPI reference at `/docs/v1/`.
- **Multi-tenant from the first commit**, enforced by Postgres row level security rather
  than by remembering a `where` clause ([tenant isolation](#tenant-isolation)).
- **Standard OpenID Connect login**, four roles and a single-tenant mode. Resotone keeps
  its own small user table and assigns roles itself; it stores no passwords and sends no
  email ([login and roles](#login-and-roles), [personal data](#personal-data)).
- **Refuses to boot into an unsafe state.** No authentication, no database or no durable
  asset storage configured: the server exits instead of starting degraded.
- **No CDN required.** Assets go on a volume and are served by the app; put a CDN in front
  when you want one ([asset storage](#asset-storage)).
- **Yours.** AGPL-3.0, no seat pricing, no request quota. Data lives in your Postgres.

## Is it ready?

The version number cannot say when Resotone is ready to rely on, so this list does.

| # | Capability | Today |
| --- | --- | --- |
| 1 | Clone it, run one command, and reach a working editor and API, with no registry account. | **Yes.** It builds from source, and CI boots it from a clean clone. Published images also exist, for amd64 only. |
| 2 | Editors log in through any standard OpenID Connect provider, with four roles and a single-tenant mode. | **Yes, for two providers.** CI logs in through Dex; the authors run BetterAuth. Others are untested. |
| 3 | Write in an English editor: rich text, blocks and media, then draft, preview, publish, history, rollback and a bin. | **Partly.** All of it works in English or Danish, but asset and reference fields ask for a raw id, and the rich text editor is an interim one. |
| 4 | Define your own content types. | **Partly.** YAML, but compiled in, so it takes a rebuild. |
| 5 | A site or app reads published content over a documented REST API, with locales, ETags and preview. | **Yes, with limits.** Nested expansion only works for the `course` type. Lists page with a cursor, 200 documents at most per page. |
| 6 | Tenants are isolated by the database, and CI proves it on every change. | **Yes.** See [Tenant isolation](#tenant-isolation). |
| 7 | The authors run the same public release that you do. | **Not yet.** The authors' production is on an older 1.x release; the OpenID Connect editor login from 2.0 onward has not run there yet. |
| 8 | The basics for trusting a project: licence, security policy, contributor agreement, signed images with an SBOM, and docs that match the code. | **Yes.** The contributor agreement is a required check. |

## Self-hosting in production

`compose.yml` is the reference deployment: four services (`postgres`, `migrate`, `api`,
`studio`) and two named volumes (`postgres_data`, `assets_data`). Secrets come from the
environment, never from a file in an image.

### Use the published images

The images `ghcr.io/30blackbirds/resotone` (API) and `ghcr.io/30blackbirds/resotone-studio`
(editor) are public, so no login is needed. They are built for **amd64 only**: on Apple
Silicon or another arm64 machine, build from source as above.

```bash
export RESONANS_VERSION=3.6.6   # the tag has no "v"
docker compose pull
docker compose up -d --no-build
```

`docker compose up --pull always` does not do this: it skips a service that can also be
built. Pin a version for anything you care about.

### Reverse proxy and TLS

`compose.yml` deliberately does **not** terminate TLS, and it publishes its ports on
`BIND_ADDRESS` (default 127.0.0.1), editor on `STUDIO_PORT` (8080), API on `API_PORT`
(3000). Put your own reverse proxy (Caddy, nginx, Traefik, whatever you already run) in
front of both, and set `EDITOR_URL` and `PUBLIC_URL` to the public https addresses.
Publishing an editor to the internet is a decision only you can make, so it is not a
default.

### Onto a Postgres you already run

If you are not using the bundled postgres, create the database and the two roles with
[deploy/manual-setup.sql](deploy/manual-setup.sql) — not by copying statements out of
[deploy/initdb/](deploy/initdb/).

```bash
RESONANS_MIGRATOR_PASSWORD=$(openssl rand -hex 24) \
RESONANS_APP_PASSWORD=$(openssl rand -hex 24) \
  psql -v ON_ERROR_STOP=1 -U postgres -d postgres -f deploy/manual-setup.sql

DATABASE_URL='postgres://resotone_migrator:...@HOST:5432/resotone' resotone migrate
psql -v ON_ERROR_STOP=1 -U postgres -d resotone -f deploy/assert-rls-coverage.sql
```

Two files rather than one because they run in **different contexts**, and three statements
change with the context. `deploy/initdb/` is executed by the postgres image as
`POSTGRES_USER`, which in `compose.yml` *is* the migrator and therefore owns the database.
A hand-run setup is a third role that owns nothing, and there the migrator needs to own the
database (or `create schema` is refused with `42501` even when the schema exists),
`alter default privileges` needs an explicit `for role`, and the change of database has to
be a command instead of a comment. Each of those has a measurement written beside it in
`manual-setup.sql`.

Use **hex**, not base64: a `/` in a base64 password breaks DSN parsing in several clients.

The two roles, `resotone_migrator` and `resotone_app`, are plain Postgres roles created
by the SQL in this repository and nothing else. They are members of no other role and need
no extension, no cloud-provider role and no external service. The app role is created
`nosuperuser nocreatedb nocreaterole noinherit nobypassrls`, which is what keeps tenant
isolation in force. (The schema that holds the tables is still named `resonans_app`.)

### Backups

State lives in two places, and a backup needs both: the Postgres database (`resotone`) and
the asset files (the `assets_data` volume, mounted at `/var/lib/resotone/assets`, unless
you use Bunny storage). With the bundled Postgres, a database dump looks like this:

```bash
docker compose exec -T postgres pg_dump -U resotone_migrator -Fc resotone > resotone.dump
```

Back the asset volume up with whatever you already use for Docker volumes, and test a
restore. Deleted assets keep their bytes (see [Deleting content](#deleting-content)).

### Before you go live

- Set the variables marked required in [Configuration](#configuration); the server exits at
  boot if one is missing, and in production it also refuses a database role that can
  bypass row level security.
- Set `RESONANS_SOURCE_URL` if you run a modified version (see [License](#copyright-and-license)).
- Check `GET /healthz` and `GET /readyz`; `/healthz` reports the `version` and `revision`
  that is running.

## Configuration

Every setting is an environment variable; [.env.example](.env.example) documents all of
them with the reasoning. The ones that decide whether the server starts at all:

| Variable | Required in production | What it does |
| --- | --- | --- |
| `DATABASE_URL` | yes | Postgres DSN. Unset in production the server exits: the in-memory store loses content *and* does not enforce tenant isolation. |
| `RESONANS_JWT_ISSUER` + `RESONANS_JWKS_URL` | yes, both, and **https** | Editor login. Either one missing and the server exits rather than exposing the editor API to an unauthenticated header. In production both must be `https`, because the JWKS is the trust anchor — over plain http anyone on the path can substitute the signing keys. `RESONANS_ALLOW_INSECURE_JWKS=1` waives it deliberately, and every boot then logs the URL. |
| `RESONANS_JWT_AUDIENCE` | yes, with the two above | The `aud` your provider puts in Resotone's access tokens. Without it the server exits: it would otherwise accept a token minted for any other service at the same provider. |
| `RESONANS_TENANT_MODE` + `RESONANS_TENANT_ID` | for one organisation | `single` with a UUID serves one tenant and never reads a tenant claim. Default `multi` reads the tenant from `RESONANS_TENANT_CLAIM` (`org`). |
| `RESONANS_DELIVERY_TOKEN` | yes | Authenticates the trusted read surface. Unset or blank, the server exits: without it `/delivery` would trust a tenant header from anyone. Only `RESONANS_DEV_AUTH=1` outside production starts it without one. |
| `RESONANS_STORAGE_DIR` *or* all three `BUNNY_STORAGE_*` | yes, one of them | Where asset bytes live. Neither set, the server exits instead of accepting uploads it will drop on restart. |
| `PUBLIC_URL` | recommended | The API's externally reachable base URL. Asset URLs are built from it, so a wrong value shows up as broken images, not as an error. |
| `RESONANS_DEV_AUTH` | never | The single value `1` opens the unauthenticated development paths, outside production only. |

## Error monitoring (Sentry, optional)

The editor (Studio) can report browser errors, and problem reports typed in by editors, to
a [Sentry](https://sentry.io) project that **you** own. It is off unless you set a DSN.

**What is covered:** the editor's browser app only. It captures unhandled errors, and the
**Help & inspiration → Report a problem** button opens a feedback form whose reports go to
the same project. **What is not covered:** the Go server (API). It has no Sentry
integration; its observability path is OpenTelemetry (`OTEL_*`, see
[.env.example](.env.example)).

**Privacy:** events go to the DSN you configure and nowhere else. Nothing is sent to the
maintainers of Resotone. With no DSN set, the editor does not start Sentry and sends
nothing. Errors and feedback can contain page addresses and whatever an editor types into
the form, so treat the project like any other place that holds your editors' data.

The editor reads two environment variables **at runtime**, when the container starts, so
there is no code change and no rebuild:

| Variable | Default | What it does |
| --- | --- | --- |
| `NUXT_PUBLIC_SENTRY_DSN` | empty (off) | The DSN of your Sentry project. A DSN is not a secret, because it is delivered to every editor's browser. |
| `NUXT_PUBLIC_SENTRY_ENV` | `staging` | The `environment` tag on events. Set it to `production` on a production site. |

With the reference `compose.yml`, put them in `.env` (it passes both to the `studio`
service) and restart the editor:

```bash
NUXT_PUBLIC_SENTRY_DSN=https://<key>@<your-sentry-host>/<project-id>
NUXT_PUBLIC_SENTRY_ENV=production
```

```bash
docker compose up -d studio
```

Without compose, pass them to the editor image directly:

```bash
docker run -d -p 8080:3000 \
  -e NUXT_PUBLIC_SENTRY_DSN=https://<key>@<your-sentry-host>/<project-id> \
  -e NUXT_PUBLIC_SENTRY_ENV=production \
  ghcr.io/30blackbirds/resotone-studio:latest
```

(That command shows only the Sentry variables; the editor also needs its login settings
from [Configuration](#configuration) to be usable.)

**Using your own Sentry project:**

1. In Sentry, create a project with the platform **JavaScript → Nuxt** (the editor is a
   Nuxt single-page app; the browser SDK is `@sentry/vue`).
2. Open **Settings → Projects → your project → Client Keys (DSN)** and copy the DSN.
3. Set `NUXT_PUBLIC_SENTRY_DSN` as above and restart the editor.
4. Check that it works: open the editor, choose **Help & inspiration → Report a problem**
   and send a report. It appears under **User Feedback** in your project. To test error
   capture, run `setTimeout(() => { throw new Error('sentry test') })` in the browser
   console on the editor page; the event appears under **Issues**.

## Login and roles

Resotone works with **any standards-compliant OpenID Connect provider** — Keycloak,
Authentik, Zitadel, Authelia, Dex, BetterAuth's OIDC provider, or a central login of your
own — and does not depend on any one of them. The provider proves who someone is.
Resotone keeps its own record of that person and decides **what they may do**: it stores
users and assigns roles itself, and never sees or stores a password. The decision and the
alternatives are in [ADR 0001](docs/adr/0001-editor-login-with-standard-oidc.md).

1. Register the editor at your provider as a **confidential** client using the
   authorization code flow with PKCE, redirect URI `<EDITOR_URL>/auth/callback`. If the
   provider offers logout (`end_session_endpoint`), also register the post-logout
   redirect URI `<EDITOR_URL>/login`, and set `OIDC_REQUIRE_PROVIDER_LOGOUT=1` so that
   logout fails rather than ending only the editor's session if the provider ever
   stops advertising it.
2. Make the provider issue **JWT access tokens** with an audience
   (`RESONANS_JWT_AUDIENCE`, e.g. `resotone`) and, in multi-tenant mode, the tenant id.
3. Fill in the login block of [.env.example](.env.example).
4. [Make the first admin](#the-first-admin), who then assigns everyone else's role.

The editor's server is the OIDC client. It keeps the tokens and gives the browser only a
sealed `HttpOnly; SameSite=Strict` cookie, so a script that got into the editor page could
not read a token even if it tried.

### Users and roles

A user is a person who has logged in, identified by the provider's **issuer** and the
token's **`sub`**, within one tenant. Resotone creates the record at the person's first
successful login, with **no role**. Every request needs a role, and there is no default:
until an admin assigns one, the person gets `403`. A **disabled** user gets `403` on every
request, whatever role they hold.

| Role | Can |
| --- | --- |
| `viewer` | Read documents, versions, schema, assets and settings. |
| `editor` | + create and edit drafts, upload assets, preview. |
| `publisher` | + publish, delete and restore, roll back to an earlier version. |
| `admin` | + change tenant settings, and manage the tenant's users and roles. |

The four roles are Resotone's own; the provider does not define them. Roles are assigned
**inside Resotone**, by an admin of the same tenant through the API below. A person who
works in two tenants has one record, and one role, per tenant. An admin can never see or
change another tenant's users.

**Last-admin protection.** Demoting, disabling or erasing the last *active* admin of a
tenant is refused with `409`, so a tenant cannot lock itself out.

#### The first admin

Nobody can assign a role until an admin exists, and there is no open registration window
and no default role. The operator makes the first admin with a command that needs access
to the database:

```bash
docker compose run --rm api admin grant --issuer https://id.example.org --sub <their-sub> --tenant <tenant-uuid>
```

`api` is the compose service, which already carries the database and issuer settings.
`--sub` is the `sub` claim in that person's token. The user record is created if they
have not logged in yet. `--issuer` defaults to `RESONANS_JWT_ISSUER`, and `--tenant` to
`RESONANS_TENANT_ID` in single-tenant mode, so a one-organization install passes only
`--sub`. Running it again changes nothing and says so in its log line
(`userCreated: false, roleChanged: false`). It never re-enables a disabled user, and it
does not write the subject to the log. Use it again at any time, for instance to recover
a tenant whose admin has left.

#### Managing users (admin API)

All admin only, reads included, because they hold personal data. The tenant is always the
caller's own, taken from the verified token; see the OpenAPI reference at `/docs/v1/`.

| Route | Does |
| --- | --- |
| `GET /api/v1/users` | List the tenant's users with role and status. |
| `GET /api/v1/users/{id}` | One user. |
| `PUT /api/v1/users/{id}/role` | Assign `viewer`, `editor`, `publisher` or `admin`. |
| `POST /api/v1/users/{id}/disable`, `.../enable` | Block or allow login without losing the record. |
| `GET /api/v1/users/{id}/export` | The user's data as Resotone holds it, as a JSON download. |
| `DELETE /api/v1/users/{id}` | Erase the person's data ([see below](#personal-data)). |

Without a database (the in-memory development mode) these answer `501`.

#### The provider's role claim is optional

Earlier versions took the role only from the token. That still works, as an **opt-in**:
`RESONANS_ROLE_CLAIM` (default `resonans_role`) names the claim, and `RESONANS_ROLE_MAP`
maps values your provider already issues, such as groups, to roles, e.g.
`RESONANS_ROLE_MAP=cms-admins=admin,cms-writers=editor`. The claim may be a string or an
array; its highest role counts. **By default it is ignored.** Set
`RESONANS_ROLE_CLAIM_GRANTS=1` (exactly `1`) to let it count, and the rule is:

| Assigned in Resotone | Role claim | Effective role |
| --- | --- | --- |
| none | any | **none** (without the opt-in) |
| `editor` | `admin` | `editor` (without the opt-in the higher claim is ignored) |
| `editor` | `viewer` | `editor` |
| none | `editor` | `editor` (with the opt-in) |
| `editor` | `admin` | `admin` (with the opt-in) |
| `admin` | `viewer` | `admin` (with the opt-in: the higher of the two) |

So the assignment in Resotone is authoritative, and the claim can only ever **add** to it.
With the opt-in on, removing a role in Resotone does not remove access the provider still
grants; take it away at the provider too.

**Upgrading from a release that read the role only from the token:** until you assign
roles in Resotone, or set `RESONANS_ROLE_CLAIM_GRANTS=1`, every existing editor has no
role and gets `403`. Do one of the two before you upgrade; for the first, start with
[the first admin](#the-first-admin).

### Standalone: what Resotone asks of a provider

Verified against the code, not assumed:

- **Configured, not built in.** The issuer (`RESONANS_JWT_ISSUER`), the keys
  (`RESONANS_JWKS_URL`), the audience, and the names of the tenant and role claims are
  settings. Nothing in the server names a particular provider. The editor's own login
  discovers every endpoint from the issuer's discovery document.
- **JWT access tokens**, signed with RS256, ES256 or EdDSA, with the configured issuer
  (compared exactly, so a trailing slash matters) and audience. Opaque access tokens are
  refused; some providers need a resource indicator to issue a JWT (`OIDC_RESOURCE`).
- **A stable `sub`.** Together with the issuer it is the user's identity. If the
  provider changes someone's `sub`, they become a new user with no role.
- **Top-level claims only.** The tenant and role claims are read from the top level of
  the token, as a string (or, for the role, an array of strings). A claim nested in an
  object, such as Keycloak's `realm_access.roles`, is not read: use a mapper at the
  provider to put it at the top level, or assign roles in Resotone instead.
- **Name and email are optional.** They are read from the access token's `name` (or
  `preferred_username`) and `email` claims, and many providers put them only in the ID
  token. If absent the user is listed by `sub` alone. They never decide access.
- **Email is trusted from the provider, never verified here.** Resotone sends no email
  and has no SMTP. It stores an address only when the access token also says
  `email_verified` is true; without that claim no email is stored. A provider that has no
  email verification can be accommodated with `RESONANS_REQUIRE_EMAIL_VERIFIED=0`, and
  then the stored address is **unverified**: treat it as a label, not as proof that the
  person owns it. Roles never depend on it; they are assigned by issuer and `sub`.
- **Identity comes only from the verified access token.** The user store is asked about
  the issuer, `sub`, name, email and tenant in the access token the API verified, and
  nothing else. Headers, parameters or a body naming someone else change nothing.
- **Untested at other providers.** CI logs in through Dex and the authors run
  BetterAuth; other providers follow the standard but are not part of CI.
- **Defaults carry one name:** the default role claim is `resonans_role`. It is a
  default, overridable with `RESONANS_ROLE_CLAIM`.

### Logging in and out

- **After login** the editor takes you back to the page you started from. Only a page
  of the editor itself counts: a login link that names any other address takes you to
  the editor's start page instead.
- **Logging out ends your session at the provider too**, when the provider advertises an
  `end_session_endpoint`. The editor's server tells the provider which login to end, so
  the next login asks for your credentials again instead of signing you straight back
  in. The editor ends its own session only once the provider confirms. If it does not,
  you stay logged in and the editor says so, rather than showing a logout that did not
  happen.
- **A provider without an `end_session_endpoint`** (Dex, which the quickstart uses, is
  one) ends only the editor's session. Your session at the provider lasts until the
  provider ends it.
- **The post-logout redirect URI must be exactly `<EDITOR_URL>/login`**, registered at the
  provider alongside the login redirect URI. Providers compare it character for
  character, so `<EDITOR_URL>/` or `<EDITOR_URL>/login/` is a different URI.

Upgrading from before 3.2.2 to a provider with an `end_session_endpoint`: register the
post-logout redirect URI first. Until it is registered, a provider that checks it refuses
the logout, and editors stay logged in with a message saying so.

## Upgrading

> **Coming from a release before Resotone stored users?** Read step 4 first: without
> `resotone admin grant` or `RESONANS_ROLE_CLAIM_GRANTS=1`, every editor gets `403`.

1. Read [CHANGELOG.md](CHANGELOG.md) for **every** version between yours and the target.
   Steps you must take are listed there under **ACTION REQUIRED**.
2. Change `RESONANS_VERSION` to the new exact tag, then `docker compose pull` and
   `docker compose up -d --no-build` (or `git pull` and `up -d --build` when building from
   source). Database migrations run when the stack starts, in the `migrate` service.
3. **From a version that still used the roles `resonans_migrator` and `resonans_app`:** rename
   them once, as a superuser, before you start the new version. The schema, its tables and
   the default privileges follow the role, so nothing else changes:

   ```sql
   alter role resonans_migrator rename to resotone_migrator;
   alter role resonans_app rename to resotone_app;
   ```

   A rename clears a password that is stored as MD5, so set both passwords again with
   `alter role ... password '...'` if a login then fails. Point `DATABASE_URL` at the new
   names.
4. **ACTION REQUIRED when upgrading from a release before Resotone stored users.** Roles
   are now assigned inside Resotone, and the identity provider's role claim is ignored by
   default. Until you act, every editor, existing ones included, has no role and gets
   `403`. Before you start the new version do one of these:
   - run `docker compose run --rm api admin grant --sub <their-sub>` for your first admin
     ([the first admin](#the-first-admin)), who then assigns the others; or
   - set `RESONANS_ROLE_CLAIM_GRANTS=1` to keep taking roles from your provider's claim
     as before ([how the two combine](#the-providers-role-claim-is-optional)).
5. After migrating a database you manage yourself, run
   [deploy/assert-rls-coverage.sql](deploy/assert-rls-coverage.sql) again.
6. Take a [backup](#backups) first. Releases are not guaranteed to be reversible.

## Reference

Behaviour worth knowing before you build on it.

### Tenant isolation

Every document carries a `tenant_id` and a `dataset` (default `production`). Three things
have to hold, and each is checked in a different place:

1. **The tenant comes from a verified token.** The middleware reads the `org` claim of a
   JWT it has verified against the issuer's JWKS. It is never read from a request body or
   header — those are attacker-controlled.
2. **Postgres enforces it.** Every content table has `enable row level security` *and*
   `force row level security`, with policies on an `app.tenant_id` GUC that the
   application sets per transaction. The policies read it as
   `current_setting('app.tenant_id', false)` — not optional — so a query with the GUC
   unset **raises an error** rather than returning an empty result. That is deliberate:
   an empty result is a plausible answer a caller might act on, and an error is not.

   Tables live in the `resonans_app` schema, not `public`. The role that connects needs
   `usage` on that schema and default privileges granted *in* it; granting on `public`
   instead produces a role that cannot read its own tables.

   `force` binds the table owner too — but read [deploy/initdb/](deploy/initdb/) for what
   that does and does not buy you, because a superuser owner bypasses RLS regardless.
3. **Every tenant table actually carries the policy.** The first two hold once and stay
   true; this one is re-decided by every migration, and a migration that forgets is
   *invisible* to the role model — the role stays correct while the data reads across
   tenants. Run [deploy/assert-rls-coverage.sql](deploy/assert-rls-coverage.sql) after
   migrating; it exits non-zero and names the offending tables, and it fails rather than
   passing if it finds no tenant table at all.
4. **The application connects as a role that cannot bypass it.** RLS is ignored by any
   role with `SUPERUSER` or `BYPASSRLS`, which is why the shipped compose file creates two
   roles: a migrator that owns the schema, and the role the server actually uses. See
   [deploy/initdb/](deploy/initdb/). Collapsing them into one is the mistake this design
   exists to prevent. **In production the server checks this itself:** at boot it reads
   its own role from `pg_roles` and refuses to start if that role is `SUPERUSER` or
   `BYPASSRLS`, naming the role — and refuses too if it cannot read it. Outside
   production it starts with a `db_role_bypasses_rls` warning, because a local database
   is usually a single owner role.

   **If you write your own provisioning instead of using that file, read its comment on
   `alter default privileges` first.** Those grants attach to the role that *runs* them, so
   a human executing the same statements as a superuser grants for objects the superuser
   creates — and the app role ends up with nothing once the migrator creates the tables.
   Name the creating role with `for role`. This has already cost one deployment plan.

### Personal data

What Resotone stores about the people who log in, so you can tell them and answer for it.
You run it, so you decide purpose, legal basis and retention; this is the factual list.

| Field | Where | Source | Why |
| --- | --- | --- | --- |
| `sub` and `issuer` | `users` | the access token | Identify the user. |
| `display_name` | `users` | token claim `name`, else `preferred_username` (may be empty) | Show who is who to admins. |
| `email` | `users` | token claim `email`, only with `email_verified` true unless `RESONANS_REQUIRE_EMAIL_VERIFIED=0` (optional) | Show who is who to admins. |
| `status`, `created_at`, `last_login_at` | `users` | Resotone | Disable a user; the first login; the latest login, rewritten at most every 15 minutes. |
| `role`, `assigned_at` | `user_roles` | an admin, or `admin grant` | What the user may do. |
| `deleted_at` | `users` | Resotone | Marks an erased user. |

There are no passwords, credentials or tokens in these tables. Beyond them:

- **Content carries no author.** Documents, versions and assets record no user, so
  nothing in your content points back to a person.
- **Traces.** With `OTEL_ENABLED` on, the token's `sub` goes on each request's trace span
  as `enduser.id`, with the tenant id. It is sent to the collector you configure.
- **Logs.** Login refusals and admin actions are logged with the user's *id* and tenant,
  not their name, email or `sub`.
- **The editor's cookie** is sealed and holds the provider's tokens for that browser
  session ([login and roles](#login-and-roles)); it is not a copy of the user table.
- **Your provider** holds the account itself. Erasing a user here does not touch it.

**Disable, or erase.** *Disable* blocks login and keeps the record, so a person who is
away can come back with the same role. *Erase* (`DELETE /api/v1/users/{id}`)
pseudonymises: issuer, `sub`, name and email are replaced, the role is removed and the
user is disabled for good. The row stays, so anything that referenced the user by id keeps
resolving, but nothing identifies the person. If they log in again at your provider they
become a new user with no role. Erase does not reach backups you have already taken.

**Export.** `GET /api/v1/users/{id}/export` returns everything Resotone holds about one
user (the record above and their role) as a JSON file.

**Retention.** Resotone deletes nothing on a schedule. A user record stays until an admin
erases it, and last-login times are kept while the record is. Set your own policy and use
erase to carry it out.

### Content model

Content types are declarative, in `internal/schema/schemas/` — YAML, versioned in git.
Document types live in `types/*.yaml`, and they share one block library (`blocks.yaml`).
Each type's `body` field names an `allow` list, so a marketing page and a lesson draw
their blocks from the same library without offering each other's.

A document body is JSONB, validated against its schema on write: required fields, unknown
fields, the block allowlist, per-field shape, nested blocks and arrays. `title`, `slug`
and `status` are columns, not body fields.

Field types: `string · text · richtext · number · boolean · asset · reference · blocks ·
array`.

`GET /api/v1/schema` serves the registry so an editor can generate its own forms and
block palette from it.

### HTTP surfaces

**Editor API** — `/api/v1`, tenant from the verified token.

| Method | Route |
| --- | --- |
| `GET` | `/api/v1/schema` · `/api/v1/schema/{type}` |
| `GET` `PUT` | `/api/v1/settings` · `/api/v1/settings/preview` |
| `GET` | `/api/v1/documents?type=&status=&deleted=` · `/api/v1/documents/{id}` |
| `GET` | `/api/v1/content/{type}/{slug}` |
| `POST` `PUT` | `/api/v1/documents` · `/api/v1/documents/{id}` |
| `POST` | `/api/v1/documents/{id}/publish` |
| `DELETE` `POST` | `/api/v1/documents/{id}` · `/api/v1/documents/{id}/restore` |
| `GET` | `/api/v1/documents/{id}/versions` |
| `POST` | `/api/v1/documents/{id}/rollback/{version}` |
| `POST` `GET` | `/api/v1/assets?deleted=` · `GET /api/v1/assets/{id}` |
| `DELETE` `POST` | `/api/v1/assets/{id}` · `/api/v1/assets/{id}/restore` |
| `GET` | `/api/v1/video/embed?id=` · `/api/v1/video/resolve?id=` |
| `POST` | `/api/v1/preview-token` · `/api/v1/preview/envelope` |

**Delivery API** — `/delivery/v1`, published content only, for your own server-side
front-ends. The caller authenticates with `RESONANS_DELIVERY_TOKEN` and passes the tenant
in `X-Scope-OrgID`, which is trusted *only* because the token matched. Never call this
from a browser.

| Method | Route |
| --- | --- |
| `GET` | `/delivery/v1/schema` |
| `GET` | `/delivery/v1/{type}` · `/delivery/v1/{type}/{slug}` |
| `GET` | `/delivery/v1/by-id/{type}/{id}` |
| `GET` | `/delivery/v1/preview/{type}/{slug}` |

**API reference** — `/docs/v1/` renders the OpenAPI document, which is served at
`/docs/v1/openapi.yaml` and kept in [docs/api/openapi.yaml](docs/api/openapi.yaml). It lists
every route above. The delivery API's responses carry schemas, and a test checks the
handlers' responses against them; the editor API's response bodies are not described yet.

**Unauthenticated** — `/healthz`, `/readyz`, the API reference under `/docs/v1/`, the
visual-editing script at `/bridge/v1/resotone-bridge.js`, and `/_assets/{key}` when
Resotone serves asset bytes itself.

### Deleting content

`DELETE` moves a document or an asset to a bin: it disappears from every read path, and
`?deleted=only` lists what is in there. `POST …/restore` brings it back. Deleting twice,
or restoring something that is already live, is a `409` rather than a quiet no-op — so a
double click cannot overwrite the original deletion time.

A deleted item answers `409`, not `404`. "In the bin" and "never existed" are different
answers, and an editor who gets a 404 goes looking for something that is one click from
returning. Across tenants it *is* a `404`, because a 409 would confirm that the id exists.

**Documents:** the slug is freed while a document sits in the bin, because a full unique
index would let a deleted page hold its own address hostage forever. The cost is named:
restoring into a slug someone has since taken fails with a conflict rather than silently
renaming anything.

**Assets:** the stored **bytes are not touched**. That is what makes the undo real — a bin
that had already discarded the file would be a delete with extra steps. Three consequences
are deliberate rather than missing: nothing is retracted from a CDN, there is no hard
delete for an actual erasure request, and **deletion does not check whether a document
references the asset**. On that last one, the cheap version would be a trap: an asset
reference is a bare id inside a JSONB body, so `body::text like '%<uuid>%'` is an
unindexed full scan that can also match a coincidence — and a plausible-but-wrong
reference count is worse than none, because an editor would act on it.

### Asset storage

| Backend | Selected by | Bytes served by | Survives a restart |
| --- | --- | --- | --- |
| `bunny` | `BUNNY_STORAGE_ENDPOINT` + `_ZONE` + `_KEY`, all three | Bunny CDN pull-zone | yes |
| `disk` | `RESONANS_STORAGE_DIR` | Resotone, at `GET /_assets/<key>` | yes, with a volume |
| `memory` | neither of the above | Resotone, at `GET /_assets/<key>` | **no** |

Bunny wins when both are configured: a CDN in front beats an origin shipping image bytes.
`BUNNY_CDN_BASE` defaults to `PUBLIC_URL` + `/_assets`, so leave it empty unless you
actually use Bunny.

**Asset URLs are for the reader's browser.** Every asset URL in a delivery response is
built from `BUNNY_CDN_BASE`, which is fixed at startup, so one address goes into every page
a site renders. Set `PUBLIC_URL` to the address readers reach Resotone at, never to a
container or internal hostname. A consumer that fetches asset bytes itself, such as a
server that resizes images inside the same network as Resotone, should not rely on that
public address resolving there. When Resotone serves the bytes (`disk` or `memory`, with
the default `BUNNY_CDN_BASE`), such a consumer replaces the `PUBLIC_URL` part of each URL
with the address it already uses to reach Resotone, and keeps the rest of the URL as it
is. With `bunny` the URLs point at the CDN, which a server fetches the same way a browser
does.

Uploads are typed from their **content**, never from the filename or the client's
`Content-Type`, against a short allowlist (PNG, JPEG, GIF, WebP, PDF, ZIP, MP3). No
`text/*` type is accepted, because an SVG or HTML file carrying a script sniffs as
`text/plain` and would then execute on the origin serving it. `/_assets` re-derives the
type from the bytes with the same allowlist on the way out, so a file that reached the
volume by some other route still cannot be served. The disk backend confines itself with
`os.Root`, so the root is enforced by the kernel rather than by string checks.

Video is an optional plugin (`RESONANS_VIDEO_PROVIDER=vimeo`) resolving embeds and oEmbed
metadata over public endpoints, with no token.

## Development

| Action | Command |
| --- | --- |
| Build | `go build ./...` |
| Test | `go test -race ./...` |
| Vet | `go vet ./...` |
| Lint | `golangci-lint run` + `gosec ./...` |
| Format | `gofumpt -w .` |

`golangci-lint` does not include `gosec`, so running only the former is a green about a
different set of tools than CI's.

With [Tilt](https://tilt.dev), `tilt up` runs the same stack as [Try it](#try-it), built
from your checkout. If ports 8080 and 3000 are taken, use
`STUDIO_PORT=3010 API_PORT=3015 tilt up`. `tilt up -- --dev` runs the development
stack instead: no login, the editor as a live-reloading dev server, and the examples.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before you spend an evening on a patch: it covers
setup, conventional commits, and the contributor licence agreement that outside
contributions need before they can be merged. Everyone taking part is expected to follow
the [Code of Conduct](CODE_OF_CONDUCT.md). Questions and help: [SUPPORT.md](SUPPORT.md).

## Security

Please do not open a public issue for a vulnerability. Use GitHub's
[private vulnerability reporting](https://github.com/30Blackbirds/resotone/security/advisories/new);
[SECURITY.md](SECURITY.md) says what counts and what happens next.

## Copyright and license

Copyright (C) 2026 The Resonans Authors. Published by **30blackbirds**, the trading name of
Asp Productions ApS (CVR 36708077, Denmark). Licensed under the
[GNU AGPL-3.0](LICENSE) — [NOTICE](NOTICE) says why the holder is the authors and not the
company.

If you modify Resotone and offer it to users over a network, §13 says those users are
entitled to your source. Set `RESONANS_SOURCE_URL` to your repository and `/healthz` will
tell them where to find it — leaving it at the upstream default is only truthful if you
changed nothing. It must be an `http(s)` URL; anything else is refused at startup.

That field is **informative**: it is a string for a human to read, and tooling should not
fetch it automatically. A scanner that followed it would be making this instance issue
requests to an address its operator chose.

If the AGPL does not fit what you want to build, ask us — but note that a different licence
needs the agreement of everyone who holds copyright in the part you want, and that is the
authors rather than the company. See [NOTICE](NOTICE).

## Links

- [CHANGELOG.md](CHANGELOG.md) · [SUPPORT.md](SUPPORT.md) · [SECURITY.md](SECURITY.md)
- [ADR 0001: editor login with standard OIDC](docs/adr/0001-editor-login-with-standard-oidc.md)
- [OpenAPI document](docs/api/openapi.yaml)
- [Releases](https://github.com/30Blackbirds/resotone/releases)
