package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

// DefinitionInstanceCount is how much work one version of a process still
// holds. Running is what has to finish before that version has fully drained;
// Suspended is stopped but not finished, and will run again when resumed;
// Total is every instance it has ever had.
type DefinitionInstanceCount struct {
	Running   int64
	Suspended int64
	Total     int64
}

// InstanceFilter narrows a page of instances to the ones somebody asked for.
//
// It is applied in the database rather than to the page that came back. The
// difference is the whole point: a project with 500,000 instances and twelve
// failures cannot surface those twelve by filtering twenty-five rows, and a
// control that appears to search the project while searching only the screen
// reports "nothing is wrong" when something is.
//
// The zero value narrows nothing, so every existing caller keeps its meaning.
type InstanceFilter struct {
	// Status is one of the four lifecycle states. Empty means every state.
	//
	// Validated at the edge against models.ProcessStatuses rather than here:
	// by this point it is a value the caller chose, and a repository that
	// silently drops an unrecognised one would turn a typo into a full list.
	Status models.ProcessStatus
	// DefinitionID narrows to runs of one process. uuid.Nil means every process.
	DefinitionID uuid.UUID
	// BusinessKey narrows to the instances started for one case, matched
	// exactly. Empty means every instance.
	BusinessKey string
	// NeedsAttention narrows to instances holding an unresolved incident.
	//
	// Deliberately not a value of Status. An instance with an incident is still
	// `active` as far as the engine is concerned — it has not failed, it is
	// waiting for a person — and conflating the two would either invent a state
	// the engine never writes or hide the one it does.
	NeedsAttention bool
}

// Any reports whether the filter narrows anything at all.
func (f InstanceFilter) Any() bool {
	return f.Status != "" || f.DefinitionID != uuid.Nil || f.NeedsAttention
}

// ProcessRepository defines the BPM process instance operations.
// WaitingRow is how much work sits on one step of one process, or — with an
// empty NodeID — on the whole process.
type WaitingRow struct {
	ProcessKey  string
	ProcessName string
	NodeID      string
	Waiting     int64
	Instances   int64
}

type ProcessRepository interface {
	// ForgetFinished deletes the instances that completed or were cancelled
	// longer ago than their process's retention period, or fallback where the
	// process has none, and that nothing still needs, with the rows that exist
	// only to run them. A zero fallback keeps the instances of every process
	// that has no period of its own. The audit trail and any deviation record
	// are kept. System context only: it spans every tenant.
	ForgetFinished(ctx context.Context, now time.Time, fallback time.Duration) (int64, error)
	// Existing answers which of ids have an instance row, in every tenant:
	// what the payload sweep asks before it removes an instance's kept
	// values. System context only.
	Existing(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error)

	// WaitingByStep counts the live tokens of running instances per process
	// and step. It is scoped to the caller's tenant like every project read.
	WaitingByStep(ctx context.Context, projectID uuid.UUID) ([]WaitingRow, error)

	Create(ctx context.Context, instance models.ProcessInstanceModel) (uuid.UUID, error)
	Get(ctx context.Context, id uuid.UUID) (models.ProcessInstanceModel, error)
	GetForUpdate(ctx context.Context, id uuid.UUID) (models.ProcessInstanceModel, error)
	Update(ctx context.Context, instance models.ProcessInstanceModel) error
	List(ctx context.Context) ([]models.ProcessInstanceModel, error)
	ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.ProcessInstanceModel, error)

	// Paged variants for the instance list a user browses.
	ListPaged(ctx context.Context, f InstanceFilter, p Pagination) (Page[models.ProcessInstanceModel], error)
	ListByProjectPaged(ctx context.Context, projectID uuid.UUID, f InstanceFilter, p Pagination) (Page[models.ProcessInstanceModel], error)

	// ListPageOfTotal is ListByProjectPaged for a caller that has counted the
	// instances f matches already, from CountByStatuses: total is taken as the
	// page's Total and not counted again. uuid.Nil lists across the tenant, as
	// ListPaged does. A filter asking for NeedsAttention is counted, since the
	// status counts do not describe it.
	ListPageOfTotal(ctx context.Context, projectID uuid.UUID, f InstanceFilter, p Pagination, total int64) (Page[models.ProcessInstanceModel], error)
	ListByDefinition(ctx context.Context, definitionID uuid.UUID) ([]models.ProcessInstanceModel, error)
	ListByParent(ctx context.Context, parentInstanceID uuid.UUID) ([]models.ProcessInstanceModel, error)

	// ScanByStatus walks every instance in one state, a batch at a time, for
	// work that has to reach all of them without holding all of them at once.
	ScanByStatus(ctx context.Context, status models.ProcessStatus, visit func([]models.ProcessInstanceModel) error) error
	CountByStatus(ctx context.Context, projectID uuid.UUID, status models.ProcessStatus) (int64, error)

	// OpenIncidentsByInstance reports how many unresolved incidents each of the
	// given instances holds, in one grouped query.
	//
	// The list needs this because an instance's status never says it. A job that
	// exhausts its retries raises an incident and stops; the instance stays
	// `active`, which is right — it has not failed, it is waiting for somebody.
	// Nothing in the engine ever writes models.ProcessFailed, so a list that
	// looked for it would mark nothing and report a healthy project for ever.
	//
	// Takes ids rather than a project so the query is bounded by the page: the
	// caller has just listed the rows it is about to draw.
	OpenIncidentsByInstance(ctx context.Context, instanceIDs []uuid.UUID) (map[uuid.UUID]int64, error)

	// CountInstancesNeedingAttention counts the project's instances that hold at
	// least one unresolved incident.
	//
	// Across the project rather than the page, for the same reason the status
	// counts are: the question "is anything broken?" is not a question about
	// the twenty-five rows that happen to be on screen.
	CountInstancesNeedingAttention(ctx context.Context, projectID uuid.UUID, f InstanceFilter) (int64, error)

	// CountByStatuses reports how many instances the project holds in each
	// state, in one grouped query.
	//
	// This is what lets a list of twenty-five rows say "12 need attention"
	// truthfully, and offer the filter that reaches them.
	//
	// Every field of the filter is applied, including Status — which is why a
	// caller drawing a chip per state must clear it first. Counting the state
	// already selected reports that state's own total and zero for the rest,
	// which is a true answer to a question nobody asked.
	CountByStatuses(ctx context.Context, projectID uuid.UUID, f InstanceFilter) (map[models.ProcessStatus]int64, error)

	// CountInstancesByDefinitions reports how many instances each of the given
	// definition versions holds, in one grouped query rather than one per
	// version.
	//
	// It takes IDs rather than a process key because the caller has just listed
	// the versions, and because `key` is reserved on MySQL — keeping it out of
	// the predicate keeps this a plain IN over an indexed column.
	CountInstancesByDefinitions(ctx context.Context, definitionIDs []uuid.UUID) (map[uuid.UUID]DefinitionInstanceCount, error)
}
