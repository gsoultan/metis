package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/redaction"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// holdWhereItStands raises an instance as an incident at the step it waits
// at, for somebody to decide, and changes nothing else about it: its tokens,
// its tasks and its status are as they were, and its work can still be done.
//
// A step that already has an incident open keeps that one — the effect raises
// no second — and the hold is recorded all the same, naming it: a hold in
// place is an act made for a visit, by somebody, for a reason, and that is
// what the ledger keeps. The same hold asked for again never gets here; apply
// answers it with its row first.
//
// Whether this hold raised the incident or found one is the effect's to say,
// and the row and the entry say what it said. The plan warned of an incident
// it read without holding it, and somebody may have resolved that one since:
// the hold then raised its own.
//
// What is written into the incident is the step, who held it and why, as
// anybody who may read the instance's incidents will read it: it goes through
// the redaction every incident's text goes through. When the incident was
// already there its text is not rewritten, and this hold's reason is in the
// ledger row and the trail entry only.
func (s *instanceDeviationService) holdWhereItStands(
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
	message := redaction.RedactText(fmt.Sprintf("held at “%s” by %s: %s", plan.NodeName, actor, command.Reason))
	incidentID, raised, err := s.actions.hold(ctx, locked, plan.NodeID, message)
	if err != nil {
		return entities.Deviation{}, effectFailed(fmt.Sprintf("holding this instance at “%s”", plan.NodeName), err)
	}

	row := inPlaceDeviation(locked, plan, command, actor, runID)
	row.After = map[string]any{"incident": map[string]any{"id": incidentID.String(), "status": string(models.IncidentOpen)}}
	row.Details = map[string]any{"incident_raised": raised}

	recorded, err := s.actions.record(ctx, row, holdEntry(locked, plan, command, actor, runID, incidentID, raised))
	if err != nil {
		return entities.Deviation{}, effectFailed(fmt.Sprintf("recording that this instance was held at “%s”", plan.NodeName), err)
	}
	return recorded, nil
}

// holdEntry is the trail entry of an instance held in place: the step it was
// held at, and the incident that holds it there — raised by this hold, or
// already open on the step, which the sentence says.
func holdEntry(
	locked models.ProcessInstanceModel,
	plan entities.DeviationPlan,
	command entities.DeviationCommand,
	actor string,
	runID, incidentID uuid.UUID,
	raised bool,
) entities.AuditEntry {
	narrative := fmt.Sprintf("This instance was held at “%s” by %s; the incident already open on that step stands. Reason: %s.",
		plan.NodeName, actor, command.Reason)
	if raised {
		narrative = fmt.Sprintf("This instance was held at “%s” by %s and raised as an incident for somebody to decide. Reason: %s.",
			plan.NodeName, actor, command.Reason)
	}
	return entities.AuditEntry{
		Type:      EventInstanceHeld,
		Message:   fmt.Sprintf("hold %s in place", plan.NodeID),
		Narrative: narrative,
		Timestamp: time.Now(),
		Data: map[string]any{
			"node_id":         plan.NodeID,
			"run_id":          runID.String(),
			"action":          string(servicecontracts.NodeActionHold),
			"origin":          string(entities.DeviationOriginInPlace),
			"reason":          command.Reason,
			"actor":           actor,
			"incident_id":     incidentID.String(),
			"incident_raised": raised,
		},
		Project:  &entities.Project{ID: uuid.UUID(locked.ProjectID)},
		Instance: &entities.ProcessInstance{ID: uuid.UUID(locked.ID)},
		Node:     &entities.Node{ID: plan.NodeID, Name: plan.NodeName},
	}
}
