package pg

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/store/instancedeviation"
	"github.com/gsoultan/storm/runtime"
)

// emptyObject is what the ledger stores for a map with nothing in it.
//
// before, after and details are NOT NULL, and an absent map is exactly what a
// hold or a cancel has to record. jsonOf and sealedJSONOf answer nil for an
// empty map, which other tables want as NULL; here nil would refuse the row, or
// store a JSON null that reads back as no map at all.
var emptyObject = runtime.JSON(`{}`)

// deviationSealed encodes before or after: the business data of the change,
// sealed like every other copy of it.
func deviationSealed(m map[string]any) (runtime.JSON, error) {
	if len(m) == 0 {
		return emptyObject, nil
	}
	return sealedJSONOf(m)
}

// deviationDetails encodes details, which is not business data.
func deviationDetails(m map[string]any) (runtime.JSON, error) {
	if len(m) == 0 {
		return emptyObject, nil
	}
	return jsonOf(m)
}

// stageDeviation sets every column of d. A column left unset would take its
// default, and an absent value is written as NULL on purpose, never inferred.
func stageDeviation(d entities.Deviation, projectID, instanceID uuid.UUID) (instancedeviation.Ins, error) {
	before, err := deviationSealed(d.Before)
	if err != nil {
		return instancedeviation.Ins{}, fmt.Errorf("could not encode the deviation's before: %w", err)
	}
	after, err := deviationSealed(d.After)
	if err != nil {
		return instancedeviation.Ins{}, fmt.Errorf("could not encode the deviation's after: %w", err)
	}
	details, err := deviationDetails(d.Details)
	if err != nil {
		return instancedeviation.Ins{}, fmt.Errorf("could not encode the deviation's details: %w", err)
	}

	ins := instancedeviation.Create()
	if d.ID != uuid.Nil {
		ins.SetID(d.ID)
	}
	ins.SetProjectID(projectID)
	ins.SetInstanceID(instanceID)
	ins.SetKind(string(d.Kind))
	ins.SetScope(string(d.Scope))
	ins.SetOrigin(string(d.Origin))
	ins.SetStatus(string(d.Status))
	ins.SetActor(d.Actor)
	ins.SetBefore(before)
	ins.SetAfter(after)
	ins.SetDetails(details)
	ins.SetRunID(d.RunID)
	stageDeviationOptionals(&ins, d)
	return ins, nil
}

func stageDeviationOptionals(ins *instancedeviation.Ins, d entities.Deviation) {
	if d.Definition != nil && d.Definition.ID != uuid.Nil {
		ins.SetDefinitionID(d.Definition.ID)
	} else {
		ins.SetDefinitionIDNull()
	}
	var nodeID, nodeName string
	if d.Node != nil {
		nodeID, nodeName = d.Node.ID, d.Node.Name
	}
	setOrNullString(ins.SetNodeID, ins.SetNodeIDNull, nodeID)
	setOrNullString(ins.SetNodeName, ins.SetNodeNameNull, nodeName)
	if d.Task != nil && d.Task.ID != uuid.Nil {
		ins.SetTaskID(d.Task.ID)
	} else {
		ins.SetTaskIDNull()
	}
	setOrNullString(ins.SetIterationID, ins.SetIterationIDNull, d.IterationID)
	setOrNullUUID(ins.SetActorID, ins.SetActorIDNull, d.ActorID)
	setOrNullString(ins.SetReason, ins.SetReasonNull, d.Reason)
	setOrNullUUID(ins.SetAuditEntryID, ins.SetAuditEntryIDNull, d.AuditEntryID)
	setOrNullString(ins.SetVisitKey, ins.SetVisitKeyNull, d.VisitKey)
	setOrNullString(ins.SetLiveVisitKey, ins.SetLiveVisitKeyNull, liveVisitKey(d))
	setOrNullUUID(ins.SetRequestID, ins.SetRequestIDNull, d.RequestID)
	setOrNullString(ins.SetApprovedBy, ins.SetApprovedByNull, d.ApprovedBy)
	setOrNullUUID(ins.SetApprovedByID, ins.SetApprovedByIDNull, d.ApprovedByID)
	if d.DecidedAt != nil {
		ins.SetDecidedAt(d.DecidedAt.UTC())
	} else {
		ins.SetDecidedAtNull()
	}
}

// liveVisitKey is what holds a visit: the row's visit key while the row is
// applied or awaiting approval, and nothing once it is rejected, expired or
// stale. The ledger is unique on it per instance, so a second live row for a
// visit is refused and a decided one no longer refuses a new request.
func liveVisitKey(d entities.Deviation) string {
	if !d.Status.Live() {
		return ""
	}
	return d.VisitKey
}

func setOrNullUUID(set func([16]byte), setNull func(), id uuid.UUID) {
	if id == uuid.Nil {
		setNull()
		return
	}
	set(id)
}

// deviationFrom is the one decoder of a ledger row.
//
// An empty before, after or details reads as an empty map, never nil: the
// column is NOT NULL, so there is always something there, and a reader that
// ranges over it or indexes into it should not have to ask which kind of empty
// it got.
func deviationFrom(row instancedeviation.Row) (entities.Deviation, error) {
	before, err := sealedMapOf(row.Before)
	if err != nil {
		return entities.Deviation{}, fmt.Errorf("could not decode a deviation's before: %w", err)
	}
	after, err := sealedMapOf(row.After)
	if err != nil {
		return entities.Deviation{}, fmt.Errorf("could not decode a deviation's after: %w", err)
	}
	details, err := mapOf(row.Details)
	if err != nil {
		return entities.Deviation{}, fmt.Errorf("could not decode a deviation's details: %w", err)
	}
	d := entities.Deviation{
		ID:           row.ID,
		CreatedAt:    row.CreatedAt,
		Project:      &entities.Project{ID: row.ProjectID},
		Instance:     &entities.ProcessInstance{ID: row.InstanceID},
		Kind:         entities.DeviationKind(row.Kind),
		Scope:        entities.DeviationScope(row.Scope),
		Origin:       entities.DeviationOrigin(row.Origin),
		Status:       entities.DeviationStatus(row.Status),
		IterationID:  valueOr(row.IterationID),
		Actor:        row.Actor,
		Reason:       valueOr(row.Reason),
		Before:       orEmpty(before),
		After:        orEmpty(after),
		Details:      orEmpty(details),
		RunID:        row.RunID,
		VisitKey:     valueOr(row.VisitKey),
		ApprovedBy:   valueOr(row.ApprovedBy),
		ActorID:      uuidOr(row.ActorID),
		AuditEntryID: uuidOr(row.AuditEntryID),
		RequestID:    uuidOr(row.RequestID),
		ApprovedByID: uuidOr(row.ApprovedByID),
	}
	if id, ok := row.DefinitionID.Get(); ok {
		d.Definition = &entities.ProcessDefinition{ID: id}
	}
	if id, ok := row.TaskID.Get(); ok {
		d.Task = &entities.Task{ID: id}
	}
	nodeID, nodeName := valueOr(row.NodeID), valueOr(row.NodeName)
	if nodeID != "" || nodeName != "" {
		d.Node = &entities.Node{ID: nodeID, Name: nodeName}
	}
	if at, ok := row.DecidedAt.Get(); ok {
		d.DecidedAt = &at
	}
	return d, nil
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func uuidOr(v runtime.Null[[16]byte]) uuid.UUID {
	id, _ := v.Get()
	return id
}
