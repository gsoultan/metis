package definition

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/endpoints/deviation"
)

type ListDefinitionsRequest struct {
	ProjectID string `json:"project_id,omitzero"`

	// Zero means "no paging requested" — the first page at the server default.
	Page     int `json:"page,omitzero"`
	PageSize int `json:"page_size,omitzero"`
}

type ListDefinitionsResponse struct {
	// Page describes the window returned, so a caller can say "1–50 of 340".
	Page        *PageInfo                     `json:"page,omitempty"`
	Definitions []*entities.ProcessDefinition `json:"definitions,omitzero"`
	Err         error                         `json:"err,omitzero"`
}

// PageInfo describes the window returned.
type PageInfo struct {
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	HasMore  bool  `json:"has_more"`
}

func (r ListDefinitionsResponse) Failed() error { return r.Err }

type GetDefinitionRequest struct {
	ID string `json:"id"`
}

type GetDefinitionResponse struct {
	Definition *entities.ProcessDefinition `json:"definition,omitzero"`
	Err        error                       `json:"err,omitzero"`
}

func (r GetDefinitionResponse) Failed() error { return r.Err }

type CreateDefinitionRequest struct {
	Definition *entities.ProcessDefinition `json:"definition,omitzero"`

	// Stage deploys the version without making it live: new instances keep
	// starting on whichever version is live now, and this one waits to be
	// promoted.
	//
	// Negative ("stage") rather than positive ("promote") so that a client that
	// has never heard of staging — which is every client written before this
	// field existed — keeps getting a deploy that goes live.
	Stage bool `json:"stage,omitzero"`
}

type CreateDefinitionResponse struct {
	ID  uuid.UUID `json:"id"`
	Err error     `json:"err,omitzero"`
	// Version and Live report what the deploy actually did, so the UI can say
	// "v4 deployed and live" or "v4 staged, v3 still live" without a second
	// round trip to find out.
	Version int  `json:"version,omitzero"`
	Live    bool `json:"live"`
}

func (r CreateDefinitionResponse) Failed() error { return r.Err }

// PromoteDefinitionRequest names the version new instances should start on.
type PromoteDefinitionRequest struct {
	ProjectID string `json:"project_id"`
	Key       string `json:"key"`
	Version   int    `json:"version"`
}

type PromoteDefinitionResponse struct {
	Err error `json:"err,omitzero"`
}

func (r PromoteDefinitionResponse) Failed() error { return r.Err }

// ScheduleDefinitionRequest arranges for a version to take over at a time.
type ScheduleDefinitionRequest struct {
	ProjectID string `json:"project_id"`
	Key       string `json:"key"`
	Version   int    `json:"version"`
	// ActivateAt is an RFC 3339 timestamp. A string rather than a parsed time so
	// a malformed one is the caller's mistake reported as such, rather than a
	// decode failure that never reaches the endpoint's validation.
	ActivateAt string `json:"activate_at"`
}

type ScheduleDefinitionResponse struct {
	Err error `json:"err,omitzero"`
}

func (r ScheduleDefinitionResponse) Failed() error { return r.Err }

// CancelScheduledDefinitionRequest names one pending cutover to drop.
type CancelScheduledDefinitionRequest struct {
	ProjectID string `json:"project_id"`
	// ReleaseID names the timeline entry, not the version: the same version can
	// be scheduled more than once.
	ReleaseID string `json:"release_id"`
}

type CancelScheduledDefinitionResponse struct {
	Err error `json:"err,omitzero"`
}

func (r CancelScheduledDefinitionResponse) Failed() error { return r.Err }

// ListDefinitionVersionsRequest asks for one process key's version history.
type ListDefinitionVersionsRequest struct {
	ProjectID string `json:"project_id"`
	Key       string `json:"key"`
}

type ListDefinitionVersionsResponse struct {
	Versions []entities.DefinitionVersionStatus `json:"versions"`
	Err      error                              `json:"err,omitzero"`
}

func (r ListDefinitionVersionsResponse) Failed() error { return r.Err }

// ListLiveVersionsRequest asks which version of each process key is live.
type ListLiveVersionsRequest struct {
	ProjectID string `json:"project_id"`
}

type ListLiveVersionsResponse struct {
	// Live maps process key to the version new instances start on. A key that
	// nobody has promoted is absent, which the caller reads the same way the
	// engine does: the highest version.
	Live map[string]int `json:"live"`
	Err  error          `json:"err,omitzero"`
}

func (r ListLiveVersionsResponse) Failed() error { return r.Err }

type DeleteDefinitionRequest struct {
	ID string `json:"id"`
}

type DeleteDefinitionResponse struct {
	Err error `json:"err,omitzero"`
}

func (r DeleteDefinitionResponse) Failed() error { return r.Err }

type ExportDefinitionRequest struct {
	ID string `json:"id"`
}

type ExportDefinitionResponse struct {
	XML []byte `json:"xml,omitzero"`
	Err error  `json:"err,omitzero"`
}

func (r ExportDefinitionResponse) Failed() error { return r.Err }

// ListJavaScriptConditionsRequest asks for the javascript-conditions worklist.
// It carries nothing: the scope is the caller's tenant, resolved from context.
type ListJavaScriptConditionsRequest struct{}

// ListJavaScriptConditionsResponse is the worklist. Usages is always present —
// an empty list is the answer an operator is working toward, and `[]` says that
// where an omitted field would leave them guessing.
type ListJavaScriptConditionsResponse struct {
	Usages []entities.JavaScriptConditionUsage `json:"usages"`
	Err    error                               `json:"err,omitzero"`
}

func (r ListJavaScriptConditionsResponse) Failed() error { return r.Err }

// ListScriptTasksRequest asks for the script-task inventory. It carries
// nothing: the scope is the caller's tenant, resolved from context.
type ListScriptTasksRequest struct{}

// ListScriptTasksResponse is the inventory. Usages is always present — `[]`
// says "none", where an omitted field would leave a reader guessing whether
// the scan ran.
type ListScriptTasksResponse struct {
	Usages []entities.ScriptTaskUsage `json:"usages"`
	Err    error                      `json:"err,omitzero"`
}

func (r ListScriptTasksResponse) Failed() error { return r.Err }

type ImportDefinitionRequest struct {
	ProjectID string `json:"project_id"`
	XML       []byte `json:"xml"`
}

type ImportDefinitionResponse struct {
	ID  uuid.UUID `json:"id"`
	Err error     `json:"err,omitzero"`
}

func (r ImportDefinitionResponse) Failed() error { return r.Err }

// MigrateInstancesRequest moves running instances onto another version.
//
// The mapping is only needed where a node changed id. Anything unlisted is
// carried across unchanged, which is the common case: most edits add or change
// a node without renaming the ones work is parked on.
type MigrateInstancesRequest struct {
	SourceDefinitionID string            `json:"source_definition_id"`
	TargetDefinitionID string            `json:"target_definition_id"`
	NodeMapping        map[string]string `json:"node_mapping,omitzero"`

	// NodeActions decides the work parked on named nodes instead of moving it:
	// "skip" advances past the step as though it had been performed, "cancel"
	// ends the instance there. Both require a reason, which is recorded on
	// every affected instance's trail.
	NodeActions map[string]servicecontracts.NodeAction `json:"node_actions,omitzero"`

	// Instances narrows the migration to particular instances. Empty means
	// every instance on the source version, which is what it has always meant.
	Instances []string `json:"instances,omitzero"`

	// Acknowledge names the control-bearing steps whose loss the caller accepts.
	//
	// Node ids rather than a blanket flag: an override people can set once and
	// forget is one they stop reading, and it would carry over to whatever the
	// plan holds next time. Naming each one means the acknowledgement lapses
	// the moment the plan changes under it.
	Acknowledge []string `json:"acknowledge,omitzero"`

	// DryRun asks what would happen and changes nothing. The default, because
	// this rewrites instances that are somebody's purchase order — committing
	// has to be the thing you ask for, not the thing you get by omission.
	//
	// A pointer so that omission can be told apart from false. It was a plain
	// bool, and an omitted flag decoded as false: a request that did not say
	// anything — as the documented examples do not — was an apply.
	DryRun *bool `json:"dry_run,omitzero"`
}

// dryRun reports whether the request is a preview: anything but an explicit
// false.
func (r MigrateInstancesRequest) dryRun() bool {
	return r.DryRun == nil || *r.DryRun
}

// MigrateInstancesResponse answers a MigrateInstancesRequest.
//
// It holds what the service answered, for whoever calls the endpoint in
// process. What goes out is written through views (MarshalJSON): the plan,
// the request that waits and each instance passed over leave as the route's
// own shapes, not as the entities happen to encode themselves.
//
// An apply that needs a second administrator is not made: the reply carries
// the plan, Applied false, nobody passed over and PendingApproval — the
// request that now waits — and is answered 202, not 200: the request was
// taken and nothing has been done. The same apply sent again by whoever made
// it is that answer again, a 202 still. Every other reply is the 200 it was.
type MigrateInstancesResponse struct {
	Plan entities.MigrationPlan `json:"plan"`
	// Applied says whether anything was written. False for a dry run, false
	// for an apply that the plan refused, and false for an apply that left
	// every instance it reached alone (PassedOver says which, and why).
	Applied bool `json:"applied"`
	// PassedOver are the instances the apply did not move because, by the time
	// it held their lock, they were no longer where the plan found them: the
	// step had been completed, the instance had finished, or it had gone on to
	// work the new version cannot take. Those are still on the version they
	// were running. So is an instance the plan was not made for, which arrived
	// on that version after the apply planned. One another run of a migration
	// had already moved is listed too, and is on the version that run put it
	// on. Always present, empty when the
	// apply left nobody behind and for a dry run, so a client need not ask
	// whether the field is there.
	PassedOver []PassedOverView `json:"passed_over"`
	// PendingApproval says the apply was not made but sent to a second
	// administrator: which request waits, who asked, until when and why it
	// needs somebody else. Left out of every reply that waits on nobody.
	PendingApproval *entities.PendingApproval `json:"pending_approval,omitzero"`
	Err             error                     `json:"err,omitzero"`
}

// PassedOverView is one instance an apply left alone, as the reply carries
// it: the instance, why as a code a client can translate (cause), the steps
// that is about (steps, by id and by name) and why in English words (reason).
// It is the view the approval of a migration lists them through, so the two
// replies cannot come to differ.
type PassedOverView = deviation.PassedOverView

// passedOverViews is the instances a run left alone as the reply lists them:
// an empty list, never null, when there are none.
func passedOverViews(passed []entities.PassedOverInstance) []PassedOverView {
	return deviation.PassedOverViewsOf(passed)
}

func (r MigrateInstancesResponse) Failed() error { return r.Err }

// StatusCode is 202 for a reply that says the apply waits for a second
// administrator, and 200 for every other. A refusal never gets here: the
// transport asks Failed first and answers with the status of its class.
func (r MigrateInstancesResponse) StatusCode() int {
	if r.PendingApproval != nil {
		return http.StatusAccepted
	}
	return http.StatusOK
}

// migrateInstancesReply is a MigrateInstancesResponse as it is written: the
// same fields under the same names in the same order, each through its view.
type migrateInstancesReply struct {
	Plan            deviation.MigrationPlanView    `json:"plan"`
	Applied         bool                           `json:"applied"`
	PassedOver      []PassedOverView               `json:"passed_over"`
	PendingApproval *deviation.PendingApprovalView `json:"pending_approval,omitzero"`
	Err             error                          `json:"err,omitzero"`
}

// MarshalJSON writes the reply through its views. It adds nothing and leaves
// nothing out: what a reply held before the views were there is written to
// the letter as it was.
func (r MigrateInstancesResponse) MarshalJSON() ([]byte, error) {
	reply := migrateInstancesReply{
		Plan:       deviation.MigrationPlanViewOf(r.Plan),
		Applied:    r.Applied,
		PassedOver: r.PassedOver,
		Err:        r.Err,
	}
	if r.PendingApproval != nil {
		view := deviation.PendingApprovalViewOf(*r.PendingApproval)
		reply.PendingApproval = &view
	}
	return json.Marshal(reply)
}
