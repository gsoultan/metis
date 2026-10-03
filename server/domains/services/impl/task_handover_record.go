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
	// byHolder: the caller held the task when the change was decided, under
	// its row lock. Only then may it be made without a reason, and only then
	// is it no deviation.
	byHolder bool
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

// announceHandOver writes a hand-over's ledger row when it is a deviation
// (needsLedger), then its audit entry, and then raises its event.
//
// The row and the entry are written in the transaction that moves the task,
// and name each other. A row that cannot be written is the hand-over's failure
// too: a change by somebody other than the holder that the ledger does not
// show is the thing the ledger exists to prevent.
//
// The entry before the event, and its failure is the hand-over's: it is
// written in the transaction that moves the task, so returning the error undoes
// the move. The other task actions log a lost entry and carry on (announce); a
// task changing hands with nothing to say who moved it is the thing this entry
// exists to prevent, so here there is no carrying on.
func (s *taskService) announceHandOver(ctx context.Context, event entities.ProcessEvent, task entities.Task, auditType string, record handOverRecord) error {
	entry := entities.AuditEntry{
		Type:     auditType,
		Project:  task.Project,
		Instance: task.Instance,
		Node:     namedNode(task),
		Data:     record.data(),
	}
	if record.needsLedger() {
		if err := s.recordHandOverDeviation(ctx, task, auditType, record, &entry); err != nil {
			return fmt.Errorf("the change was not recorded in the deviation ledger, so it was not made: %w", err)
		}
	}
	if s.auditWriter != nil {
		if err := s.auditWriter.RecordEvent(ctx, entry); err != nil {
			return fmt.Errorf("the change was not recorded in the audit trail, so it was not made: %w", err)
		}
	}
	event.Audited = s.auditWriter != nil
	s.engine.DispatchEvent(ctx, event)
	return nil
}
