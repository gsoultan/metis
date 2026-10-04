package entities

// DecisionPoint is one place in a process that decides from a value a step
// would have set: what it reads, and how much of that a waiver of the step
// supplies.
//
// It is what a plan to waive a step shows an administrator, so that a branch
// is never taken on a value nobody gave. A point that reads from the step and
// is not supplied is a reason to refuse; one that could not be read is a
// warning, and is listed so that somebody looks.
//
// No JSON or ORM tags: an endpoint maps it to a view.
type DecisionPoint struct {
	NodeID string
	// NodeName is the step's name, or its id when it has none.
	NodeName string
	Kind     DecisionPointKind

	// Reads is every variable the point is known to read, sorted. For a called
	// process it is what the process is handed of the step's fields: what it
	// then reads is not known.
	Reads []string
	// Supplied is what it takes from the step that the waiver gives a value
	// for; Missing is what it is known to take from the step that the waiver
	// does not. Both sorted. Nothing is known to be missing from a called
	// process, which is not read: Analysed says so instead.
	Supplied []string
	Missing  []string

	// HasDefaultFlow reports that a gateway has a flow to take when no
	// condition holds, so a missing value sends it there rather than raising
	// an incident.
	HasDefaultFlow bool
	// Analysed is false when what the point reads could not be told in full.
	// Reads, Supplied and Missing then hold what could be told, and may be
	// short.
	Analysed bool
}
