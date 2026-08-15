package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	contracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
	repcontracts "github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

// Business Timeline event type constants. These are the human-friendly keys
// used in the Narrative lookup; they intentionally differ from technical BPMN
// event codes to make the audit feed readable to non-technical users.
const (
	EventTaskClaimed    = "task_claimed"
	EventTaskUnclaimed  = "task_unclaimed"
	EventTaskCompleted  = "task_completed"
	EventTaskAssigned   = "task_assigned"
	EventTaskDelegated  = "task_delegated"
	EventTaskEscalated  = "task_escalated"
	EventTaskCreated    = "task_created"
	EventProcessStarted = "process_started"
	EventProcessEnded   = "process_ended"
	EventProcessFailed  = "process_failed"
	EventNodeReached    = "node_reached"
	EventNodeCompleted  = "node_completed"
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
		actor := actorName(entry)
		subject := subjectName(entry)
		entry.Narrative = narrativeFor(entry.Type, subject, actor)
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
		return fmt.Sprintf("%s claimed task \"%s\"", actor, subject)
	case EventTaskUnclaimed:
		return fmt.Sprintf("Task \"%s\" was released back to the queue", subject)
	case EventTaskCompleted:
		return fmt.Sprintf("%s completed task \"%s\"", actor, subject)
	case EventTaskAssigned:
		return fmt.Sprintf("Task \"%s\" was assigned to %s", subject, actor)
	case EventTaskDelegated:
		return fmt.Sprintf("Task \"%s\" was delegated to %s", subject, actor)
	case EventTaskEscalated:
		return fmt.Sprintf("Task \"%s\" was escalated to %s", subject, actor)
	case EventTaskCreated:
		return fmt.Sprintf("Task \"%s\" became available", subject)
	case EventProcessStarted:
		return fmt.Sprintf("Process \"%s\" was started", subject)
	case EventProcessEnded:
		return fmt.Sprintf("Process \"%s\" completed successfully", subject)
	case EventProcessFailed:
		return fmt.Sprintf("Process \"%s\" failed", subject)
	case EventNodeReached:
		return fmt.Sprintf("Step \"%s\" started", subject)
	case EventNodeCompleted:
		return fmt.Sprintf("Step \"%s\" finished", subject)
	default:
		if subject != "" {
			return fmt.Sprintf("Event %q occurred on \"%s\"", eventType, subject)
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
	m.Base.CreatedAt = e.Timestamp
	if m.Base.CreatedAt.IsZero() {
		m.Base.CreatedAt = time.Now()
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
