package contracts

import "context"

// OrganizationCounter says how many organizations share the installation.
//
// Its own interface because its one consumer is the gate on what every
// organization runs, which needs this and nothing else of OrganizationService —
// and because, unlike the reads beside it, the answer is not scoped to the
// caller's organization.
type OrganizationCounter interface {
	CountOrganizations(ctx context.Context) (int64, error)
}
