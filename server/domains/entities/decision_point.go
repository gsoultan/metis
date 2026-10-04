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

	// Reads is what the point is known to read of the fields the step's form
	// declares, sorted: the names somebody waiving the step can act on, and
	// no more of them than a sentence would spell out. For a called process
	// it is what the process is handed of those fields: what it then reads is
	// not known.
	Reads []string
	// ReadsInAll is how many variables the point is known to read, the step's
	// and everybody else's. A decision table of a thousand columns reads a
	// thousand, and lists the two the step sets. Where nodes that share an id
	// consult several decisions it is each decision's count added up, so a
	// name two of them read is counted twice: never too few.
	ReadsInAll int
	// Supplied is what it takes from the step that the waiver gives a value
	// for; Missing is what it is known to take from the step that the waiver
	// does not. Both sorted, and like Reads the first few: the whole of what
	// is missing is the plan's to list, once for every point. Nothing is
	// known to be missing from a called process, which is not read: Analysed
	// says so instead.
	//
	// Reads, Supplied and Missing may be shared with other points that read
	// the same thing. Read them; to cut or change one, copy it first.
	Supplied []string
	Missing  []string
	// MissingInAll is how many of the step's fields the point is known to
	// take that the waiver does not supply; Missing names the first of them.
	// Counted as ReadsInAll is where nodes share an id, and never more than
	// the form has fields.
	MissingInAll int

	// HasDefaultFlow reports that a gateway has a flow to take when no
	// condition holds, so a missing value sends it there.
	HasDefaultFlow bool
	// Analysed is false when what the point reads could not be told in full.
	// Reads, ReadsInAll, Supplied and Missing then hold what could be told,
	// and may be short.
	Analysed bool
}
