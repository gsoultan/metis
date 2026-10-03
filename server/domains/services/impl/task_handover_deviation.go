package impl

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// auditDeviationID is the key under which an audit entry names the ledger row
// that records the same act. The row names the entry back (AuditEntryID).
const auditDeviationID = "deviation_id"

// needsLedger reports whether the hand-over or edit is a deviation: exactly
// when slice 2 asked its caller for a reason. Somebody handing on their own
// task deviates from nothing; anybody else does, and so does an administrator
// holder who sends it to somebody it was not offered to.
func (r handOverRecord) needsLedger() bool {
	return !r.byHolder || r.candidateOverride
}

// recordHandOverDeviation writes the hand-over's ledger row in the hand-over's
// transaction, and makes the entry about to be written and the row name each
// other: the entry is given its id here, so the row can point at it before it
// exists.
func (s *taskService) recordHandOverDeviation(ctx context.Context, task entities.Task, auditType string, record handOverRecord, entry *entities.AuditEntry) error {
	auditID, err := uuid.NewV7()
	if err != nil {
		return err
	}
	d := handOverDeviation(task, auditType, record)
	d.AuditEntryID = auditID
	recorded, err := s.ledger.Record(ctx, d)
	if err != nil {
		return err
	}
	entry.ID = auditID
	entry.Data[auditDeviationID] = recorded.ID.String()
	return nil
}

// handOverDeviation is the ledger row of a hand-over or edit of task. An audit
// type that is not a hand-over gives a row with no kind, which the ledger
// refuses as the writer's mistake rather than recording something unreadable.
func handOverDeviation(task entities.Task, auditType string, record handOverRecord) entities.Deviation {
	d := entities.Deviation{
		Kind:   handOverDeviationKind(auditType),
		Scope:  entities.DeviationScopeTask,
		Origin: entities.DeviationOriginTask,
		Status: entities.DeviationApplied,
		Node:   &entities.Node{ID: task.NodeID()},
		Task:   &entities.Task{ID: task.ID},
		Actor:  record.actor,
		Reason: record.reason,
	}
	if named := namedNode(task); named != nil {
		d.Node.Name = named.Name
	}
	if task.Project != nil {
		d.Project = &entities.Project{ID: task.Project.ID}
	}
	if task.Instance != nil {
		d.Instance = &entities.ProcessInstance{ID: task.Instance.ID}
	}
	if task.IterationID != "" {
		d.Scope, d.IterationID = entities.DeviationScopeIteration, task.IterationID
	}
	if record.candidateOverride {
		d.Details = map[string]any{"override": "not_a_candidate"}
	}
	before, after := handOverValues(auditType, record)
	d.Before = map[string]any{"tasks": map[string]any{task.ID.String(): before}}
	d.After = map[string]any{"tasks": map[string]any{task.ID.String(): after}}
	return d
}

// handOverDeviationKind is the ledger's name for a hand-over's audit type, or
// "" for one that is not a hand-over.
func handOverDeviationKind(auditType string) entities.DeviationKind {
	switch auditType {
	case EventTaskAssigned:
		return entities.DeviationReassign
	case EventTaskDelegated:
		return entities.DeviationDelegate
	case EventTaskResolved:
		return entities.DeviationResolve
	case EventTaskUnclaimed:
		return entities.DeviationRelease
	case EventTaskEdited:
		return entities.DeviationTaskEdit
	}
	return ""
}

// handOverValues is what the hand-over changed on the task, as it was and as it
// is. Nobody holding the task is nil, not "".
func handOverValues(auditType string, record handOverRecord) (before, after map[string]any) {
	switch auditType {
	case EventTaskAssigned:
		return map[string]any{"assignee": holderValue(record.previousHolder)},
			map[string]any{"assignee": holderValue(record.target)}
	case EventTaskDelegated:
		return map[string]any{"assignee": holderValue(record.previousHolder)},
			map[string]any{
				"assignee": holderValue(record.target), "owner": holderValue(record.owner),
				"delegation_state": string(entities.DelegationPending),
			}
	case EventTaskResolved:
		return map[string]any{"assignee": holderValue(record.previousHolder), "delegation_state": string(entities.DelegationPending)},
			map[string]any{"assignee": holderValue(record.target), "delegation_state": string(entities.DelegationResolved)}
	case EventTaskUnclaimed:
		return map[string]any{"assignee": holderValue(record.previousHolder)}, map[string]any{"assignee": nil}
	case EventTaskEdited:
		return editValues(record.changes)
	}
	return map[string]any{}, map[string]any{}
}

// editValues splits an edit's changes, each field with what it was and what it
// is (fieldChange), into the fields as they were and as they are.
func editValues(changes map[string]any) (before, after map[string]any) {
	before, after = make(map[string]any, len(changes)), make(map[string]any, len(changes))
	for field, change := range changes {
		pair, ok := change.(map[string]any)
		if !ok {
			continue
		}
		before[field], after[field] = pair["before"], pair["after"]
	}
	return before, after
}

func holderValue(username string) any {
	if username == "" {
		return nil
	}
	return username
}
