package deviation

import (
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

// maxRequestInstancesShown is how many of the instances a request covers its
// view lists. The list is for reading — a migration over a busy version can
// cover thousands — and is always beside how many there are in all. Nothing
// decides from the shortened list: the gate reads the request's own.
const maxRequestInstancesShown = 200

// ListDeviationRequestsRequest asks for a page of the queue of requests for a
// second administrator. Every field comes from the address.
type ListDeviationRequestsRequest struct {
	// Status is the status to list, as a request reads now; with none, what
	// still waits.
	Status string
	// ProjectID narrows the page to one project of the caller's organization.
	ProjectID      string
	Page, PageSize int
}

// GetDeviationRequestRequest asks for one request. ID is taken from the
// address.
type GetDeviationRequestRequest struct {
	ID string
}

// DecideDeviationRequestRequest approves or rejects one request.
//
// It says why, and nothing else. What an approval carries out is the command
// the request sealed when it was asked for: a decision has no field for a
// command, for outputs or for a visit key, and a body that names one is
// refused rather than read without it.
type DecideDeviationRequestRequest struct {
	// ID is taken from the address, never from the body.
	ID     string `json:"-"`
	Reason string `json:"reason"`
}

// DeviationRequestView is a request for a second administrator as an
// administrator of its organization reads it. It carries names, never account
// ids, and not the fingerprint the server tells requests apart by.
//
// Status is the status the request has now: one past its deadline reads
// expired whether or not anything has written that down.
//
// It never holds null. An id the request does not have (instance_id for a
// migration, the two definition ids for a waive) and a decision nobody has
// made (decided_by, decision_reason, decided_at) are left out. self_approved
// is always said.
//
// Command, Plan, Because, Instances and InstancesInAll are what was asked,
// what the requester was shown, why it needs somebody else and which
// instances it covers. A request read by itself has all five; one with
// nothing to say in them says so with an empty object, an empty list or 0.
// They are left out — not written as empty, which would read as a fact about
// the request — in two cases:
//
//   - in the queue, which does not read them (ListedRequestViewOf);
//   - when a stored document no longer opens. Unavailable then names which
//     of command, plan and instances could not be read; because goes with the
//     plan, and instances_in_all with instances.
//
// Plan is the plan as the requester previewed it, with every list it cuts
// short beside its count. Instances is at most 200 ids, beside
// InstancesInAll. Neither is what an approval acts on: it plans again.
type DeviationRequestView struct {
	ID                 string         `json:"id"`
	Kind               string         `json:"kind"`
	Status             string         `json:"status"`
	ProjectID          string         `json:"project_id"`
	InstanceID         string         `json:"instance_id,omitzero"`
	SourceDefinitionID string         `json:"source_definition_id,omitzero"`
	TargetDefinitionID string         `json:"target_definition_id,omitzero"`
	RequestedBy        string         `json:"requested_by"`
	Reason             string         `json:"reason"`
	Because            []string       `json:"because,omitzero"`
	Instances          []string       `json:"instances,omitzero"`
	InstancesInAll     *int           `json:"instances_in_all,omitzero"`
	Command            map[string]any `json:"command,omitzero"`
	Plan               map[string]any `json:"plan,omitzero"`
	// Unavailable is left out of a request whose documents were all read, and
	// of a listed one.
	Unavailable    []string       `json:"unavailable,omitzero"`
	ExpiresAt      time.Time      `json:"expires_at"`
	DecidedBy      string         `json:"decided_by,omitzero"`
	DecisionReason string         `json:"decision_reason,omitzero"`
	DecidedAt      *time.Time     `json:"decided_at,omitzero"`
	SelfApproved   bool           `json:"self_approved"`
	Outcome        map[string]any `json:"outcome"`
	CreatedAt      time.Time      `json:"created_at"`
}

// ListedRequestViewOf maps a request to what the queue returns: who asked,
// for what kind of thing, where it stands and what became of it — and none of
// what only a single read has, whatever r happens to carry.
func ListedRequestViewOf(r entities.DeviationRequest) DeviationRequestView {
	view := DeviationRequestView{
		ID: idString(r.ID), Kind: string(r.Kind), Status: string(r.Status),
		RequestedBy: r.RequestedBy, Reason: r.Reason, ExpiresAt: r.ExpiresAt,
		DecidedBy: r.DecidedBy, DecisionReason: r.DecisionReason, DecidedAt: r.DecidedAt,
		SelfApproved: r.SelfApproved(), Outcome: objectOf(r.Outcome), CreatedAt: r.CreatedAt,
	}
	if r.Project != nil {
		view.ProjectID = idString(r.Project.ID)
	}
	if r.Instance != nil {
		view.InstanceID = idString(r.Instance.ID)
	}
	if r.SourceDefinition != nil {
		view.SourceDefinitionID = idString(r.SourceDefinition.ID)
	}
	if r.TargetDefinition != nil {
		view.TargetDefinitionID = idString(r.TargetDefinition.ID)
	}
	return view
}

// RequestViewOf maps a request read by itself to what a route returns: the
// listed view, and the documents the request keeps.
//
// A request is whole when its command, its plan and its list of instances
// are not nil; the repository answers one without them only when what is
// stored no longer opens, so that a damaged request can still be read and
// closed. Each that is missing is left out and named in Unavailable.
func RequestViewOf(r entities.DeviationRequest) DeviationRequestView {
	view := ListedRequestViewOf(r)
	if r.Command != nil {
		view.Command = r.Command
	} else {
		view.Unavailable = append(view.Unavailable, "command")
	}
	if r.Plan != nil {
		view.Plan, view.Because = r.Plan, listOf(r.Because())
	} else {
		view.Unavailable = append(view.Unavailable, "plan")
	}
	if r.ApprovedInstances != nil {
		view.Instances, view.InstancesInAll = instancesShown(r)
	} else {
		view.Unavailable = append(view.Unavailable, "instances")
	}
	return view
}

// instancesShown is the first ids a request covers, at most
// maxRequestInstancesShown of them, and how many it covers in all. The list
// is a new one: the request's own is not cut.
func instancesShown(r entities.DeviationRequest) ([]string, *int) {
	inAll := len(r.ApprovedInstances)
	listed := min(inAll, maxRequestInstancesShown)
	shown := make([]string, 0, listed)
	for _, id := range r.ApprovedInstances[:listed] {
		shown = append(shown, id.String())
	}
	return shown, &inAll
}

// listedViews maps a page of the queue. A list always, empty rather than
// null.
func listedViews(page []entities.DeviationRequest) []DeviationRequestView {
	views := make([]DeviationRequestView, 0, len(page))
	for _, request := range page {
		views = append(views, ListedRequestViewOf(request))
	}
	return views
}

// ListDeviationRequestsResponse is one page of the queue, and how many
// requests there are in all at the status asked for.
type ListDeviationRequestsResponse struct {
	Requests []DeviationRequestView `json:"requests"`
	Total    int64                  `json:"total"`
	Err      error                  `json:"err,omitzero"`
}

func (r ListDeviationRequestsResponse) Failed() error { return r.Err }

// GetDeviationRequestResponse is one request, whole.
type GetDeviationRequestResponse struct {
	Request DeviationRequestView `json:"request"`
	Err     error                `json:"err,omitzero"`
}

func (r GetDeviationRequestResponse) Failed() error { return r.Err }

// ApproveDeviationRequestResponse answers an approval: the request as the
// decision left it, whether what it asked for was carried out, and what was
// done — for a waive, the ledger's record and the plan it was applied from,
// as the instance stood when it was approved.
type ApproveDeviationRequestResponse struct {
	Request DeviationRequestView `json:"request"`
	Applied bool                 `json:"applied"`
	// Deviation is the record of an approved waive.
	Deviation *DeviationView `json:"deviation,omitzero"`
	// Plan is the plan the approval acted on: a PlanView for a waive, and a
	// MigrationPlanView — the plan as the migrate route writes it — for a
	// migration.
	Plan any `json:"plan,omitzero"`
	// PassedOver are the instances an approved migration's run left as they
	// were, each with why: a list for a migration, empty when the run left
	// nobody behind, and absent for a waive.
	PassedOver []PassedOverView `json:"passed_over,omitzero"`
	Err        error            `json:"err,omitzero"`
}

// PassedOverView is one instance a migration's run left alone, as a reply
// carries it: the migrate route's, for an apply one administrator could
// make, and the approval's, for a run a second administrator approved.
type PassedOverView struct {
	InstanceID string `json:"instance_id"`
	// Cause is why, as a code: one of the closed set entities.PassedOverCauses
	// names. It is for a client that says it in its own language, or acts on
	// it; a cause a client does not know is said with Reason.
	Cause string `json:"cause"`
	// Steps are the steps the cause is about, in the order Reason names
	// them. A list always, empty for a cause that is about no step.
	Steps []PassedOverStepView `json:"steps"`
	// Reason is why, in plain English words; it names a step by its name, not
	// its id.
	Reason string `json:"reason"`
}

// PassedOverStepView is a step the cause of passing an instance over is
// about: its id in the version the instance runs, and the name that version
// gives it — the id again where it gives none.
type PassedOverStepView struct {
	NodeID string `json:"node_id"`
	Name   string `json:"name"`
}

func (r ApproveDeviationRequestResponse) Failed() error { return r.Err }

// RejectDeviationRequestResponse answers a rejection, or a withdrawal by
// whoever asked: the request as it then is.
type RejectDeviationRequestResponse struct {
	Request DeviationRequestView `json:"request"`
	Err     error                `json:"err,omitzero"`
}

func (r RejectDeviationRequestResponse) Failed() error { return r.Err }
