package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// cancelWhereItStands ends an instance as it stands: every task it has open
// is withdrawn and whoever held it told, the work it had parked for outside
// workers is taken back, what it was waiting for is dropped, the incidents
// open on it are closed, and it is cancelled — and the ledger and the trail
// say who ended it, where it stood and why.
//
// It ends the whole instance, whichever step the command names; the step is
// where the record says the instance stood. A command that names none closes
// an instance that waits nowhere, which is what apply asked of the locked row,
// and its row and its entry name no step: none is invented for either.
//
// The record is made from what the effect did, and counts it: the tasks it
// held and withdrew, the parked work it took back, the incidents it closed.
// The plan lists no more than a screenful of the instance's open work, read
// without its rows held; who holds a task may have changed since, and there
// may be more of them than it lists.
//
// The row names the first maxRecordedInPlace tasks, by id, and as many
// incidents. Past that it does not name every holder: the holder of a task it
// does not name is on the task's own row, which a withdrawal changes only the
// status of, and in the notice sent to them.
func (s *instanceDeviationService) cancelWhereItStands(
	ctx context.Context,
	locked models.ProcessInstanceModel,
	plan entities.DeviationPlan,
	command entities.DeviationCommand,
	actor string,
) (entities.Deviation, error) {
	runID, err := uuid.NewV7()
	if err != nil {
		return entities.Deviation{}, err
	}
	done, err := s.actions.cancel(ctx, locked)
	if err != nil {
		return entities.Deviation{}, effectFailed("cancelling this instance", err)
	}

	row := inPlaceDeviation(locked, plan, command, actor, runID)
	// As a migration's cancel names it: the one task it took, when it took one.
	if len(done.withdrawn) == 1 {
		row.Task = &entities.Task{ID: uuid.UUID(done.withdrawn[0].ID)}
	}
	named := lowestByID(done.withdrawn, maxRecordedInPlace)
	row.Before, row.After = withdrawnTaskValues(named)
	row.Before["instance"] = map[string]any{"status": string(locked.Status)}
	row.After["instance"] = map[string]any{"status": string(done.instance.Status)}
	if was, is := closedIncidentValues(done.incidentsClosed, maxRecordedInPlace); len(was) > 0 {
		row.Before["incidents"], row.After["incidents"] = was, is
	}
	// Counts, and no business value: the details of a row are not sealed.
	row.Details = map[string]any{
		"withdrawn":           len(done.withdrawn),
		"tasks_listed":        len(named),
		detailParkedWithdrawn: done.parkedWithdrawn,
		detailIncidentsClosed: len(done.incidentsClosed),
	}

	recorded, err := s.actions.record(ctx, row, cancelEntry(locked, plan, command, actor, runID, done.parkedWithdrawn, len(done.incidentsClosed)))
	if err != nil {
		return entities.Deviation{}, effectFailed("recording that this instance was cancelled", err)
	}
	return recorded, nil
}

// closedIncidentValues is what closing incidents changed on them, as they
// were and as they are, keyed by incident id: the first limit of them. ids are
// in order already. No incidents gives two empty maps.
func closedIncidentValues(ids []uuid.UUID, limit int) (was, is map[string]any) {
	ids = ids[:min(len(ids), limit)]
	was, is = make(map[string]any, len(ids)), make(map[string]any, len(ids))
	for _, id := range ids {
		was[id.String()] = map[string]any{"status": string(models.IncidentOpen)}
		is[id.String()] = map[string]any{"status": string(models.IncidentResolved)}
	}
	return was, is
}

// cancelEntry is the trail entry of an instance ended in place. It says where
// the instance stood when the command names a step, and that it stood nowhere
// when it names none: the entry then carries no step and no node_id, as its
// ledger row carries none. It counts the work parked for outside workers that
// the cancel took back and the incidents it closed, as the row's details do;
// which incidents they were is on the row.
func cancelEntry(
	locked models.ProcessInstanceModel,
	plan entities.DeviationPlan,
	command entities.DeviationCommand,
	actor string,
	runID uuid.UUID,
	parkedWithdrawn, incidentsClosed int,
) entities.AuditEntry {
	entry := entities.AuditEntry{
		Type:    EventInstanceCancelled,
		Message: "cancel in place",
		Narrative: fmt.Sprintf("This instance was ended by %s while it was not waiting at any step. Reason: %s.",
			actor, command.Reason),
		Timestamp: time.Now(),
		Data: map[string]any{
			"run_id":              runID.String(),
			"action":              string(servicecontracts.NodeActionCancel),
			"origin":              string(entities.DeviationOriginInPlace),
			"reason":              command.Reason,
			"actor":               actor,
			detailParkedWithdrawn: parkedWithdrawn,
			detailIncidentsClosed: incidentsClosed,
		},
		Project:  &entities.Project{ID: uuid.UUID(locked.ProjectID)},
		Instance: &entities.ProcessInstance{ID: uuid.UUID(locked.ID)},
	}
	if plan.NodeID == "" {
		return entry
	}
	entry.Message = fmt.Sprintf("cancel %s in place", plan.NodeID)
	entry.Narrative = fmt.Sprintf("This instance was ended at “%s” by %s. Reason: %s.", plan.NodeName, actor, command.Reason)
	entry.Data["node_id"] = plan.NodeID
	entry.Node = &entities.Node{ID: plan.NodeID, Name: plan.NodeName}
	return entry
}
