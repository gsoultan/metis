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
	// VisitKey identifies the work the plan was made for. An apply sends it
	// back, and is refused when the work has changed since.
	VisitKey string

	// OpenWork is the tasks open where the command acts: on the step, or
	// anywhere on the instance for a cancel that names no step.
	OpenWork []DeviationOpenWork
	// Outputs is what a waive would set.
	Outputs map[string]any
	// DecisionPoints is every place in the process that decides from a value
	// the waived step would have set.
	DecisionPoints []DecisionPoint
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
