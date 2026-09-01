package impl

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// stubAuditRepo is a minimal AuditRepository stub for tests.
type stubAuditRepo struct {
	onCreate func(models.AuditModel) error
}

func (s *stubAuditRepo) Create(_ context.Context, m models.AuditModel) error {
	if s.onCreate != nil {
		return s.onCreate(m)
	}
	return nil
}

func (s *stubAuditRepo) ListByInstance(_ context.Context, _ uuid.UUID) ([]models.AuditModel, error) {
	return nil, nil
}

func (s *stubAuditRepo) ListByProject(_ context.Context, _ uuid.UUID) ([]models.AuditModel, error) {
	return nil, nil
}

func TestNarrativeFor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		eventType string
		subject   string
		actor     string
		want      string
	}{
		{EventTaskClaimed, "Review Invoice", "alice", `alice claimed task "Review Invoice"`},
		{EventTaskUnclaimed, "Review Invoice", "", `Task "Review Invoice" was released back to the queue`},
		{EventTaskCompleted, "Review Invoice", "alice", `alice completed task "Review Invoice"`},
		{EventTaskAssigned, "Review Invoice", "bob", `Task "Review Invoice" was assigned to bob`},
		{EventTaskDelegated, "Review Invoice", "carol", `Task "Review Invoice" was delegated to carol`},
		{EventTaskEscalated, "Review Invoice", "manager", `Task "Review Invoice" was escalated to manager`},
		{EventTaskCreated, "Approve Budget", "", `Task "Approve Budget" became available`},
		{EventProcessStarted, "Loan Approval", "", `Process "Loan Approval" was started`},
		{EventProcessEnded, "Loan Approval", "", `Process "Loan Approval" completed successfully`},
		{EventProcessFailed, "Loan Approval", "", `Process "Loan Approval" failed`},
		{EventNodeReached, "Credit Check", "", `Step "Credit Check" started`},
		{EventNodeCompleted, "Credit Check", "", `Step "Credit Check" finished`},
		{"unknown_event", "something", "", `Event "unknown_event" occurred on "something"`},
		{"unknown_event", "", "", `Event "unknown_event" occurred`},
	}

	for _, tc := range cases {
		t.Run(tc.eventType+"/"+tc.subject, func(t *testing.T) {
			t.Parallel()
			got := narrativeFor(tc.eventType, tc.subject, tc.actor)
			if got != tc.want {
				t.Errorf("narrativeFor(%q, %q, %q)\n got:  %q\n want: %q",
					tc.eventType, tc.subject, tc.actor, got, tc.want)
			}
		})
	}
}

func TestActorName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		entry entities.AuditEntry
		want  string
	}{
		{"with actor", entities.AuditEntry{Data: map[string]any{"actor": "alice"}}, "alice"},
		{"empty actor string", entities.AuditEntry{Data: map[string]any{"actor": ""}}, "System"},
		{"no actor key", entities.AuditEntry{Data: map[string]any{}}, "System"},
		{"nil data", entities.AuditEntry{}, "System"},
		{"non-string actor", entities.AuditEntry{Data: map[string]any{"actor": 42}}, "System"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := actorName(tc.entry)
			if got != tc.want {
				t.Errorf("actorName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSubjectName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		entry entities.AuditEntry
		want  string
	}{
		{"with node name", entities.AuditEntry{Node: &entities.Node{Name: "Credit Check"}}, "Credit Check"},
		{"node without name", entities.AuditEntry{Node: &entities.Node{}}, "unknown"},
		{"nil node", entities.AuditEntry{}, "unknown"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := subjectName(tc.entry)
			if got != tc.want {
				t.Errorf("subjectName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRecordEvent_NarrativeEnrichment(t *testing.T) {
	t.Parallel()

	var persisted models.AuditModel
	stub := &stubAuditRepo{onCreate: func(m models.AuditModel) error {
		persisted = m
		return nil
	}}
	writer := &auditWriter{repo: stub}
	err := writer.RecordEvent(context.Background(), entities.AuditEntry{
		Type: EventTaskClaimed,
		Node: &entities.Node{Name: "Review Invoice"},
		Data: map[string]any{"actor": "alice"},
	})
	if err != nil {
		t.Fatalf("RecordEvent returned unexpected error: %v", err)
	}
	want := `alice claimed task "Review Invoice"`
	if persisted.Narrative != want {
		t.Errorf("Narrative = %q, want %q", persisted.Narrative, want)
	}
}

func TestRecordEvent_PreservesExistingNarrative(t *testing.T) {
	t.Parallel()

	var persisted models.AuditModel
	stub := &stubAuditRepo{onCreate: func(m models.AuditModel) error {
		persisted = m
		return nil
	}}
	writer := &auditWriter{repo: stub}
	custom := "Custom narrative already set"
	err := writer.RecordEvent(context.Background(), entities.AuditEntry{
		Type:      EventTaskClaimed,
		Narrative: custom,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if persisted.Narrative != custom {
		t.Errorf("Narrative = %q, want %q", persisted.Narrative, custom)
	}
}

func TestRecordEvent_RepoError(t *testing.T) {
	t.Parallel()

	stub := &stubAuditRepo{onCreate: func(_ models.AuditModel) error {
		return errors.New("db down")
	}}
	writer := &auditWriter{repo: stub}
	err := writer.RecordEvent(context.Background(), entities.AuditEntry{
		Type: EventTaskClaimed,
	})
	if err == nil {
		t.Fatal("expected error from repo, got nil")
	}
}
