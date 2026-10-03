package impl

import (
	"context"
	"fmt"

	"github.com/gsoultan/metis/server/domains/entities"
)

// handOverRecord is what the trail keeps about one hand-over or edit: who
// asked, who had the task, who has it now, and why.
type handOverRecord struct {
	// actor is the caller — never the person the task went to.
	actor string
	// previousHolder is who held the task before it moved.
	previousHolder string
	// holder is who holds a task that was edited rather than moved.
	holder string
	// target is who it went to.
	target string
	// owner is who a delegated task goes back to.
	owner  string
	reason string
	// candidateOverride: an administrator sent it to somebody it was not
	// offered to.
	candidateOverride bool
	// changes are an edit's fields, each with what it was and what it is.
	changes map[string]any
}

// data is the record as an audit entry keeps it. A part that does not apply is
// left out rather than stored empty, so "who had it" being absent means nobody
// did.
func (r handOverRecord) data() map[string]any {
	data := map[string]any{auditActor: r.actor}
	for key, value := range map[string]string{
		auditPreviousHolder: r.previousHolder,
		auditHolder:         r.holder,
		auditTarget:         r.target,
		auditOwner:          r.owner,
		auditReason:         r.reason,
	} {
		if value != "" {
			data[key] = value
		}
	}
	if r.candidateOverride {
		data[auditCandidateOverride] = true
	}
	if len(r.changes) > 0 {
		data[auditChanges] = r.changes
	}
	return data
}

// announceHandOver writes a hand-over's audit entry and then raises its event.
//
// The entry first, and its failure is the hand-over's: it is written in the
// transaction that moves the task, so returning the error undoes the move. The
// other task actions log a lost entry and carry on (announce); a task changing
// hands with nothing to say who moved it is the thing this entry exists to
// prevent, so here there is no carrying on.
func (s *taskService) announceHandOver(ctx context.Context, event entities.ProcessEvent, task entities.Task, auditType string, record handOverRecord) error {
	if s.auditWriter != nil {
		if err := s.auditWriter.RecordEvent(ctx, entities.AuditEntry{
			Type:     auditType,
			Project:  task.Project,
			Instance: task.Instance,
			Node:     namedNode(task),
			Data:     record.data(),
		}); err != nil {
			return fmt.Errorf("the change was not recorded in the audit trail, so it was not made: %w", err)
		}
	}
	event.Audited = s.auditWriter != nil
	s.engine.DispatchEvent(ctx, event)
	return nil
}
