package entities

import "github.com/google/uuid"

// DeviationPlan is what a deviation command would do to an instance as it
// stands, and every reason it cannot be done: the answer to a preview, and
// what an apply is checked against.
//
// A plan with refusals is still an answer. Each refusal and each warning is a
// sentence a process owner can act on.
//
// No JSON or ORM tags: an endpoint maps it to a view.
type DeviationPlan struct {
	InstanceID uuid.UUID
	Kind       DeviationKind
	Scope      DeviationScope

	// NodeID and NodeName are the step the command acts on, the name falling
	// back to the id. Both are empty for a cancel that names no step, and the
	// name is empty for a step the process does not have.
	NodeID, NodeName string
	// VisitKey identifies the work the plan was made for: the open work it
	// lists and where the instance waits — at the step, or anywhere for a
	// cancel. An apply sends it back, and is refused when the work has
	// changed since.
	VisitKey string

	// OpenWork is the tasks open where the command acts: on the step for a
	// waive and a hold, and anywhere on the instance for a cancel, which
	// withdraws them all whichever step it names.
	OpenWork []DeviationOpenWork
	// OpenWorkInAll is how many such tasks there are. OpenWork lists the
	// first of them; the visit key and the act cover them all.
	OpenWorkInAll int
	// Outputs is what a waive would set.
	Outputs map[string]any
	// DecisionPoints is the places in the process that decide from a value
	// the waived step would have set: those missing a value first, then those
	// nobody could read, then the rest. A process is somebody's input, so no
	// more than a screenful is listed, each with no more names than a
	// sentence spells out; DecisionPointsInAll is how many there are. The
	// refusals are worked out from all of them.
	DecisionPoints      []DecisionPoint
	DecisionPointsInAll int
	// Missing is every value the waived step would have set that some
	// decision point reads and the waive does not give, sorted: the whole of
	// what has still to be supplied, where a decision point names only the
	// first few it is missing. No more are listed than one waive may set
	// (MaxDeviationOutputs), each cut to a length somebody would read;
	// MissingInAll is how many there are.
	Missing      []string
	MissingInAll int
	// CalledInstances is the processes this instance started that are still
	// running, which a cancel has to wait for.
	CalledInstances []uuid.UUID

	// RequiresSecondApprover reports that the act waits for somebody else to
	// agree to it before it is made.
	RequiresSecondApprover bool

	// Refusals is why the command cannot be applied; Warnings is what whoever
	// applies it should know first.
	Refusals, Warnings []string
}

// Applicable reports whether the plan can be applied: nothing refuses it.
func (p DeviationPlan) Applicable() bool {
	return len(p.Refusals) == 0
}
