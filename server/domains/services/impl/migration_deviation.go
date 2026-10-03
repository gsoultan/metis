package impl

import (
	"slices"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// decisionRecord is one node action a migration took on one instance — a skip,
// a cancel or a hold — with what it changed, so that its ledger row and its
// trail entry can be written from the same facts.
//
// instanceBefore and instanceAfter are the instance's status either side of a
// cancel, and empty otherwise; incidentID is the incident a hold raised, and nil
// otherwise.
type decisionRecord struct {
	instance                      models.ProcessInstanceModel
	definitionID                  uuid.UUID
	source, target                models.ProcessDefinitionModel
	nodeID                        string
	action                        servicecontracts.NodeAction
	options                       servicecontracts.MigrationOptions
	runID                         uuid.UUID
	withdrawn                     []models.TaskModel
	instanceBefore, instanceAfter string
	incidentID                    uuid.UUID
}

// decisionDeviation is the ledger row of a migration's node action. An action
// kind it does not know gives a row with no kind, which the ledger refuses as
// the writer's mistake rather than recording something unreadable.
func decisionDeviation(d decisionRecord, nodeName string) entities.Deviation {
	kind, scope := decisionKind(d.action.Kind)
	row := entities.Deviation{
		Project:    &entities.Project{ID: uuid.UUID(d.instance.ProjectID)},
		Instance:   &entities.ProcessInstance{ID: uuid.UUID(d.instance.ID)},
		Definition: &entities.ProcessDefinition{ID: d.definitionID},
		Kind:       kind,
		Scope:      scope,
		Origin:     entities.DeviationOriginMigration,
		Status:     entities.DeviationApplied,
		Node:       &entities.Node{ID: d.nodeID, Name: nodeName},
		Actor:      migrationActor(d.options),
		Reason:     d.action.Reason,
		RunID:      d.runID,
	}
	if len(d.withdrawn) == 1 {
		row.Task = &entities.Task{ID: uuid.UUID(d.withdrawn[0].ID)}
	}
	if len(d.withdrawn) > 0 {
		row.Details = map[string]any{"withdrawn": len(d.withdrawn)}
	}
	row.Before, row.After = withdrawnTaskValues(d.withdrawn)
	if d.instanceBefore != "" || d.instanceAfter != "" {
		row.Before["instance"] = map[string]any{"status": d.instanceBefore}
		row.After["instance"] = map[string]any{"status": d.instanceAfter}
	}
	if d.incidentID != uuid.Nil {
		row.After["incident"] = map[string]any{"id": d.incidentID.String(), "status": string(models.IncidentOpen)}
	}
	return row
}

// decisionKind is the ledger's name and reach for a migration node action: a
// skip waives one step's work, a cancel or a hold acts on the whole instance.
func decisionKind(kind servicecontracts.NodeActionKind) (entities.DeviationKind, entities.DeviationScope) {
	switch kind {
	case servicecontracts.NodeActionSkip:
		return entities.DeviationWaive, entities.DeviationScopeTask
	case servicecontracts.NodeActionCancel:
		return entities.DeviationCancel, entities.DeviationScopeInstance
	case servicecontracts.NodeActionHold:
		return entities.DeviationHold, entities.DeviationScopeInstance
	}
	return "", entities.DeviationScopeInstance
}

// withdrawnTaskValues is what withdrawing tasks changed on them, as they were
// and as they are, keyed by task id. Nobody holding a task is nil, not "". No
// tasks gives two empty maps, ready for the caller's other sections.
func withdrawnTaskValues(withdrawn []models.TaskModel) (before, after map[string]any) {
	before, after = map[string]any{}, map[string]any{}
	if len(withdrawn) == 0 {
		return before, after
	}
	was, is := make(map[string]any, len(withdrawn)), make(map[string]any, len(withdrawn))
	for _, task := range withdrawn {
		id := uuid.UUID(task.ID).String()
		was[id] = map[string]any{"status": string(task.Status), "assignee": holderValue(task.Assignee)}
		is[id] = map[string]any{"status": string(models.TaskCanceled)}
	}
	before["tasks"], after["tasks"] = was, is
	return before, after
}

// controlsNotPassed is the acknowledged control-bearing steps an instance loses
// in a migration: those it had not performed yet. An instance that passed the
// approval before the cutover waived nothing and must not read as though it
// did. completed is the instance's own, unmapped list.
func controlsNotPassed(holds []entities.ComplianceHold, completed []string) []entities.ComplianceHold {
	var lost []entities.ComplianceHold
	for _, hold := range holds {
		if slices.Contains(completed, hold.NodeID) {
			continue
		}
		lost = append(lost, hold)
	}
	return lost
}

// controlLossDeviations is one control_waived row per control step the
// instance loses. The system records them, because the person who
// acknowledged the loss signed for every instance at once: no reason is a
// person's here. Each row points at the instance_migrated entry that tells it.
func controlLossDeviations(
	instance models.ProcessInstanceModel,
	sourceDefID uuid.UUID,
	holds []entities.ComplianceHold,
	actor string,
	runID, entryID uuid.UUID,
) []entities.Deviation {
	rows := make([]entities.Deviation, 0, len(holds))
	for _, hold := range holds {
		row := entities.Deviation{
			Project:      &entities.Project{ID: uuid.UUID(instance.ProjectID)},
			Instance:     &entities.ProcessInstance{ID: uuid.UUID(instance.ID)},
			Definition:   &entities.ProcessDefinition{ID: sourceDefID},
			Kind:         entities.DeviationControlWaived,
			Scope:        entities.DeviationScopeInstance,
			Origin:       entities.DeviationOriginMigration,
			Status:       entities.DeviationApplied,
			Node:         &entities.Node{ID: hold.NodeID, Name: hold.Name},
			Actor:        actor,
			RunID:        runID,
			AuditEntryID: entryID,
		}
		if hold.Note != "" {
			row.Details = map[string]any{"note": hold.Note}
		}
		rows = append(rows, row)
	}
	return rows
}

// migrationActor is who authorised a migration, or "System" when nobody was
// named — what its trail entries have always said.
func migrationActor(options servicecontracts.MigrationOptions) string {
	if options.Actor == "" {
		return "System"
	}
	return options.Actor
}
