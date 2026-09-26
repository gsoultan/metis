package entities

import "github.com/google/uuid"

// RolesIn returns the roles the account acts with in an organization: its
// global roles, and the ones it holds in that organization alone.
//
// uuid.Nil is no organization — a request that is for none — and gets the
// global roles only, as does an organization the account holds nothing in.
// Neither case allocates, and nor does an account whose roles are all of one
// kind; the list is only built when there is something on both sides.
func (u User) RolesIn(organizationID uuid.UUID) []string {
	here := u.rolesOnlyIn(organizationID)
	switch {
	case len(here) == 0:
		return u.Roles
	case len(u.Roles) == 0:
		return here
	}
	roles := make([]string, 0, len(u.Roles)+len(here))
	roles = append(roles, u.Roles...)
	return append(roles, here...)
}

// HoldsRoleIn reports whether the account holds role in an organization:
// globally, or there. Compared as HasRole compares, whatever the case.
func (u User) HoldsRoleIn(organizationID uuid.UUID, role string) bool {
	return HasRole(u.Roles, role) || HasRole(u.rolesOnlyIn(organizationID), role)
}

// HoldsRoleGlobally reports whether the account holds role in every
// organization it belongs to — the only roles that count for what no one
// organization owns.
func (u User) HoldsRoleGlobally(role string) bool {
	return HasRole(u.Roles, role)
}

// rolesOnlyIn is what the account holds in one organization alone. Nothing for
// no organization, whatever a map built by hand might hold under the nil id.
func (u User) rolesOnlyIn(organizationID uuid.UUID) []string {
	if organizationID == uuid.Nil {
		return nil
	}
	return u.RolesByOrganization[organizationID]
}
