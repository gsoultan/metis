package impl

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/internal/pkg/platformadmins"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
)

// SetOrganizationRoles replaces the roles an account holds in the organization
// the request is for.
//
// Who may is the endpoint gate's question: an administrator there, which
// counts the administrator role held in every organization and the one held
// in this organization alone. This keeps the grant inside the organization.
// The account has to be a member of it — anybody else reads as not found, as
// on every read of the directory — and nothing about the account outside the
// organization changes, so an account another organization shares needs no
// authority there.
func (s *userService) SetOrganizationRoles(ctx context.Context, userID uuid.UUID, roles []string) error {
	organization := entities.ActingOrganization(ctx)
	if organization == uuid.Nil {
		return apierr.Invalidf("roles in an organization are granted in a request for that organization")
	}
	roles, err := organizationRoles(roles)
	if err != nil {
		return err
	}
	stored, err := s.repo.User().Get(ctx, userID)
	if err != nil {
		return err
	}
	if err := requireAccountVisible(ctx, stored); err != nil {
		return err
	}
	if losesAdministratorIn(stored, organization, roles) {
		if err := s.requireAnotherAdministrator(ctx, stored, []uuid.UUID{organization}); err != nil {
			return err
		}
	}
	if err := s.repo.User().SetOrganizationRoles(ctx, userID, organization, roles); err != nil {
		return err
	}
	// What the account may do is read from it on every request, through a
	// cache that would otherwise keep the roles it had for its lifetime.
	s.principals.forget(userID)
	// A change that changes nothing is not logged as one, here as in an
	// update of the account: the same roles, in whatever order or case.
	before := rolesOf(stored)
	if sameRoles(before.byOrganization[organization], roles) {
		return nil
	}
	traceAccountChange(ctx, accountOrganizationRolesChanged, userID, stored.Username, before, before.holdingIn(organization, roles))
	return nil
}

// rolesForNewMember is what a new account holds in the organization the request
// is for: the roles its body names there, which it can only hold by being
// created in that organization. Anything else the account arrived with is
// dropped — only the roles named for this organization are the caller's to
// grant.
func rolesForNewMember(ctx context.Context, u entities.User) (map[uuid.UUID][]string, error) {
	if len(u.OrganizationRoles) == 0 {
		return nil, nil
	}
	organization := entities.ActingOrganization(ctx)
	if organization == uuid.Nil || !slices.ContainsFunc(u.Organizations, func(org *entities.Organization) bool {
		return org != nil && org.ID == organization
	}) {
		return nil, apierr.Invalidf("roles in an organization are granted to an account created in the organization " +
			"the request is for")
	}
	roles, err := organizationRoles(u.OrganizationRoles)
	if err != nil {
		return nil, err
	}
	return map[uuid.UUID][]string{organization: roles}, nil
}

// MayChangeGlobalRoles reports whether the caller may change what an account
// holds in every organization: the administrator role held in every
// organization, and the platform gate's admission — which on an installation
// of several organizations is the operator naming them.
func (s *userService) MayChangeGlobalRoles(ctx context.Context) (bool, error) {
	caller := signedIn(ctx)
	if caller == nil || !caller.HoldsRoleGlobally(entities.RoleAdmin) {
		return false, nil
	}
	return s.platform.Admits(ctx, caller.ID)
}

// requireGlobalRoleAuthority refuses a change to the roles an account holds in
// every organization — granting one, taking one away, or deleting an account
// that holds one — from anybody but a platform administrator.
//
// Such a role acts in every organization the account belongs to, so the
// administrator of one of them granting it was granting it in all the others.
// The endpoint gate admits an administrator of the organization the request is
// for; this is what keeps that from reaching past it.
//
// A request from nobody at all is the one case let through: setup seeding the
// first administrator, or a maintenance command, which no request is — the
// auth chain refuses a request that carries nobody before it reaches here.
// Somebody who is not an account is refused, as nobody the gate can name.
func (s *userService) requireGlobalRoleAuthority(ctx context.Context) error {
	if ctx.Value(pkgauth.UserContextKey) == nil {
		return nil
	}
	may, err := s.MayChangeGlobalRoles(ctx)
	if err != nil {
		return err
	}
	if may {
		return nil
	}
	return globalRoleRefusal(signedIn(ctx))
}

// globalRoleRefusal says who may change a role held in every organization, and
// what would make the caller one.
func globalRoleRefusal(caller *entities.User) error {
	if caller == nil || !caller.HoldsRoleGlobally(entities.RoleAdmin) {
		return apierr.Forbiddenf("a role held in every organization is changed only by a platform administrator: an "+
			"administrator of every organization whom whoever operates the installation names in %s. That covers "+
			"granting one, taking one away and deleting an account that holds one; you can grant roles in this "+
			"organization instead", platformadmins.Env)
	}
	return apierr.Forbiddenf("a role held in every organization acts in all of them, so where there is more than one "+
		"organization it is changed only by a platform administrator: granting one, taking one away or deleting an "+
		"account that holds one. Whoever operates the installation makes you one by adding your account id, %s, to "+
		"%s; you can grant roles in this organization instead", caller.ID, platformadmins.Env)
}

// organizationRoles is the roles to hold in an organization, as the
// installation spells them, each once.
//
// Only the roles the installation has. A global role has never been checked
// against them, and an existing account may hold a word no gate reads; a new
// grant in an organization is not given that latitude, because a typo there
// would read as a grant and admit to nothing.
func organizationRoles(requested []string) ([]string, error) {
	roles := make([]string, 0, len(requested))
	for _, role := range requested {
		name, known := builtInRole(role)
		if !known {
			return nil, apierr.Invalidf("%q is not a role here; a role in an organization is one of %s",
				role, strings.Join(builtInRoleNames(), ", "))
		}
		if !slices.Contains(roles, name) {
			roles = append(roles, name)
		}
	}
	return roles, nil
}

// builtInRole is the installation's own spelling of role, compared as every
// role check compares.
func builtInRole(role string) (string, bool) {
	role = strings.TrimSpace(role)
	for _, name := range builtInRoleNames() {
		if entities.HasRole([]string{name}, role) {
			return name, true
		}
	}
	return "", false
}

func builtInRoleNames() []string {
	builtIn := entities.BuiltInPlatformRoles()
	names := make([]string, 0, len(builtIn))
	for _, role := range builtIn {
		names = append(names, role.Name)
	}
	return names
}

// holdsBuiltInRole reports whether roles include one the installation's gates
// read. A legacy word no gate reads grants nothing, so an account holding only
// that holds nothing in every organization worth the platform's decision.
func holdsBuiltInRole(roles []string) bool {
	for _, role := range roles {
		if _, known := builtInRole(role); known {
			return true
		}
	}
	return false
}

// sameRoles reports whether two lists hold the same roles, in any order and
// letter case.
func sameRoles(a, b []string) bool {
	for _, role := range a {
		if !entities.HasRole(b, role) {
			return false
		}
	}
	for _, role := range b {
		if !entities.HasRole(a, role) {
			return false
		}
	}
	return true
}

// losesAdministratorIn reports whether replacing an account's roles in one
// organization takes away the last administrator role it holds there. An
// account holding the role in every organization keeps it here whatever it
// holds here alone.
func losesAdministratorIn(account models.UserModel, organization uuid.UUID, roles []string) bool {
	return entities.HasRole(account.RolesByOrganization[models.UUID(organization)], entities.RoleAdmin) &&
		!entities.HasRole(roles, entities.RoleAdmin) &&
		!entities.HasRole(account.Roles, entities.RoleAdmin)
}

// administeredOrganizations is every organization an account administers: all
// of them, with the role held in every organization, and otherwise the ones it
// holds it in alone.
func administeredOrganizations(account models.UserModel) []uuid.UUID {
	global := entities.HasRole(account.Roles, entities.RoleAdmin)
	var organizations []uuid.UUID
	for _, org := range account.Organizations {
		if global || entities.HasRole(account.RolesByOrganization[org.ID], entities.RoleAdmin) {
			organizations = append(organizations, uuid.UUID(org.ID))
		}
	}
	return organizations
}

// administeredOnlyThroughGlobalRole is every organization an account
// administers through its global role alone — the ones that stop being
// administered by it when that role goes.
func administeredOnlyThroughGlobalRole(account models.UserModel) []uuid.UUID {
	var organizations []uuid.UUID
	for _, org := range account.Organizations {
		if !entities.HasRole(account.RolesByOrganization[org.ID], entities.RoleAdmin) {
			organizations = append(organizations, uuid.UUID(org.ID))
		}
	}
	return organizations
}

// withRolesHere is an account as a request for organization shows it: with the
// roles it holds there alone. The rest of RolesByOrganization stays unwritten.
func withRolesHere(user entities.User, organization uuid.UUID) entities.User {
	if organization != uuid.Nil {
		user.OrganizationRoles = user.RolesByOrganization[organization]
	}
	return user
}

// organizationCount counts the installation's organizations for the platform
// gate, unscoped: whether an installation is shared has one answer whoever
// asks.
type organizationCount struct {
	repo repositories.Repository
}

func (c organizationCount) CountOrganizations(ctx context.Context) (int64, error) {
	return c.repo.Organization().Count(ctx)
}
