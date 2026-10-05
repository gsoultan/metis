package impl

import (
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// deviationService is the three things a request may ask about an instance's
// deviations, each done by what already does it: the ledger reads, the
// in-place command acts, and the request service reads and decides what
// waits for a second administrator. It adds nothing to any of them (a facade
// over the three, so that the composition root hands the service facade one
// value).
type deviationService struct {
	servicecontracts.DeviationReader
	servicecontracts.InstanceDeviator
	servicecontracts.DeviationRequestService
}

// NewDeviationService joins the reader of the ledger, the in-place command
// and the requests for a second administrator into what the service facade
// exposes.
func NewDeviationService(
	reader servicecontracts.DeviationReader,
	deviator servicecontracts.InstanceDeviator,
	requests servicecontracts.DeviationRequestService,
) servicecontracts.DeviationService {
	return deviationService{DeviationReader: reader, InstanceDeviator: deviator, DeviationRequestService: requests}
}
