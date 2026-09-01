package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// TenantResolver extracts and validates the tenant identity from an inbound
// request context. Implementations can resolve tenant from JWT claims,
// API key headers, subdomain, or any other mechanism.
type TenantResolver interface {
	// Resolve extracts the TenantContext from the given context.
	// Returns an error if the tenant cannot be identified or is not authorised.
	Resolve(ctx context.Context) (entities.TenantContext, error)
}
