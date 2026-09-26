package bpmn_test

import (
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A rollback cancels the cutovers arranged for later.
//
// Promoting writes one entry on the release timeline, effective now, and left
// the cutovers already arranged for later where they were. They had been
// arranged from the version being rolled back from, so the first to arrive
// undid the rollback without anybody deciding it again.
func refundProcess(h engineHarness) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "refund",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{{ID: "f1", SourceRef: "start", TargetRef: "end"}},
	}
}

func (h engineHarness) stage(t *testing.T, def *entities.ProcessDefinition) {
	t.Helper()
	if _, err := h.svc.DeployDefinition(h.Ctx(), def, false); err != nil {
		t.Fatalf("stage %s: %v", def.Key, err)
	}
}

func (h engineHarness) scheduledCutovers(t *testing.T, key string) map[int]time.Time {
	t.Helper()
	versions, err := h.svc.ListDefinitionVersions(h.Ctx(), h.projID, key)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	scheduled := map[int]time.Time{}
	for _, v := range versions {
		if !v.ScheduledFor.IsZero() {
			scheduled[v.Version] = v.ScheduledFor
		}
	}
	return scheduled
}

func TestARollbackCancelsTheCutoversArrangedForLater(t *testing.T) {
	h := newEngineHarness(t, "Rollback Cutover Project")
	h.deploy(t, refundProcess(h)) // v1, live
	h.deploy(t, refundProcess(h)) // v2, live
	h.stage(t, refundProcess(h))  // v3, staged
	if err := h.svc.ScheduleDefinitionVersion(h.Ctx(), h.projID, "refund", 3, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("schedule v3: %v", err)
	}

	if err := h.svc.PromoteDefinitionVersion(h.Ctx(), h.projID, "refund", 1); err != nil {
		t.Fatalf("roll back to v1: %v", err)
	}

	if scheduled := h.scheduledCutovers(t, "refund"); len(scheduled) != 0 {
		t.Fatalf("after rolling back to v1, cutovers are still arranged for later: %v", scheduled)
	}
}

// Going forward is not a rollback: a cutover arranged for later still happens.
func TestPromotingForwardKeepsTheCutoversArrangedForLater(t *testing.T) {
	h := newEngineHarness(t, "Forward Cutover Project")
	h.deploy(t, refundProcess(h)) // v1, live
	h.stage(t, refundProcess(h))  // v2, staged
	h.stage(t, refundProcess(h))  // v3, staged
	if err := h.svc.ScheduleDefinitionVersion(h.Ctx(), h.projID, "refund", 3, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("schedule v3: %v", err)
	}

	if err := h.svc.PromoteDefinitionVersion(h.Ctx(), h.projID, "refund", 2); err != nil {
		t.Fatalf("promote v2: %v", err)
	}

	if _, still := h.scheduledCutovers(t, "refund")[3]; !still {
		t.Fatal("promoting a newer version cancelled the cutover arranged for later")
	}
}
