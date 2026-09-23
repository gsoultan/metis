package entities

import "github.com/google/uuid"

// MigrationPlan is what moving running instances onto another version would do,
// worked out without writing anything.
//
// It exists because the alternative is asking somebody to authorise a change to
// durable business commitments — instances that are somebody's purchase order,
// somebody's leave request — from a form with no preview. The refusals in
// particular are worth seeing before committing: a mapping that strands a token
// is refused either way, and finding that out from a dry run costs nothing while
// finding it out from a failed apply costs a half-finished cutover.
type MigrationPlan struct {
	SourceKey     string    `json:"source_key"`
	SourceVersion int       `json:"source_version"`
	TargetVersion int       `json:"target_version"`
	TargetID      uuid.UUID `json:"target_id"`

	// Instances is how many running instances would move.
	Instances int `json:"instances"`

	// Moves is where the work currently sits and where it would land, one entry
	// per distinct node, so a plan over a thousand instances is still readable.
	Moves []NodeMove `json:"moves,omitzero"`

	// Refusals are the reasons this would not be applied. A plan with any is
	// one the apply would reject; they are returned rather than raised so the
	// caller sees all of them at once instead of one per attempt.
	Refusals []string `json:"refusals,omitzero"`

	// Warnings are what somebody should look at before applying, as distinct
	// from what would corrupt an instance. They do not block.
	//
	// Separate from Refusals because collapsing the two teaches people to
	// dismiss the list: a warning that reads like a refusal gets clicked past,
	// and then a real refusal does too.
	Warnings []string `json:"warnings,omitzero"`

	// ComplianceHolds are the control-bearing steps this migration would take
	// away from instances that have not performed them yet.
	//
	// A hold is a refusal the caller may accept by name. It is not a warning:
	// the difference between "this approval was skipped" and "this approval was
	// skipped, by Dita, on the 22nd, because the approver had left" is the whole
	// of what an auditor asks for, and only a deliberate acknowledgement can
	// produce the second.
	ComplianceHolds []ComplianceHold `json:"compliance_holds,omitzero"`

	// Actions are the nodes whose work this migration decides rather than
	// moves — skipped, or the instance ended there.
	//
	// Reported back so a preview shows the decisions as prominently as the
	// moves. "Two instances move" and "two instances have an approval skipped"
	// are not the same sentence, and a plan that only counted moves would show
	// them identically.
	Actions []PlannedNodeAction `json:"actions,omitzero"`

	// RemovedNodes are the nodes the target version no longer has, whether or
	// not anything is currently sitting on one.
	//
	// Listed because a removed user task is not only a removed step: it is the
	// removed producer of every variable its form used to write, and the
	// gateways downstream still read them. That consequence is invisible in a
	// node mapping, which is why people discover it as an incident storm.
	RemovedNodes []string `json:"removed_nodes,omitzero"`
}

// Applicable reports whether applying this plan would be accepted.
func (p MigrationPlan) Applicable() bool { return len(p.Refusals) == 0 }

// PlannedNodeAction is one node whose work is decided rather than moved.
type PlannedNodeAction struct {
	NodeID string `json:"node_id"`
	Name   string `json:"name,omitzero"`
	// Kind is "skip" or "cancel".
	Kind string `json:"kind"`
	// Reason is why, and it is carried into every instance's trail.
	Reason string `json:"reason,omitzero"`
}

// ComplianceHold is one control-bearing step a migration would drop.
type ComplianceHold struct {
	NodeID string `json:"node_id"`
	Name   string `json:"name,omitzero"`
	// Note is whatever the modeller wrote about why the step is there. It is
	// carried into the refusal and the audit entry, because "you are about to
	// skip node opsApprove" and "you are about to skip the second signature
	// SOX requires over $50k" are not the same sentence.
	Note string `json:"note,omitzero"`
	// Instances is how many running instances have not passed it yet.
	Instances int `json:"instances"`
}

// NodeMove is one node's worth of a plan.
type NodeMove struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Tokens, Tasks and Jobs are how much work sits on From. Separated because
	// they read differently to whoever is deciding: a task is somebody's inbox
	// item and a job is a timer that will fire.
	Tokens int `json:"tokens"`
	Tasks  int `json:"tasks"`
	Jobs   int `json:"jobs"`
	// TasksClaimed and TasksDelegated are the subset of Tasks that somebody has
	// in their hands right now, counted apart because they cost differently.
	// An unclaimed task is a queue item nobody has started; a claimed one is a
	// person with the form open who is about to lose their place, because a
	// task that changes node has its assignment re-derived from the new node.
	TasksClaimed   int `json:"tasks_claimed,omitzero"`
	TasksDelegated int `json:"tasks_delegated,omitzero"`
	// Events is how many message or signal subscriptions wait on this node.
	// Counted apart from Jobs because a subscription is a promise to somebody
	// outside the process: a timer that does not fire is a delay, a message
	// that correlates to nothing is a caller who never gets an answer.
	Events int `json:"events,omitzero"`
	// Mapped is false when From is carried across unchanged because the target
	// has a node of the same id. That is the common case and needs no mapping
	// entry; showing it is how somebody confirms they did not need one.
	Mapped bool `json:"mapped"`
}
