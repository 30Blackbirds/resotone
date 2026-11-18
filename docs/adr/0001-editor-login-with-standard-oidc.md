# ADR 0001 — The editor logs in with standard OpenID Connect

- **Status:** Implemented in v2.0.0. Not yet proven against a provider other than BetterAuth,
  and not yet running in the authors' production.
- **Date:** 2026-10-22
- **Amended 2026-11-18:** §5 below no longer holds. Resotone now stores its own users and
  assigns roles itself; the provider's role claim is an optional input (see the amendment
  at §5). Everything else here stands.
- **Decided by:** Allan (direction, 2026-10-22); this record written by [resotone]
- **Scope:** Resotone's editor UI (the code in `studio/`) and the editor API (`/api/v1`).
  The delivery API, preview tokens and `RESONANS_DEV_AUTH` are unchanged.

## Context

Resotone is open source under AGPL-3.0 and is meant to be run by anyone, on their own
infrastructure. The editor API is already close to that. The editor UI is not.

Measured on `main` `f2114a9`:

- **The API is provider-neutral.** `internal/auth` verifies a bearer JWT against any
  issuer's JWKS (`RESONANS_JWT_ISSUER`, `RESONANS_JWKS_URL`) and reads the tenant from a
  claim named `org`, hard-coded (`internal/auth/jwt.go`, `orgClaim`).
- **The editor UI is tied to one product.** `studio/app/composables/useAuth.ts` calls
  BetterAuth's own endpoints — `/api/auth/sign-in/email`, `/api/auth/get-session`,
  `/api/auth/token` — with an e-mail and a password typed into Resotone's own login page.
  Someone running Keycloak, Authentik, Zitadel or any other identity provider cannot log in
  to the editor at all.
- **There are no roles.** The actor carries `Sub` and `Tenant` only. Every holder of a
  valid token for a tenant can do everything, including `PUT /api/v1/settings` and
  `PUT /api/v1/settings/preview` (open item: ClickUp `869fbfz73`).
- **The audience is never checked in production.** `VerifierConfig.Audience` exists and is
  asserted when set, but no configuration sets it: outside tests, `Audience` occurs only in
  `internal/auth/jwt.go`. Resotone therefore accepts any valid token from its issuer,
  whichever service it was minted for. Whether a token minted for another 30blackbirds
  service actually carries an `org` claim that would pass here is UNMEASURED.
- **30blackbirds' own identity service is already a standard OIDC provider.** The `auth`
  service runs `@better-auth/oauth-provider` (authorization code + PKCE, discovery at
  `/api/auth/.well-known/openid-configuration`, RS256 via JWKS), measured on `auth`
  `main` `b3bf711`. What it does NOT do today: put `org` or a role into the tokens that
  plugin issues. `org` is added only by the `jwt()` plugin's `definePayload`, which serves
  the `/api/auth/token` path the editor uses now.

The README then promised that Resotone "does not manage users or send email. It verifies
JWTs from an identity provider you already run." The editor UI broke that promise.

## Decision

### 1. The editor logs in with OIDC authorization code + PKCE, against any provider

The editor UI speaks only standard OpenID Connect. No provider-specific endpoint, SDK or
login form remains in Resotone.

- **Flow:** authorization code with PKCE (`S256` only; `plain` is refused). `state` and
  `nonce` are required and checked. The `iss` parameter in the authorization response
  (RFC 9207) is checked when the provider sends it.
- **Discovery:** configured by issuer URL only; endpoints and JWKS come from
  `/.well-known/openid-configuration`. Resotone refuses to start if discovery fails or the
  discovered `issuer` differs from the configured one.
- **Where the tokens live: a backend-for-frontend.** The editor UI already has a server
  (Nitro, `studio/server/`). It becomes the OIDC client: a *confidential* client that does
  the code exchange, keeps access and refresh tokens server-side, and gives the browser
  only an `HttpOnly; Secure; SameSite=Strict` session cookie. Editor API calls go through
  it and get the bearer added there.
  - Why not a public client in the browser: a CMS editor renders user-authored HTML, and
    Resotone has a stored-XSS history (resotone#52). A token that no script can read is
    the one an XSS cannot steal. This follows the IETF best current practice for
    browser-based apps.
  - The current "JWT in memory only" rule is kept in spirit and strengthened: the browser
    holds no token at all.
  - **As implemented:** the session is a sealed (encrypted + MACed) cookie, not a server-side
    store, so the editor stays stateless and needs no table. It holds the access and refresh
    tokens and a small user object, never the ID token. Its sealed size is measured on every
    write and refused above 3 800 bytes with a message naming the cause, instead of the
    browser silently dropping a cookie over its ~4 kB limit.
- **Logout:** when the provider advertises an `end_session_endpoint`, the editor's
  server calls it with the ID token as `id_token_hint`, and ends the local session only
  when the provider confirms (a redirect to, or JSON naming, the registered
  `<EDITOR_URL>/login`). Otherwise the editor stays logged in and says so. Without an
  `end_session_endpoint` only the local session ends, unless
  `NUXT_OIDC_REQUIRE_PROVIDER_LOGOUT=1` (`OIDC_REQUIRE_PROVIDER_LOGOUT` in compose): then
  that is an error too and the editor stays logged in. A deployment whose provider offers
  logout sets it, so a discovery document that loses the endpoint cannot turn logout into
  a local one unnoticed.

### 2. The API checks audience, and tenant and role come from configurable claims

The editor API keeps verifying JWTs locally against the issuer's JWKS, with four changes:

| Setting | Default | Meaning |
| --- | --- | --- |
| `RESONANS_JWT_AUDIENCE` | *(required)* | The `aud` a token must carry. Startup is refused without it once JWT auth is configured — closes the gap above. |
| `RESONANS_TENANT_CLAIM` | `org` | Claim holding the tenant id (multi-tenant mode). |
| `RESONANS_ROLE_CLAIM` | `resonans_role` | Claim holding the role. A string or an array of strings. |
| `RESONANS_ROLE_MAP` | *(empty)* | Optional `provider-value=role` pairs, e.g. `cms-admins=admin,cms-writers=editor`, so a Keycloak group or a BetterAuth organization role can be used without changing the provider. |

Signature algorithm is pinned to an allowlist (`RS256`, `ES256`, `EdDSA`); `none` and HMAC
algorithms are refused.

### 3. Four built-in roles, enforced in Resotone, fail closed

| Role | Can |
| --- | --- |
| `viewer` | Read documents, versions, schema, assets and settings. |
| `editor` | `viewer`, plus create and update documents, upload assets, request a preview token. |
| `publisher` | `editor`, plus publish, delete and restore documents and assets, roll back versions. |
| `admin` | `publisher`, plus `PUT /api/v1/settings` and `PUT /api/v1/settings/preview`. |

- A token with no recognised role is refused with `403`, never given a default role. A
  missing claim is a configuration error to fix, not a reason to open up.
- Roles are checked in one middleware against a route table, so a new route without an
  entry fails a test instead of being reachable by everyone.
- The check sits behind an `Authorizer` interface. The built-in role model is the only
  implementation now. An external decision point (30blackbirds' PDP / OpenFGA) can be added
  later as a second implementation without changing handlers. Self-hosters never need it.

### 4. A single-tenant mode

`RESONANS_TENANT_MODE=single` with `RESONANS_TENANT_ID=<uuid>` serves exactly one tenant.
The tenant claim is not read. This is the normal case for an organisation hosting its own
CMS, and it means their provider needs only a role claim. Row level security stays on in
both modes: single-tenant is one tenant id, not RLS turned off.

`RESONANS_TENANT_MODE=multi` (default, today's behaviour) requires the tenant claim and
refuses a token without it.

### 5. ~~No user database of our own~~ — superseded 2026-11-18

*Original decision (2026-10-22):* Resotone stores no users, passwords, sessions beyond the
editor's own cookie, or e-mail addresses. It keeps the token's `sub` where it records who
did something. Accounts, password reset, MFA and SSO belong to the provider.

**Amendment (Allan, 2026-11-18).** Resotone must run standalone with any standards-compliant
provider, and a central login of the authors' own is only one such provider. So:

- Resotone **stores users**, keyed by (issuer, `sub`) within a tenant, created at the first
  successful login with no role. Fields: `sub`, issuer, display name, optional email, status
  (active or disabled), created and last-login times. **Still no passwords or credentials**,
  and still no e-mail sent. Accounts, password reset, MFA and SSO still belong to the provider.
- **Roles live inside Resotone.** Each user has one role per tenant, assigned by an admin of
  that tenant or by the operator's `admin grant` command. The four roles stay Resotone's own
  definitions. A disabled user, or one with no role, gets 403.
- The provider's role claim (`RESONANS_ROLE_CLAIM`, `RESONANS_ROLE_MAP`) is **optional and
  off by default**. With `RESONANS_ROLE_CLAIM_GRANTS=1` it can raise a role above the one
  assigned in Resotone, never lower it. The safest rule won: the local assignment is
  authoritative.
- There is **no default role and no open registration**: the first admin comes from the
  operator, with database access.
- Personal data, erasure and export are described in the README ([personal data](../../README.md#personal-data)).

### 6. The authors keep BetterAuth as their OIDC provider

30blackbirds' own deployment uses the same code path as everyone else. The `auth` service
registers Resotone's editor as a confidential OIDC client in `oauthProvider`, and adds `org`
and a role to the tokens it issues for that client. The role can come from the existing
BetterAuth organization membership (`owner`, `admin`, `member`) through
`RESONANS_ROLE_MAP`, with `RESONANS_ROLE_CLAIM_GRANTS=1` (§5 amendment), or be assigned inside
Resotone instead.

The post-logout redirect URI is registered there through the `auth` service's
`RESONANS_OIDC_POST_LOGOUT_REDIRECT_URIS`, which for production must be exactly
`https://cms.30blackbirds.dk/login`. The service's development default is
`http://localhost:3000/`, a different path, so for a local editor that variable has to
name `<EDITOR_URL>/login` as well.

## Consequences

**Good**

- Anyone can run Resotone with the identity provider they already have, which is what
  "self-hosted, AGPL" promises.
- Editors' tokens are out of reach of scripts in the editor.
- The missing audience check is closed, and per-route roles replace "a valid token can do
  everything". `869fbfz73` is resolved by this.
- 30blackbirds dogfoods the same path outside users take, so its bugs reach us first.

**Costs**

- The editor UI's server needs a session secret (`NUXT_OIDC_SESSION_SECRET`). Rotating it
  logs every editor out. A provider that issues very large access tokens can exceed the
  cookie budget; that is refused loudly, and the cure is fewer claims for this client.
- Editor API calls gain a hop through the editor's server.
- Every deployment must configure an OIDC client. There is no "just log in" without a
  provider; the README's quickstart needs a tested example (Keycloak or Authentik in
  `compose.yml` under a profile).
- 30blackbirds needs work in `auth` before cutover (claims for the Resotone client).

## Rejected alternatives

- **A built-in login with passwords.** Passwords, reset e-mails, MFA, lockout and
  breach handling would all become Resotone's problem, contradicting "Postgres plus a
  binary". Every provider worth running already does this. (This stays rejected; the §5
  amendment adds user *records and roles*, not credentials.)
- **Keep BetterAuth-specific login and document it as a requirement.** Forces every
  self-hoster to run our identity stack; that is lock-in, the thing Resotone exists to
  avoid.
- **Public OIDC client in the browser (tokens in memory).** Simpler, no session store, but
  a successful XSS can use or steal the token. Rejected for the reason in §1.
- **A default role for tokens without one.** Convenient for first setup, and exactly the
  "permissive by default for developer experience" guard CONTRIBUTING.md says we refuse.

## Rollout

1. `RESONANS_JWT_AUDIENCE` added and required (API only; independent of everything else —
   can ship first).
2. Configurable claims, the role middleware and single-tenant mode in the API.
3. OIDC client and session in the editor UI's server; login page replaced by a redirect.
4. `auth`: register the client and add `org` + role claims for it ([auth]'s change).
5. 30blackbirds cutover in production. (The BetterAuth-specific calls in `useAuth.ts` were
   removed in the same change as step 3: steps 1–2 already require a role claim, so no
   30blackbirds editor could have kept working through the old path anyway.)
6. README quickstart with a provider example, measured from a clean checkout.

Each step ships on its own. Until step 5, 30blackbirds keeps today's login.

## Open questions (UNMEASURED)

- Whether `@better-auth/oauth-provider` access tokens are JWTs Resotone can verify
  locally, and which `iss` they carry (`https://auth.30blackbirds.dk` or `…/api/auth`). If
  they are opaque, step 4 needs token introspection or a JWT-format option.
- Whether a token minted for another 30blackbirds service passes Resotone today (the
  audience gap above). To be measured by [sikkerhed] before step 1 ships, not after.
- Tenant ids that are not UUIDs (some providers use slugs). Out of scope here;
  `RESONANS_ROLE_MAP` does not cover tenants.
