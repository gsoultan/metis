package bpmn_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// auditedEngine is the service facade with the audit trail written the way the
// server writes it: by the audit observer, in the transaction of the step it
// describes.
type auditedEngine struct {
	svc       services.ServiceFacade
	db        *gorm.DB
	ctx       context.Context
	projectID uuid.UUID
}

func newAuditedEngine(t *testing.T, name string) auditedEngine {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	dispatcher := observersimpl.NewEventDispatcher()
	dispatcher.Register(observersimpl.NewAuditLogObserver(repo.Audit()))
	svc := services.NewServiceFacade(repo, dispatcher, observersimpl.NewSSEObserver(), name, nil, nil, nil)
	ctx, _, projectID := testutils.ScopedProject(t, repo)
	return auditedEngine{svc: svc, db: db, ctx: ctx, projectID: projectID}
}

// passThroughAfterDraft is the process: a draft ada writes, then three
// milestones and the end. None of the milestones waits for anything, so
// completing the draft runs through all of them — and records reaching each —
// in the one transaction that completes the task. The draft names ada because a
// task that names nobody is only an administrator's or an operator's to take.
func passThroughAfterDraft(projectID uuid.UUID) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "claim",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "draft", Type: entities.UserTask, Name: "Draft the claim", Assignee: "ada"},
			{ID: "checked", Type: entities.IntermediateThrowEvent, Name: "Claim checked"},
			{ID: "approved", Type: entities.IntermediateThrowEvent, Name: "Claim approved"},
			{ID: "filed", Type: entities.IntermediateThrowEvent, Name: "Claim filed"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "draft"},
			{ID: "f2", SourceRef: "draft", TargetRef: "checked"},
			{ID: "f3", SourceRef: "checked", TargetRef: "approved"},
			{ID: "f4", SourceRef: "approved", TargetRef: "filed"},
			{ID: "f5", SourceRef: "filed", TargetRef: "end"},
		},
	}
}

// writeOrder is the order the steps are reached in, and so the order their
// entries are written: start and draft in the transaction that starts the
// instance, the rest in the one that completes the draft.
var writeOrder = []string{"start", "draft", "checked", "approved", "filed", "end"}

// maxAttemptsForDisagreeingIDs bounds the search for a run whose ids sort
// against the write order. Random ids agree with it one run in 48, so twenty
// attempts all agreeing is a broken generator, not bad luck.
const maxAttemptsForDisagreeingIDs = 20

// The entries one transaction writes share its created_at, and their ids are
// random. Read in created_at order, a trail's entries came back in whatever
// order PostgreSQL returned the ties — the order the rows happen to be stored
// in, which is the write order only until something moves them: an update in
// place (a key rotation's reseal rewrites every sealed entry), CLUSTER,
// pg_repack. Ordered by (created_at, id) they come back in id order, which is
// random. Either way the timeline, the execution path drawn on the diagram and
// the OCEL export could show a step before the one that led to it.
//
// This writes a transaction's entries through the engine, keeps only a run
// whose ids sort against the write order, then stores the table in id order,
// as CLUSTER on the primary key does. The trail must still read in the order it
// was written.
func TestAnInstancesTrailIsReadInTheOrderItWasWritten(t *testing.T) {
	e := newAuditedEngine(t, "audit-write-order-test")
	if _, err := e.svc.CreateDefinition(e.ctx, passThroughAfterDraft(e.projectID)); err != nil {
		t.Fatalf("deploy: %v", err)
	}

	var instanceID uuid.UUID
	for attempt := 1; ; attempt++ {
		if attempt > maxAttemptsForDisagreeingIDs {
			t.Fatalf("%d runs in a row wrote ids that sort in the write order", maxAttemptsForDisagreeingIDs)
		}
		instanceID = e.runToTheEnd(t)
		if !slices.Equal(e.reachedInIDOrder(t, instanceID), writeOrder) {
			break
		}
	}
	e.storeTheTrailInIDOrder(t)

	path, err := e.svc.GetExecutionPath(e.ctx, instanceID)
	if err != nil {
		t.Fatalf("execution path: %v", err)
	}
	var drawn []string
	for _, node := range path.Nodes {
		drawn = append(drawn, node.ID)
	}
	if !slices.Equal(drawn, writeOrder) {
		t.Errorf("the execution path is %v, want %v", drawn, writeOrder)
	}

	trail, err := e.svc.GetAuditLogs(e.ctx, instanceID)
	if err != nil {
		t.Fatalf("audit trail: %v", err)
	}
	var shown []string
	for _, entry := range trail {
		if entry.Type == entities.EventNodeReached && entry.Node != nil {
			shown = append(shown, entry.Node.ID)
		}
	}
	if !slices.Equal(shown, writeOrder) {
		t.Errorf("the timeline shows the steps reached as %v, want %v", shown, writeOrder)
	}

	log, err := e.svc.ExportOCEL(e.ctx, e.projectID, entities.OCELOptions{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if exported := reachedInOCEL(log, instanceID); !slices.Equal(exported, writeOrder) {
		t.Errorf("the OCEL export has the steps reached as %v, want %v", exported, writeOrder)
	}
}

// runToTheEnd starts an instance and completes its draft.
func (e auditedEngine) runToTheEnd(t *testing.T) uuid.UUID {
	t.Helper()
	instanceID, err := e.svc.StartProcess(e.ctx, e.projectID, "claim", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	tasks, err := e.svc.ListTasks(e.ctx, e.projectID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && taskIsOpen(task.Status) {
			if err := e.svc.CompleteTask(e.ctx, task.ID, "ada", nil); err != nil {
				t.Fatalf("complete the draft: %v", err)
			}
			return instanceID
		}
	}
	t.Fatalf("instance %s has no draft to complete", instanceID)
	return uuid.Nil
}

// reachedInIDOrder is the order a read by (created_at, id) would give the steps
// an instance reached, asked of the database rather than of the code under
// test.
func (e auditedEngine) reachedInIDOrder(t *testing.T, instanceID uuid.UUID) []string {
	t.Helper()
	var nodes []string
	if err := e.db.WithContext(e.ctx).Raw(
		`SELECT node_id FROM audit_logs WHERE instance_id = ? AND type = ? ORDER BY created_at, id`,
		instanceID, entities.EventNodeReached).Scan(&nodes).Error; err != nil {
		t.Fatalf("read the steps in id order: %v", err)
	}
	return nodes
}

// storeTheTrailInIDOrder rewrites the audit table in primary-key order, which
// is what CLUSTER and pg_repack do to a table. No row changes; only where each
// is stored.
func (e auditedEngine) storeTheTrailInIDOrder(t *testing.T) {
	t.Helper()
	if err := e.db.WithContext(e.ctx).Exec(`CLUSTER audit_logs USING audit_logs_pkey`).Error; err != nil {
		t.Fatalf("cluster the audit table on its primary key: %v", err)
	}
}

// reachedInOCEL is the steps one case's events say it reached, in the order the
// export lists them.
func reachedInOCEL(log entities.OCELLog, instanceID uuid.UUID) []string {
	var nodes []string
	for _, event := range log.Events {
		if len(event.Relationships) == 0 || event.Relationships[0].ObjectID != instanceID.String() {
			continue
		}
		var lifecycle, nodeID string
		for _, attr := range event.Attributes {
			switch attr.Name {
			case "lifecycle":
				lifecycle = attr.Value
			case "node_id":
				nodeID = attr.Value
			}
		}
		if lifecycle == entities.EventNodeReached {
			nodes = append(nodes, nodeID)
		}
	}
	return nodes
}
