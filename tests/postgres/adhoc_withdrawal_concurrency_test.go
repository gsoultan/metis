package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// An ad-hoc sub-process that finishes withdraws the work still parked for
// workers inside it, and a worker can be reporting on that work at the same
// moment.
//
// The engine holds the instance and then takes the parked work's row. A
// worker's report took the row first and the instance second — to delete the
// row and then advance, or to record a failure and then raise an incident,
// which refers to the instance. Each held what the other wanted, and the
// database ended one of them as a deadlock: a person told their task could not
// be completed, or a worker told its report had failed.
//
// It needs two transactions that really run at once, so it is here rather than
// with the scenario tests.

// claimResearch is start → an ad-hoc sub-process (a call for a person, a
// scoring for a worker) → decide → end. The sub-process finishes when the call
// has been made.
func claimResearch(projID uuid.UUID, key, topic string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projID},
		Key:     key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{
				ID: "research", Type: entities.SubProcess, Name: "Research the claim",
				IsAdHoc: true, CompletionCondition: "callsMade >= 1",
				Nodes: []*entities.Node{
					{ID: "call-customer", Type: entities.UserTask, Name: "Call the customer", ParentID: "research",
						Properties: testutils.FormDeclaring("callsMade")},
					{ID: "score-claim", Type: entities.ServiceTask, Name: "Score the claim", ParentID: "research",
						ExternalTopic: topic},
				},
			},
			{ID: "decide", Type: entities.UserTask, Name: "Decide the claim"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "research"},
			{ID: "f2", SourceRef: "research", TargetRef: "decide"},
			{ID: "f3", SourceRef: "decide", TargetRef: "end"},
		},
	}
}

// researchRun is one instance with both steps of its sub-process running: the
// call open in an inbox, the scoring fetched by a worker.
type researchRun struct {
	instanceID uuid.UUID
	call       uuid.UUID
	scoring    uuid.UUID
	reported   error
}

// startResearch starts n instances of the claim research and brings each to
// the point where a person and a worker are both about to answer.
func startResearch(ctx context.Context, t *testing.T, repo repositories.Repository, engine *serviceimpl.Engine,
	workers servicecontracts.ExternalTaskService, projID uuid.UUID, key string, n int) []*researchRun {
	t.Helper()
	topic := key + "-scoring"
	if _, err := serviceimpl.NewDefinitionService(repo).CreateDefinition(ctx, claimResearch(projID, key, topic)); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	activator := serviceimpl.NewAdHocActivator(engine, repo)

	runs := make([]*researchRun, 0, n)
	byInstance := map[uuid.UUID]*researchRun{}
	for range n {
		id, err := engine.StartProcess(ctx, projID, key, map[string]any{"callsMade": 0})
		if err != nil {
			t.Fatalf("start process: %v", err)
		}
		for _, step := range []string{"call-customer", "score-claim"} {
			if err := activator.ActivateTask(ctx, id, "research", step); err != nil {
				t.Fatalf("activate %s: %v", step, err)
			}
		}
		tasks, err := repo.Task().ListByInstance(ctx, id)
		if err != nil || len(tasks) != 1 {
			t.Fatalf("expected the one call to be open: %d, err=%v", len(tasks), err)
		}
		run := &researchRun{instanceID: id, call: uuid.UUID(tasks[0].ID)}
		runs = append(runs, run)
		byInstance[id] = run
	}
	scorings, err := workers.FetchAndLock(ctx, topic, "worker", n, 60_000)
	if err != nil || len(scorings) != n {
		t.Fatalf("expected %d scorings: %d, err=%v", n, len(scorings), err)
	}
	for _, scoring := range scorings {
		byInstance[scoring.ProcessInstance.ID].scoring = scoring.ID
	}
	return runs
}

// requireResearchFinishedOnce fails unless the sub-process finished, took
// everything inside it, and moved the process on exactly once.
func requireResearchFinishedOnce(ctx context.Context, t *testing.T, repo repositories.Repository, engine *serviceimpl.Engine, run *researchRun) {
	t.Helper()
	final, err := engine.GetInstance(ctx, run.instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	for _, inside := range []string{"research", "call-customer", "score-claim"} {
		if left := len(final.GetTokensByNode(&entities.Node{ID: inside})); left != 0 {
			t.Errorf("the finished sub-process still holds %d token(s) on %s", left, inside)
		}
	}
	if on := len(final.GetTokensByNode(&entities.Node{ID: "decide"})); on != 1 {
		t.Errorf("the process holds %d token(s) on the next step, want 1", on)
	}
	tasks, err := repo.Task().ListByInstance(ctx, run.instanceID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	decisions := 0
	for _, task := range tasks {
		if task.NodeID == "decide" {
			decisions++
		}
	}
	if decisions != 1 {
		t.Errorf("the next step was opened %d times, want once", decisions)
	}
	parked, err := repo.ExternalTask().ListByProcessInstance(ctx, run.instanceID)
	if err != nil {
		t.Fatalf("list external tasks: %v", err)
	}
	if len(parked) != 0 {
		t.Errorf("%d scoring(s) are still parked for a sub-process that ended", len(parked))
	}
}

// BPMN 2.0.2 §10.3.5, §13.2.5 (ad-hoc sub-process, cancelRemainingInstances):
// when the completion condition holds, the instances still running inside are
// cancelled and the sub-process completes — once, whatever a worker was
// reporting about one of them at that moment.
func TestAReportArrivingAsItsAdHocSubProcessFinishesDoesNotDeadlock(t *testing.T) {
	db := testutils.SetupPostgresDB(t, 8)
	repo, engine, projID, ctx := newPostgresEngine(t, db)
	workers := serviceimpl.NewExternalTaskService(repo, engine)
	inbox := serviceimpl.NewTaskService(repo, engine, serviceimpl.NewAuditWriter(repo.Audit()))

	// Several instances, so the interleaving that deadlocks is met rather than
	// hoped for.
	runs := startResearch(ctx, t, repo, engine, workers, projID, "claim-research-report", 8)

	operator := testutils.AsOperator(ctx, "carol")
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, run := range runs {
		wg.Go(func() {
			<-start
			if err := inbox.CompleteTask(operator, run.call, "carol", map[string]any{"callsMade": 1}); err != nil {
				t.Errorf("the completion that finishes the sub-process failed: %v", err)
			}
		})
		wg.Go(func() {
			<-start
			run.reported = workers.Complete(ctx, run.scoring, "worker", nil)
		})
	}
	close(start)
	wg.Wait()

	for _, run := range runs {
		// Either the report landed first and was counted, or the sub-process
		// had finished and the work it was about was no longer there.
		if run.reported != nil && !errors.Is(run.reported, apierr.ErrNotFound) {
			t.Errorf("a report neither landed nor was told its work was gone: %v", run.reported)
		}
		requireResearchFinishedOnce(ctx, t, repo, engine, run)
	}
}

// BPMN 2.0.2 §10.3.5, §13.2.5 (ad-hoc sub-process, cancelRemainingInstances):
// the same, with the worker giving up — no tries left — as the sub-process
// finishes. A failure that lands raises its incident; one that arrives after
// the work was withdrawn is told there is no such work, and raises nothing.
func TestAFinalFailureArrivingAsItsAdHocSubProcessFinishesDoesNotDeadlock(t *testing.T) {
	db := testutils.SetupPostgresDB(t, 8)
	repo, engine, projID, ctx := newPostgresEngine(t, db)
	workers := serviceimpl.NewExternalTaskService(repo, engine)
	inbox := serviceimpl.NewTaskService(repo, engine, serviceimpl.NewAuditWriter(repo.Audit()))

	runs := startResearch(ctx, t, repo, engine, workers, projID, "claim-research-failure", 8)

	operator := testutils.AsOperator(ctx, "carol")
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, run := range runs {
		wg.Go(func() {
			<-start
			if err := inbox.CompleteTask(operator, run.call, "carol", map[string]any{"callsMade": 1}); err != nil {
				t.Errorf("the completion that finishes the sub-process failed: %v", err)
			}
		})
		wg.Go(func() {
			<-start
			run.reported = workers.HandleFailure(ctx, run.scoring, "worker", "the scoring service is down", "", 0, 0)
		})
	}
	close(start)
	wg.Wait()

	for _, run := range runs {
		incidents, err := repo.Incident().ListByInstance(ctx, run.instanceID)
		if err != nil {
			t.Fatalf("list incidents: %v", err)
		}
		switch {
		case run.reported == nil:
			if len(incidents) != 1 {
				t.Errorf("a final failure that was accepted raised %d incident(s), want 1", len(incidents))
			}
		case errors.Is(run.reported, apierr.ErrNotFound):
			if len(incidents) != 0 {
				t.Errorf("a failure that was told its work was gone raised %d incident(s)", len(incidents))
			}
		default:
			t.Errorf("a final failure neither landed nor was told its work was gone: %v", run.reported)
		}
		requireResearchFinishedOnce(ctx, t, repo, engine, run)
	}
}
