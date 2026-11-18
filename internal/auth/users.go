package auth

import (
	"context"

	"github.com/google/uuid"
)

// Identity is who a verified token says the caller is, in which tenant.
type Identity struct {
	Tenant      uuid.UUID
	Issuer      string
	Sub         string
	DisplayName string
	Email       string
}

// UserRecord is what Resotone itself knows about a user in a tenant.
type UserRecord struct {
	ID       uuid.UUID
	Disabled bool
	// Role is the locally assigned role; RoleNone when none was assigned.
	Role Role
}

// UserStore is Resotone's own user table. Login finds the user for the identity
// or creates it (just in time, at the first successful login) with NO role, and
// records the login. It never assigns a role.
type UserStore interface {
	Login(ctx context.Context, id Identity) (UserRecord, error)
}

// ResolveRole is the one place the effective role is decided.
//
// The role Resotone itself assigned (local) is authoritative. The identity
// provider's role claim (claim: RESONANS_ROLE_CLAIM / RESONANS_ROLE_MAP) is an
// optional input and is ignored unless the operator opted in with
// RESONANS_ROLE_CLAIM_GRANTS=1 (claimGrants). With the opt-in the claim can only
// ADD: the result is the higher of the two, so a claim never removes access that
// was assigned here, and never lowers it.
//
//	local   claim   claimGrants  effective
//	none    any     false        none      (a new user has no role until assigned)
//	editor  admin   false        editor    (a higher claim is ignored)
//	editor  viewer  false        editor
//	editor  admin   true         admin
//	editor  viewer  true         editor    (local wins when it is higher)
//	none    editor  true         editor
func ResolveRole(local, claim Role, claimGrants bool) Role {
	if claimGrants && claim > local {
		return claim
	}
	return local
}
