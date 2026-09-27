package contracts

import (
	"context"

	"github.com/google/uuid"
)

// OrganizationRoleGrants is how an organization's administrators decide what an
// account may do in their organization — and how the interface learns whether
// the person looking may also change what an account holds in every
// organization.
type OrganizationRoleGrants interface {
	// SetOrganizationRoles replaces the roles an account holds in the
	// organization the request is for. The account has to be a member of it,
	// the roles have to be ones the installation has, and the organization's
	// last administrator cannot be taken away. A request for no organization is
	// refused: there is nowhere to hold the roles.
	SetOrganizationRoles(ctx context.Context, userID uuid.UUID, roles []string) error

	// MayChangeGlobalRoles reports whether the signed-in caller may grant and
	// take away the roles an account holds in every organization: an
	// administrator of every organization whom the platform gate admits.
	MayChangeGlobalRoles(ctx context.Context) (bool, error)
}
