package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	contracts "github.com/gsoultan/metis/server/domains/services/contracts"
	repcontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// Business Timeline event type constants. These are the human-friendly keys
// used in the Narrative lookup; they intentionally differ from technical BPMN
// event codes to make the audit feed readable to non-technical users.
const (
	EventTaskClaimed   = "task_claimed"
	EventTaskUnclaimed = "task_unclaimed"
	EventTaskCompleted = "task_completed"
	EventTaskAssigned  = "task_assigned"
	EventTaskDelegated = "task_delegated"
	// EventTaskResolved marks a delegated task handed back to its owner.
	EventTaskResolved = "task_resolved"
	// EventTaskEdited marks a change to a task's name, priority or due date,
	// with who made it and what each field was before and after.
	EventTaskEdited     = "task_edited"
	EventTaskEscalated  = "task_escalated"
	EventTaskCreated    = "task_created"
	EventProcessStarted = "process_started"
	EventProcessEnded   = "process_ended"
	EventProcessFailed  = "process_failed"
	EventNodeReached    = "node_reached"
	EventNodeCompleted  = "node_completed"
	// EventInstanceMigrated marks an instance that changed process version
	// while it was running.
	//
	// Without it the trail shows a task completed on a node the instance never
	// started on, and nothing explains how it got there. It also keeps the
	// conformance story honest: a trace that is part one version and part
	// another is not a deviation, but only this event can say so.
	EventInstanceMigrated = "instance_migrated"
	// EventNodeSkipped marks a step the instance was moved past without anybody
	// performing it: one a migration skipped, or one an administrator waived
	// where the instance stood (its data then says outcome: waived). It is the
	// entry that stops a skipped approval from reading like an approval
	// somebody gave.
	EventNodeSkipped = "node_skipped"
	// EventInstanceCancelled marks an instance somebody ended before it
	// finished: a migration that ended it rather than moved it, or an
	// administrator who cancelled it where it stood.
	EventInstanceCancelled = "instance_cancelled"
	// EventInstanceHeld marks an instance raised as an incident for a person
	// to decide: one a migration deliberately left behind, or one an
	// administrator held where it stood.
	EventInstanceHeld = "instance_held"
	// EventParkedWorkWithdrawn marks work a step inside an ad-hoc
	// sub-process had parked for outside workers, taken back when the
	// sub-process finished without it.
	//
	// Withdrawing that work removes its row, which is also what completing it
	// does. Without this entry nothing on the instance tells the two apart, and
	// a worker told there is no such task has nowhere to find out why.
	EventParkedWorkWithdrawn = "parked_work_withdrawn"
	// EventStepActivated marks a step somebody started inside an ad-hoc
	// sub-process.
	//
	// The step's own entries say that it started and that its task became
	// available, as they do for a step the flow reached. Inside an ad-hoc
	// sub-process no flow reaches a step: somebody chose it. This entry names
	// who, and the reason when they gave one.
	EventStepActivated = "step_activated"
	// EventDeviationRequested marks a deviation one administrator asked for
	// that waits for a second: nothing about the instance has changed, and
	// the entry says so.
	EventDeviationRequested = "deviation_requested"
	// EventDeviationApproved marks a request a second administrator approved.
	// It stands beside the entry of what was then done — a waived step's
	// node_skipped — and is where the trail says who approved: that entry's
	// own sentence does not, so that it never reads as the step's approval.
	EventDeviationApproved = "deviation_approved"
	// EventDeviationSelfApproved marks a request approved by whoever asked for
	// it, in an installation that allows a sole administrator to. It says in
	// so many words that no second person approved.
	EventDeviationSelfApproved = "deviation_self_approved"
	// EventDeviationRejected marks a request somebody rejected, or its
	// requester withdrew. Nothing was changed.
	EventDeviationRejected = "deviation_rejected"
	// EventDeviationExpired marks a request nobody decided before its
	// deadline. Nothing was changed.
	EventDeviationExpired = "deviation_expired"
	// EventDeviationStale marks a request that no longer held when somebody
	// came to approve it: the instance had moved, or ended. Nothing was
	// changed.
	EventDeviationStale = "deviation_stale"
)

// auditWriter is the default AuditWriter implementation. It enriches each
// AuditEntry with a human-readable Narrative before persisting it.
type auditWriter struct {
	repo repcontracts.AuditRepository
}

// NewAuditWriter returns an AuditWriter backed by the given audit repository.
func NewAuditWriter(repo repcontracts.AuditRepository) contracts.AuditWriter {
	return &auditWriter{repo: repo}
}

// RecordEvent enriches entry with a human-readable Narrative (when absent)
// and persists it via the audit repository.
func (w *auditWriter) RecordEvent(ctx context.Context, entry entities.AuditEntry) error {
	if entry.Narrative == "" {
		if told, ok := handOverNarrative(entry); ok {
			entry.Narrative = told
		} else {
			entry.Narrative = narrativeFor(entry.Type, subjectName(entry), actorName(entry))
		}
	}

	m := toAuditModel(entry)
	if err := w.repo.Create(ctx, m); err != nil {
		return fmt.Errorf("audit writer: failed to persist event %q: %w", entry.Type, err)
	}
	return nil
}

// narrativeFor returns a plain-English Business Timeline sentence for a given
// event type, task/node subject name, and actor (user) name. It is a pure
// function and safe to test in isolation.
func narrativeFor(eventType, subject, actor string) string {
	switch eventType {
	case EventTaskClaimed:
		return fmt.Sprintf("%s claimed task %q", actor, subject)
	case EventTaskUnclaimed:
		return fmt.Sprintf("Task %q was released back to the queue", subject)
	case EventTaskCompleted:
		return fmt.Sprintf("%s completed task %q", actor, subject)
	case EventTaskAssigned:
		return fmt.Sprintf("Task %q was assigned to %s", subject, actor)
	case EventTaskDelegated:
		return fmt.Sprintf("Task %q was delegated to %s", subject, actor)
	case EventTaskEscalated:
		return fmt.Sprintf("Task %q was escalated to %s", subject, actor)
	case EventTaskCreated:
		return fmt.Sprintf("Task %q became available", subject)
	case EventProcessStarted:
		return fmt.Sprintf("Process %q was started", subject)
	case EventProcessEnded:
		return fmt.Sprintf("Process %q completed successfully", subject)
	case EventProcessFailed:
		return fmt.Sprintf("Process %q failed", subject)
	case EventNodeReached:
		return fmt.Sprintf("Step %q started", subject)
	case EventNodeCompleted:
		return fmt.Sprintf("Step %q finished", subject)
	case EventInstanceMigrated:
		return "This instance was moved onto another version of the process by a migration"
	case EventNodeSkipped:
		return fmt.Sprintf("Step %q was skipped without being performed", subject)
	case EventInstanceCancelled:
		return "This instance was ended before it finished"
	case EventInstanceHeld:
		return "This instance was held for somebody to decide"
	default:
		if subject != "" {
			return fmt.Sprintf("Event %q occurred on %q", eventType, subject)
		}
		return fmt.Sprintf("Event %q occurred", eventType)
	}
}

// actorName returns the display name of the actor from the entry's Data map,
// falling back to "System" when no actor is present.
func actorName(entry entities.AuditEntry) string {
	if v, ok := entry.Data["actor"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return "System"
}

// subjectName returns the task or node name from the entry, preferring the
// node name over a generic fallback.
func subjectName(entry entities.AuditEntry) string {
	if entry.Node != nil && entry.Node.Name != "" {
		return entry.Node.Name
	}
	return "unknown"
}

// toAuditModel converts an AuditEntry entity to the persistence model.
func toAuditModel(e entities.AuditEntry) models.AuditModel {
	m := models.AuditModel{
		Type:      e.Type,
		Message:   e.Message,
		Narrative: e.Narrative,
		Data:      e.Data,
	}
	m.ID = models.UUID(e.ID)
	if m.ID == models.NilUUID {
		m.ID = models.UUID(uuid.New())
	}
	m.CreatedAt = e.Timestamp
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now()
	}
	if e.Project != nil {
		m.ProjectID = models.UUID(e.Project.ID)
	}
	if e.Instance != nil {
		m.InstanceID = models.UUID(e.Instance.ID)
	}
	if e.Node != nil {
		m.NodeID = e.Node.ID
		m.NodeName = e.Node.Name
	}
	return m
}
