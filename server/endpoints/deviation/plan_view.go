package deviation

import (
	"github.com/gsoultan/metis/server/domains/entities"
)

// PlanView is a plan as the route returns it: what a waive, a cancel or a
// hold would do to an instance as it stands, and every reason it cannot be
// done.
//
// Every list is a list, empty when there is nothing in it, so a client reads
// it without asking first. A list is not the whole of what there is: a process
// is somebody's input and a plan has a size whatever the process, so OpenWork
// and DecisionPoints hold the first of them and OpenWorkInAll and
// DecisionPointsInAll say how many there are; Refusals and Warnings spell out
// the first few of a kind and count the rest. Missing is the one list that is
// what to supply, each name in full. Whether the plan can be applied is
// Applicable, and nothing else: an empty list is not "nothing".
type PlanView struct {
	InstanceID string `json:"instance_id"`
	Kind       string `json:"kind"`
	Scope      string `json:"scope"`
	// NodeID and NodeName are left out of a cancel that names no step.
	NodeID   string `json:"node_id,omitzero"`
	NodeName string `json:"node_name,omitzero"`
	// VisitKey is what an apply sends back: it is refused when the work is no
	// longer what this plan was made for.
	VisitKey string `json:"visit_key"`

	OpenWork      []OpenWorkView `json:"open_work"`
	OpenWorkInAll int            `json:"open_work_in_all"`
	// Outputs is what a waive would set; an empty object when it sets nothing.
	Outputs map[string]any `json:"outputs"`

	DecisionPoints      []DecisionPointView `json:"decision_points"`
	DecisionPointsInAll int                 `json:"decision_points_in_all"`
	// Missing is every value some decision point reads that the waive does not
	// give, and MissingInAll how many there are.
	Missing      []string `json:"missing"`
	MissingInAll int      `json:"missing_in_all"`

	// CalledInstances is the processes this instance started that have not
	// ended, by id.
	CalledInstances        []string `json:"called_instances"`
	RequiresSecondApprover bool     `json:"requires_second_approver"`

	Refusals []string `json:"refusals"`
	Warnings []string `json:"warnings"`
	// Applicable says whether an apply of this plan would be made: nothing
	// refuses it.
	Applicable bool `json:"applicable"`
}

// OpenWorkView is one task that is somebody's to do where the act would be
// made. Its holder is named, never identified by an account id.
type OpenWorkView struct {
	TaskID   string `json:"task_id"`
	Name     string `json:"name"`
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name"`
	Status   string `json:"status"`
	// Assignee is left out when nobody holds the task; IterationID on a step
	// that runs once.
	Assignee    string `json:"assignee,omitzero"`
	IterationID string `json:"iteration_id,omitzero"`
}

// DecisionPointView is one place in the process that decides from a value the
// waived step would have set. Reads, Supplied and Missing name the first few;
// ReadsInAll and MissingInAll count them all.
type DecisionPointView struct {
	NodeID         string   `json:"node_id"`
	NodeName       string   `json:"node_name"`
	Kind           string   `json:"kind"`
	Reads          []string `json:"reads"`
	ReadsInAll     int      `json:"reads_in_all"`
	Supplied       []string `json:"supplied"`
	Missing        []string `json:"missing"`
	MissingInAll   int      `json:"missing_in_all"`
	HasDefaultFlow bool     `json:"has_default_flow"`
	// Analysed is false when what the point reads could not be told in full;
	// the lists then hold what could be told, and may be short.
	Analysed bool `json:"analysed"`
}

// PlanViewOf maps a plan to what the route returns.
func PlanViewOf(plan entities.DeviationPlan) PlanView {
	view := PlanView{
		InstanceID: idString(plan.InstanceID), Kind: string(plan.Kind), Scope: string(plan.Scope),
		NodeID: plan.NodeID, NodeName: plan.NodeName, VisitKey: plan.VisitKey,
		OpenWork: make([]OpenWorkView, 0, len(plan.OpenWork)), OpenWorkInAll: plan.OpenWorkInAll,
		Outputs:        objectOf(plan.Outputs),
		DecisionPoints: make([]DecisionPointView, 0, len(plan.DecisionPoints)), DecisionPointsInAll: plan.DecisionPointsInAll,
		Missing: listOf(plan.Missing), MissingInAll: plan.MissingInAll,
		CalledInstances:        make([]string, 0, len(plan.CalledInstances)),
		RequiresSecondApprover: plan.RequiresSecondApprover,
		Refusals:               listOf(plan.Refusals), Warnings: listOf(plan.Warnings),
		Applicable: plan.Applicable(),
	}
	for _, work := range plan.OpenWork {
		view.OpenWork = append(view.OpenWork, OpenWorkView{
			TaskID: idString(work.TaskID), Name: work.Name, NodeID: work.NodeID, NodeName: work.NodeName,
			Status: string(work.Status), Assignee: work.Assignee, IterationID: work.IterationID,
		})
	}
	for _, point := range plan.DecisionPoints {
		view.DecisionPoints = append(view.DecisionPoints, DecisionPointView{
			NodeID: point.NodeID, NodeName: point.NodeName, Kind: string(point.Kind),
			Reads: listOf(point.Reads), ReadsInAll: point.ReadsInAll,
			Supplied: listOf(point.Supplied), Missing: listOf(point.Missing), MissingInAll: point.MissingInAll,
			HasDefaultFlow: point.HasDefaultFlow, Analysed: point.Analysed,
		})
	}
	for _, called := range plan.CalledInstances {
		view.CalledInstances = append(view.CalledInstances, called.String())
	}
	return view
}

// listOf is names, or an empty list for none: encoded, a nil list is null.
// The plan's own list is handed on, not copied: a view is written out and not
// changed.
func listOf(names []string) []string {
	if names == nil {
		return []string{}
	}
	return names
}
