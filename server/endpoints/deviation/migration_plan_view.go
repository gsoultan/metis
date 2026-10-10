package deviation

import "github.com/gsoultan/metis/server/domains/entities"

// MigrationPlanView is a migration's plan as a route writes it: on the
// migrate route, whose plan it is, and on the approval of a migration, which
// answers with the plan the run was made from. One view for both, so a client
// reads one shape.
//
// It writes exactly what the migrate route wrote while the plan went out as
// the entity encodes itself — the same names in the same order, a list left
// out when it holds nothing and a list when it holds something, never null —
// and is now the route's own: a field added to the entity is not on the wire
// until it is added here. It carries no account id; a plan has none.
type MigrationPlanView struct {
	SourceKey     string `json:"source_key"`
	SourceVersion int    `json:"source_version"`
	TargetVersion int    `json:"target_version"`
	TargetID      string `json:"target_id"`
	// Instances is how many instances of the source version the plan lists.
	Instances int `json:"instances"`
	// Moves is where work sits and where it would land, one entry a step.
	Moves []NodeMoveView `json:"moves,omitzero"`
	// Refusals are why the plan would not be applied; Warnings what somebody
	// should look at first, and do not block.
	Refusals []string `json:"refusals,omitzero"`
	Warnings []string `json:"warnings,omitzero"`
	// ComplianceHolds are the controls the migration would take from
	// instances that have not passed them.
	ComplianceHolds []ComplianceHoldView `json:"compliance_holds,omitzero"`
	// Actions are the steps whose work the migration decides rather than moves.
	Actions []PlannedNodeActionView `json:"actions,omitzero"`
	// RemovedNodes are the steps the target version no longer has.
	RemovedNodes []string `json:"removed_nodes,omitzero"`
	// RequiresSecondApprover says an apply of this plan is sent to a second
	// administrator instead of being made; SecondApproverReasons says why,
	// and is left out when it is not.
	RequiresSecondApprover bool     `json:"requires_second_approver"`
	SecondApproverReasons  []string `json:"second_approver_reasons,omitzero"`
}

// NodeMoveView is one step's worth of a plan: how much work sits on From, and
// where it would land.
type NodeMoveView struct {
	From           string `json:"from"`
	To             string `json:"to"`
	Tokens         int    `json:"tokens"`
	Tasks          int    `json:"tasks"`
	Jobs           int    `json:"jobs"`
	TasksClaimed   int    `json:"tasks_claimed,omitzero"`
	TasksDelegated int    `json:"tasks_delegated,omitzero"`
	Events         int    `json:"events,omitzero"`
	// Mapped is false when the step is carried across under the same id.
	Mapped bool `json:"mapped"`
}

// ComplianceHoldView is one control-bearing step a migration would drop.
type ComplianceHoldView struct {
	NodeID string `json:"node_id"`
	Name   string `json:"name,omitzero"`
	Note   string `json:"note,omitzero"`
	// Instances is how many listed instances have not passed it.
	Instances int `json:"instances"`
}

// PlannedNodeActionView is one step whose work is decided rather than moved.
type PlannedNodeActionView struct {
	NodeID string `json:"node_id"`
	Name   string `json:"name,omitzero"`
	Kind   string `json:"kind"`
	Reason string `json:"reason,omitzero"`
}

// MigrationPlanViewOf maps a plan to what a route writes. A list the plan
// does not have stays absent and one it has stays a list, empty or not: the
// plan's own lists of words are handed on, not copied — a view is written
// out and not changed.
func MigrationPlanViewOf(plan entities.MigrationPlan) MigrationPlanView {
	return MigrationPlanView{
		SourceKey:              plan.SourceKey,
		SourceVersion:          plan.SourceVersion,
		TargetVersion:          plan.TargetVersion,
		TargetID:               plan.TargetID.String(),
		Instances:              plan.Instances,
		Moves:                  viewsOf(plan.Moves, nodeMoveViewOf),
		Refusals:               plan.Refusals,
		Warnings:               plan.Warnings,
		ComplianceHolds:        viewsOf(plan.ComplianceHolds, complianceHoldViewOf),
		Actions:                viewsOf(plan.Actions, plannedNodeActionViewOf),
		RemovedNodes:           plan.RemovedNodes,
		RequiresSecondApprover: plan.RequiresSecondApprover,
		SecondApproverReasons:  plan.SecondApproverReasons,
	}
}

// viewsOf maps a list one for one. No list stays no list, so that it is left
// out as it always was, and an empty one stays an empty list.
func viewsOf[E, V any](list []E, view func(E) V) []V {
	if list == nil {
		return nil
	}
	views := make([]V, 0, len(list))
	for _, one := range list {
		views = append(views, view(one))
	}
	return views
}

func nodeMoveViewOf(move entities.NodeMove) NodeMoveView {
	return NodeMoveView{
		From: move.From, To: move.To, Tokens: move.Tokens, Tasks: move.Tasks, Jobs: move.Jobs,
		TasksClaimed: move.TasksClaimed, TasksDelegated: move.TasksDelegated, Events: move.Events, Mapped: move.Mapped,
	}
}

func complianceHoldViewOf(hold entities.ComplianceHold) ComplianceHoldView {
	return ComplianceHoldView{NodeID: hold.NodeID, Name: hold.Name, Note: hold.Note, Instances: hold.Instances}
}

func plannedNodeActionViewOf(action entities.PlannedNodeAction) PlannedNodeActionView {
	return PlannedNodeActionView{NodeID: action.NodeID, Name: action.Name, Kind: action.Kind, Reason: action.Reason}
}
