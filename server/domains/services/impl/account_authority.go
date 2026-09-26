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
// organization the caller does not.
//
// Roles are global, so changing an account's roles — or deleting it — acts in
// every organization it belongs to. Being an administrator grants authority
// over your own organizations, not over the others an account happens to share.
func requireAccountAuthority(ctx context.Context, account models.UserModel) error {
	if err := requireAccountVisible(ctx, account); err != nil {
		return err
	}
	memberships, hasCaller := callerOrganizations(ctx)
	if !hasCaller {
		return nil
	}
	for _, org := range account.Organizations {
		if !memberships[uuid.UUID(org.ID)] {
			return apierr.Forbiddenf(
				"%s also belongs to %s, which you are not a member of; an administrator there has to make this change",
				account.Username, org.Name)
		}
	}
	return nil
}

// requireAnotherAdministrator refuses to take the last administrator away from
// any organization the account belongs to.
//
// There is no default account by design, so an organization left with nobody
// who can administer it cannot be administered through Metis again. Each
// organization is asked scoped to itself: requireAccountAuthority has already
// established that the caller belongs to every one of them, so this is the
// scope the tenant resolver would have given them there.
//
// The repository is asked the question rather than handed the member list to
// search: that list stops at a thousand rows, and an administrator past the end
// of it did not count.
func (s *userService) requireAnotherAdministrator(ctx context.Context, account models.UserModel) error {
	for _, org := range account.Organizations {
		orgID := uuid.UUID(org.ID)
		orgCtx := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: orgID.String()})
		another, err := s.repo.User().HasAnotherAdministrator(orgCtx, orgID, uuid.UUID(account.ID))
		if err != nil {
			return fmt.Errorf("could not count the administrators of %s: %w", org.Name, err)
		}
		if !another {
			return apierr.Forbiddenf(
				"%s is the last administrator of %s; make somebody else an administrator first",
				account.Username, org.Name)
		}
	}
	return nil
}
