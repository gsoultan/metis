package bpmn_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// entriesAhead is as many audit entries as one read of the generated store
// returns: storm starts every query with a limit of 1000.
const entriesAhead = 1000

// seedEarlierEntries records n steps an instance took before anything already
// on its trail — a loop that went round a thousand times — in one statement.
func (h engineHarness) seedEarlierEntries(t *testing.T, instanceID uuid.UUID, nodeID string, n int) {
	t.Helper()
	if err := h.db.WithContext(h.Ctx()).Exec(`
		INSERT INTO audit_logs (id, created_at, updated_at, project_id, instance_id, type, node_id, message)
		SELECT gen_random_uuid(), now() - interval '1 day' + n * interval '1 second',
		       now() - interval '1 day' + n * interval '1 second', ?, ?, ?, ?, 'The retry step was reached'
		  FROM generate_series(1, ?) AS n`, h.projID, instanceID, entities.EventNodeReached, nodeID, n).Error; err != nil {
		t.Fatalf("seed %d audit entries: %v", n, err)
	}
}

// trailLength counts an instance's audit entries in the database, not through
// the code under test.
func (h engineHarness) trailLength(t *testing.T, instanceID uuid.UUID) int {
	t.Helper()
	var n int
	if err := h.db.WithContext(h.Ctx()).Raw(
		`SELECT count(*) FROM audit_logs WHERE instance_id = ? AND deleted_at IS NULL`, instanceID).Scan(&n).Error; err != nil {
		t.Fatalf("count the trail: %v", err)
	}
	return n
}

// An instance's history shows everything that happened to it, and the export
// of a project's history carries every event.
//
// All three read the trail oldest first through a query the store caps at a
// thousand rows. Past a thousand entries — a loop that retries, a long-lived
// case — the audit view ended a thousand steps in and never showed where the
// instance is now, the execution path drawn on the diagram stopped at the same
// place, and the OCEL export handed a mining tool a log with its most recent
// events missing.
func TestAnInstancesWholeHistoryIsShownAndExported(t *testing.T) {
	h := newEngineHarness(t, "Long History Project")
	ctx := h.Ctx()
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "long-running",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "end"},
		},
	})
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "long-running", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	h.seedEarlierEntries(t, instanceID, "retry", entriesAhead)
	// And where it is now, recorded the way the audit observer records it.
	if err := h.db.WithContext(ctx).Exec(`
		INSERT INTO audit_logs (id, created_at, updated_at, project_id, instance_id, type, node_id, message)
		VALUES (gen_random_uuid(), now(), now(), ?, ?, ?, 'review', 'The review step was reached')`,
		h.projID, instanceID, entities.EventNodeReached).Error; err != nil {
		t.Fatalf("record the step reached last: %v", err)
	}
	want := h.trailLength(t, instanceID)

	trail, err := h.engine.GetAuditLogs(ctx, instanceID)
	if err != nil {
		t.Fatalf("audit trail: %v", err)
	}
	if len(trail) != want {
		t.Errorf("the audit view shows %d of the instance's %d entries", len(trail), want)
	}

	path, err := h.engine.GetExecutionPath(ctx, instanceID)
	if err != nil {
		t.Fatalf("execution path: %v", err)
	}
	reached := map[string]bool{}
	for _, node := range path.Nodes {
		reached[node.ID] = true
	}
	if !reached["review"] || path.Frequencies["retry"] != entriesAhead {
		t.Errorf("the execution path reaches review: %v, and counts %d visits to retry of %d",
			reached["review"], path.Frequencies["retry"], entriesAhead)
	}

	log, err := h.engine.ExportOCEL(ctx, h.projID, entities.OCELOptions{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(log.Events) != want {
		t.Errorf("the OCEL export carries %d of the project's %d events", len(log.Events), want)
	}
}
