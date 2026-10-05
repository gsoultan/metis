package entities

// DeviationOutcome answers a deviation command: the plan, and what became of
// it.
//
// No JSON or ORM tags: an endpoint maps it to a view.
type DeviationOutcome struct {
	Plan DeviationPlan
	// Applied reports that the instance was changed as the plan says, by this
	// request or by an earlier one for the same visit.
	Applied bool
	// Replayed reports that an earlier request had already made the change,
	// and this one answers with its record instead of acting again.
	Replayed bool
	// Deviation is the record of the change, nil for a preview.
	Deviation *Deviation
	// PendingApproval says the change was not made but sent to a second
	// administrator, and which request waits. Nil when nothing waits.
	PendingApproval *PendingApproval
}
