package impl

import (
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// deviationService is the two things a request may ask about an instance's
// deviations, each done by what already does it: the ledger reads, the
// in-place command acts. It adds nothing to either (a facade over the two, so
// that the composition root hands the service facade one value).
type deviationService struct {
	servicecontracts.DeviationReader
	servicecontracts.InstanceDeviator
}

// NewDeviationService joins the reader of the ledger and the in-place command
// into what the service facade exposes.
func NewDeviationService(reader servicecontracts.DeviationReader, deviator servicecontracts.InstanceDeviator) servicecontracts.DeviationService {
	return deviationService{DeviationReader: reader, InstanceDeviator: deviator}
}
