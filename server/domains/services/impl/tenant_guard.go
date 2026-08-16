package impl

import (
	"fmt"

	"context"

	"github.com/gsoultan/gobpm/internal/pkg/auth"
	"github.com/gsoultan/gobpm/server/domains/entities"
)

// ErrForeignOrganization is returned when a request names an organization the
// caller does not belong to.
var ErrForeignOrganization = fmt.Errorf("%w: that organization is not yours", auth.ErrUnauthorized)

// requireOwnOrganizations refuses a write that places a record in an
// organization other than the caller's.
//
// The repository tenant scope stops a caller *reading* another tenant's rows.
// It cannot stop this: the organization arrives in the request body, and a row
// created there is well-formed and correctly scoped — to the victim. Only the
// service layer knows the difference between "the tenant I am in" and "a tenant
// named in this payload".
//
// With no TenantContext there is nothing to compare against, and the call is
// system work — setup seeding the first administrator, for one — so it passes.
func requireOwnOrganizations(ctx context.Context, orgs []*entities.Organization) error {
	tenant, ok := entities.TenantContextFrom(ctx)
	if !ok || tenant.TenantID == "" {
		return nil
	}

	for _, org := range orgs {
		if org == nil {
			continue
		}
		if org.ID.String() != tenant.TenantID {
			return fmt.Errorf("%w (%s)", ErrForeignOrganization, org.ID)
		}
	}
	return nil
}
