package impl

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/tenantscope"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// errNoSuchAccount is the answer for an account outside the caller's
// organization — the same as for one that does not exist, so an ID is not an
// oracle for which accounts other organizations have.
var errNoSuchAccount = fmt.Errorf("%w: no such user", apierr.ErrNotFound)

// requireAccountVisible refuses an account the caller's organization does not
// hold.
//
// Accounts are installation-wide in the repository, and have to be: one is
// read by ID while a token is validated, before there is a tenant to scope by.
// So the boundary is drawn here, where the request's organization is known.
// Without a tenant it answers the way every scoped read does: system work sees
// everything, anything else nothing once the strict scope is on.
func requireAccountVisible(ctx context.Context, account models.UserModel) error {
	tenant, ok := entities.TenantContextFrom(ctx)
	if !ok || tenant.TenantID == "" {
		if tenantscope.Allowed(ctx) {
			return nil
		}
		return errNoSuchAccount
	}
	for _, org := range account.Organizations {
		if uuid.UUID(org.ID).String() == tenant.TenantID {
			return nil
		}
	}
	return errNoSuchAccount
}

// belongsTo reports whether an account is a member of the organization.
func belongsTo(account models.UserModel, organizationID uuid.UUID) bool {
	for _, org := range account.Organizations {
		if uuid.UUID(org.ID) == organizationID {
			return true
		}
	}
	return false
}

// requireAccountAuthority refuses a change to an account that belongs to an
// organization the caller does not administer.
//
// The account itself — its names, its global roles, whether it exists — is
// the same in every organization it belongs to, so changing it acts in all of
// them. Being an administrator grants authority over your own organizations,
// not over the others an account happens to share. The roles it holds in one
// organization alone are that organization's to change, and do not come
// through here.
//
// Administering, not belonging. While every role was global, a member of an
// organization who was an administrator anywhere was one there too, and asking
// for membership was enough. With a role held in one organization it is not:
// an administrator of one organization, and an ordinary member of another,
// could rename or delete the other's administrator.
//
// The refusal does not name the other organization. The caller may not be a
// member — its name is not theirs to read — and the account's memberships
// carry ids only. The person the account belongs to can say where else they
// are.
func requireAccountAuthority(ctx context.Context, account models.UserModel) error {
	if err := requireAccountVisible(ctx, account); err != nil {
		return err
	}
	caller := signedIn(ctx)
	if caller == nil {
		return nil
	}
	memberships, _ := callerOrganizations(ctx)
	for _, org := range account.Organizations {
		organization := uuid.UUID(org.ID)
		if !memberships[organization] || !caller.HoldsRoleIn(organization, entities.RoleAdmin) {
			return apierr.Forbiddenf(
				"%s also belongs to an organization you do not administer; "+
					"an administrator there has to make this change",
				account.Username)
		}
	}
	return nil
}

// requireAnotherAdministrator refuses to take the last administrator away from
// any of the organizations given — the ones the change stops the account
// administering.
//
// There is no default account by design, so an organization left with nobody
// who can administer it cannot be administered through Metis again. Somebody
// administers an organization with the administrator role held in every
// organization or held in that one, and either counts. Each organization is
// asked scoped to itself: the caller belongs to every one of them —
// requireAccountAuthority established it for a change to the account, and a
// change to the roles in one organization names only the one the request is
// for — so this is the scope the tenant resolver would have given them there.
//
// The repository is asked the question rather than handed the member list to
// search: that list stops at a thousand rows, and an administrator past the end
// of it did not count.
func (s *userService) requireAnotherAdministrator(ctx context.Context, account models.UserModel, organizations []uuid.UUID) error {
	for _, orgID := range organizations {
		orgCtx := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: orgID.String()})
		another, err := s.repo.User().HasAnotherAdministrator(orgCtx, orgID, uuid.UUID(account.ID))
		if err != nil {
			return fmt.Errorf("could not count the administrators of organization %s: %w", orgID, err)
		}
		if !another {
			return s.lastAdministratorRefusal(orgCtx, account, orgID)
		}
	}
	return nil
}

// lastAdministratorRefusal says whose organization would be left without an
// administrator, by name.
//
// The account's memberships carry ids only, so the name is read here, on the
// way to refusing and nowhere else, scoped to that organization as the count
// before it was. The caller is a member, so the name is theirs to read.
func (s *userService) lastAdministratorRefusal(orgCtx context.Context, account models.UserModel, orgID uuid.UUID) error {
	org, err := s.repo.Organization().Get(orgCtx, orgID)
	if err != nil {
		return fmt.Errorf("could not read the organization %s: %w", orgID, err)
	}
	return apierr.Forbiddenf("%s is the last administrator of %s; make somebody else an administrator first",
		account.Username, org.Name)
}

// requireAnotherGlobalAdministrator refuses to take away the last account that
// holds the administrator role in every organization.
//
// Only such an account can add an organization, manage the platform accounts,
// and — where the platform gate admits it — change what every organization
// shares, a role held in every organization included. A role granted in one
// organization never makes somebody one, so an installation left without any
// could not get one back through Metis, however well each organization is
// administered.
func (s *userService) requireAnotherGlobalAdministrator(ctx context.Context, account models.UserModel) error {
	another, err := s.repo.User().HasAnotherGlobalAdministrator(ctx, uuid.UUID(account.ID))
	if err != nil {
		return fmt.Errorf("could not count the administrators of every organization: %w", err)
	}
	if another {
		return nil
	}
	return apierr.Forbiddenf("%s is the last administrator of every organization; make somebody else one first: only an "+
		"administrator of every organization can add an organization, manage the platform accounts and change what "+
		"every organization shares", account.Username)
}
