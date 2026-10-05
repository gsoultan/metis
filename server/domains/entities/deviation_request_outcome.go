package entities

// DeviationRequestOutcome answers a decision on a request: the request as the
// decision left it, and what was done because of it.
//
// No JSON or ORM tags: an endpoint maps it to a view.
type DeviationRequestOutcome struct {
	Request DeviationRequest
	// Applied reports that what the request asked for was carried out.
	Applied bool
	// Deviation is the ledger's record of an approved waive.
	Deviation *Deviation
	// WaivePlan is the plan an approved waive was applied from.
	WaivePlan *DeviationPlan
	// MigrationPlan is the plan an approved migration ran from.
	MigrationPlan *MigrationPlan
}
