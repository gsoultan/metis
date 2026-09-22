package bpmn_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	handlersimpl "github.com/gsoultan/metis/server/domains/handlers/impl"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// releaseFixture is the smallest wiring that can deploy, start and complete —
// enough to prove which version an instance actually runs.
func releaseFixture(t *testing.T) (services.ServiceFacade, uuid.UUID, context.Context) {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	dispatcher := impl.NewEventDispatcher()

	engine := serviceimpl.NewExecutionEngine(repo, dispatcher)
	connectorSvc := serviceimpl.NewConnectorService(repo)
	taskSvc := serviceimpl.NewTaskService(repo, engine, serviceimpl.NewAuditWriter(repo.Audit()))
	jobSvc := serviceimpl.NewJobService(repo, engine, connectorSvc, serviceimpl.NewNoOpLocker(), handlersimpl.NewErrorBoundaryMatcher())
	externalTaskSvc := serviceimpl.NewExternalTaskService(repo, engine)
	decisionSvc := serviceimpl.NewDecisionService(repo, serviceimpl.NewDecisionTableEvaluator(serviceimpl.NewFEELEvaluator()))
	engine.Apply(
		serviceimpl.WithHandlerFactory(handlersimpl.NewNodeHandlerFactory(
			engine, taskSvc, jobSvc, externalTaskSvc, decisionSvc, connectorSvc,
			repo.Subscription(), serviceimpl.NewAuditWriter(repo.Audit()))),
		serviceimpl.WithJobService(jobSvc),
	)

	svc := services.NewService(services.ServiceParams{
		OrganizationService: serviceimpl.NewOrganizationService(repo),
		ProjectService:      serviceimpl.NewProjectService(repo),
		DefinitionService:   serviceimpl.NewDefinitionService(repo),
		TaskService:         taskSvc,
		ExecutionEngine:     engine,
		JobService:          jobSvc,
		ExternalTaskService: externalTaskSvc,
		DecisionService:     decisionSvc,
		MigrationService:    serviceimpl.NewMigrationService(repo, engine),
		ConnectorService:    connectorSvc,
		MessagingService:    serviceimpl.NewMessagingService(engine, externalTaskSvc),
		UserService:         serviceimpl.NewUserService(repo, "test-jwt-secret"),
		SetupService:        serviceimpl.NewSetupService(nil),
	})

	ctx := context.Background()
	org, err := svc.CreateOrganization(ctx, "Release Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	// From here this test stands in for a request from inside that
	// organization. It carried no identity at all, which only worked while
	// the repository scope failed open.
	ctx = entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()})
	proj, err := svc.CreateProject(ctx, org.ID, "Release Project", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return svc, proj.ID, ctx
}

// approvalModel is start -> hold -> end, where hold is a user task the instance
// parks on. Parking is the point: it is what lets a deploy happen mid-flight.
func approvalModel(projectID uuid.UUID, name, holdNodeID string) entities.ProcessDefinition {
	return entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "expense-approval",
		Name:    name,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{ID: holdNodeID, Type: entities.UserTask, Name: "Hold", Assignee: "approver"},
			{ID: "end", Type: entities.EndEvent, Name: "End"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: holdNodeID},
			{ID: "f2", SourceRef: holdNodeID, TargetRef: "end"},
		},
	}
}

func startedVersion(t *testing.T, ctx context.Context, svc services.ServiceFacade, projectID uuid.UUID) int {
	t.Helper()
	instanceID, err := svc.StartProcess(ctx, projectID, "expense-approval", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	instance, err := svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	def, err := svc.GetDefinition(ctx, instance.Definition.ID)
	if err != nil {
		t.Fatalf("get definition: %v", err)
	}
	return def.Version
}

// A staged version is deployed but not live: it exists, it has a version number,
// and new instances keep starting on the version that was live before it.
//
// This is the case the release table exists for. Without it "live" means "the
// highest version", so the act of saving a model promoted it.
func TestStagedVersionDoesNotTakeNewInstances(t *testing.T) {
	svc, projectID, ctx := releaseFixture(t)

	v1 := approvalModel(projectID, "v1", "hold")
	if _, err := svc.CreateDefinition(ctx, &v1); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if got := startedVersion(t, ctx, svc, projectID); got != 1 {
		t.Fatalf("before staging, new instances should start on v1, got v%d", got)
	}

	v2 := approvalModel(projectID, "v2", "hold-revised")
	if _, err := svc.DeployDefinition(ctx, &v2, false); err != nil {
		t.Fatalf("stage v2: %v", err)
	}
	if v2.Version != 2 {
		t.Fatalf("staging should still allocate a version, got v%d", v2.Version)
	}

	if got := startedVersion(t, ctx, svc, projectID); got != 1 {
		t.Fatalf("a staged version must not take new instances, but one started on v%d", got)
	}

	if err := svc.PromoteDefinitionVersion(ctx, projectID, "expense-approval", 2); err != nil {
		t.Fatalf("promote v2: %v", err)
	}
	if got := startedVersion(t, ctx, svc, projectID); got != 2 {
		t.Fatalf("after promotion new instances should start on v2, got v%d", got)
	}
}

// Promotion is reversible: naming an older version puts new instances back on
// it. This is the rollback that previously required redeploying the old model
// under a higher number.
func TestPromotingAnOlderVersionRollsBack(t *testing.T) {
	svc, projectID, ctx := releaseFixture(t)

	v1 := approvalModel(projectID, "v1", "hold")
	if _, err := svc.CreateDefinition(ctx, &v1); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	v2 := approvalModel(projectID, "v2", "hold-revised")
	if _, err := svc.CreateDefinition(ctx, &v2); err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	if got := startedVersion(t, ctx, svc, projectID); got != 2 {
		t.Fatalf("a promoted deploy should be live, got v%d", got)
	}

	if err := svc.PromoteDefinitionVersion(ctx, projectID, "expense-approval", 1); err != nil {
		t.Fatalf("roll back to v1: %v", err)
	}
	if got := startedVersion(t, ctx, svc, projectID); got != 1 {
		t.Fatalf("after rollback new instances should start on v1, got v%d", got)
	}
}

// Promoting a version nobody deployed has to be refused. Accepted, the release
// row would name a version that does not exist, GetByKey's fallback would
// quietly resolve to the highest one, and the caller would be told a rollback
// worked while new instances kept starting on what they rolled back from.
func TestPromotingAnUndeployedVersionIsRefused(t *testing.T) {
	svc, projectID, ctx := releaseFixture(t)

	v1 := approvalModel(projectID, "v1", "hold")
	if _, err := svc.CreateDefinition(ctx, &v1); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}

	err := svc.PromoteDefinitionVersion(ctx, projectID, "expense-approval", 7)
	if err == nil {
		t.Fatal("promoting a version that was never deployed should be refused")
	}
	if !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("expected a not-found refusal, got %v", err)
	}
	if got := startedVersion(t, ctx, svc, projectID); got != 1 {
		t.Fatalf("a refused promotion must not move the live version, new instance is on v%d", got)
	}
}

// The drain view: what is live, and how much work each version still holds.
// "Has v1 finished?" is a question about running instances, not about which
// version is live, and this is where that answer comes from.
func TestVersionStatusReportsLiveAndDraining(t *testing.T) {
	svc, projectID, ctx := releaseFixture(t)

	v1 := approvalModel(projectID, "v1", "hold")
	if _, err := svc.CreateDefinition(ctx, &v1); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	// Two instances park on v1's user task, so v1 has work outstanding when it
	// stops being live.
	first, err := svc.StartProcess(ctx, projectID, "expense-approval", nil)
	if err != nil {
		t.Fatalf("start first: %v", err)
	}
	if _, err := svc.StartProcess(ctx, projectID, "expense-approval", nil); err != nil {
		t.Fatalf("start second: %v", err)
	}

	v2 := approvalModel(projectID, "v2", "hold-revised")
	if _, err := svc.CreateDefinition(ctx, &v2); err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	versions, err := svc.ListDefinitionVersions(ctx, projectID, "expense-approval")
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected two versions, got %d", len(versions))
	}
	// Newest first.
	if versions[0].Version != 2 || !versions[0].Live {
		t.Fatalf("v2 should be live, got v%d live=%v", versions[0].Version, versions[0].Live)
	}
	if versions[0].RunningInstances != 0 {
		t.Fatalf("v2 has taken no instances yet, got %d", versions[0].RunningInstances)
	}
	if versions[1].Version != 1 || versions[1].Live {
		t.Fatalf("v1 should no longer be live, got v%d live=%v", versions[1].Version, versions[1].Live)
	}
	if versions[1].RunningInstances != 2 {
		t.Fatalf("v1 should still hold two running instances, got %d", versions[1].RunningInstances)
	}
	if !versions[1].Draining() {
		t.Fatal("v1 has been replaced and still has work, so it is draining")
	}

	// Finishing one instance moves the count, which is what makes the drain
	// observable rather than a guess.
	tasks, err := svc.ListTasks(ctx, projectID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == first {
			if err := svc.CompleteTask(ctx, task.ID, "approver", nil); err != nil {
				t.Fatalf("complete task: %v", err)
			}
		}
	}

	versions, err = svc.ListDefinitionVersions(ctx, projectID, "expense-approval")
	if err != nil {
		t.Fatalf("list versions after completion: %v", err)
	}
	if versions[1].RunningInstances != 1 {
		t.Fatalf("one instance finished, so v1 should hold one, got %d", versions[1].RunningInstances)
	}
	if versions[1].TotalInstances != 2 {
		t.Fatalf("v1 has had two instances in total, got %d", versions[1].TotalInstances)
	}
}

// An instance keeps running the version it started on after a newer one is
// promoted — including which nodes it advances through. This is the guarantee
// the whole feature rests on, so it is asserted against the graph rather than
// against the definition ID alone.
func TestRunningInstanceDrainsOnItsOwnVersion(t *testing.T) {
	svc, projectID, ctx := releaseFixture(t)

	v1 := approvalModel(projectID, "v1", "hold")
	if _, err := svc.CreateDefinition(ctx, &v1); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	instanceID, err := svc.StartProcess(ctx, projectID, "expense-approval", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// v2 renames the node the running instance is parked on. If the instance
	// were moved onto it, the token would be sitting on a node v2 does not have.
	v2 := approvalModel(projectID, "v2", "hold-revised")
	if _, err := svc.CreateDefinition(ctx, &v2); err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	instance, err := svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if instance.Definition.ID != v1.ID {
		t.Fatalf("a running instance must stay on the version it started on, moved to %s", instance.Definition.ID)
	}

	tasks, err := svc.ListTasks(ctx, projectID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	var completed bool
	for _, task := range tasks {
		if task.Instance == nil || task.Instance.ID != instanceID {
			continue
		}
		if task.NodeID() != "hold" {
			t.Fatalf("the instance should be parked on v1's node, found %q", task.NodeID())
		}
		if err := svc.CompleteTask(ctx, task.ID, "approver", nil); err != nil {
			t.Fatalf("complete task: %v", err)
		}
		completed = true
	}
	if !completed {
		t.Fatal("the running instance had no open task to complete")
	}

	instance, err = svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("get instance after completion: %v", err)
	}
	if instance.Status != entities.ProcessCompleted {
		t.Fatalf("v1's graph runs hold -> end, so the instance should have completed, got %q", instance.Status)
	}
}

// A process key is unique per project, not per organization, so two projects in
// one tenant can both hold "expense-approval". Their live versions are separate
// choices, and one project's release must not pin the other project's starts.
func TestReleasesAreScopedToTheirProject(t *testing.T) {
	svc, projectA, ctx := releaseFixture(t)

	// A second project in the same organization, holding the same key.
	orgs, err := svc.ListOrganizations(ctx)
	if err != nil || len(orgs) == 0 {
		t.Fatalf("list organizations: %v", err)
	}
	projectB, err := svc.CreateProject(ctx, orgs[0].ID, "Second Project", "")
	if err != nil {
		t.Fatalf("create second project: %v", err)
	}

	// Project A: two versions, rolled back to v1.
	a1 := approvalModel(projectA, "A v1", "hold")
	if _, err := svc.CreateDefinition(ctx, &a1); err != nil {
		t.Fatalf("deploy A v1: %v", err)
	}
	a2 := approvalModel(projectA, "A v2", "hold")
	if _, err := svc.CreateDefinition(ctx, &a2); err != nil {
		t.Fatalf("deploy A v2: %v", err)
	}
	if err := svc.PromoteDefinitionVersion(ctx, projectA, "expense-approval", 1); err != nil {
		t.Fatalf("roll project A back to v1: %v", err)
	}

	// Project B: its own series, never rolled back.
	b1 := approvalModel(projectB.ID, "B v1", "hold")
	if _, err := svc.CreateDefinition(ctx, &b1); err != nil {
		t.Fatalf("deploy B v1: %v", err)
	}
	b2 := approvalModel(projectB.ID, "B v2", "hold")
	if _, err := svc.CreateDefinition(ctx, &b2); err != nil {
		t.Fatalf("deploy B v2: %v", err)
	}

	if got := startedVersion(t, ctx, svc, projectB.ID); got != 2 {
		t.Fatalf("project B never rolled back, so it should start on v2, got v%d", got)
	}
	if got := startedVersion(t, ctx, svc, projectA); got != 1 {
		t.Fatalf("project A rolled back, so it should start on v1, got v%d", got)
	}

	versions, err := svc.ListDefinitionVersions(ctx, projectB.ID, "expense-approval")
	if err != nil {
		t.Fatalf("list project B versions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("project B should see only its own two versions, got %d", len(versions))
	}
	if !versions[0].Live || versions[0].Version != 2 {
		t.Fatalf("project B's live version should be v2, got v%d live=%v", versions[0].Version, versions[0].Live)
	}
}

// A version an instance references cannot be deleted.
//
// Instances pin their definition by ID, so deleting the row strands a running
// one — the engine can never load the graph it is executing — and erases the
// record of what a finished one actually ran.
func TestDeletingAVersionInUseIsRefused(t *testing.T) {
	svc, projectID, ctx := releaseFixture(t)

	v1 := approvalModel(projectID, "v1", "hold")
	v1ID, err := svc.CreateDefinition(ctx, &v1)
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	instanceID, err := svc.StartProcess(ctx, projectID, "expense-approval", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	err = svc.DeleteDefinition(ctx, v1ID)
	if err == nil {
		t.Fatal("deleting a version with a running instance should be refused")
	}
	if !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("expected an invalid-argument refusal, got %v", err)
	}

	// The instance is untouched and still runnable.
	tasks, err := svc.ListTasks(ctx, projectID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID {
			if err := svc.CompleteTask(ctx, task.ID, "approver", nil); err != nil {
				t.Fatalf("the instance should still run: %v", err)
			}
		}
	}

	// Finished is still "in use": the record of what it ran has to survive.
	if err := svc.DeleteDefinition(ctx, v1ID); err == nil {
		t.Fatal("deleting a version whose instances have completed should still be refused")
	}
}

// A version nothing has ever run can be deleted — a staged one thought better
// of, or a mistake.
func TestDeletingAnUnusedVersionIsAllowed(t *testing.T) {
	svc, projectID, ctx := releaseFixture(t)

	v1 := approvalModel(projectID, "v1", "hold")
	if _, err := svc.CreateDefinition(ctx, &v1); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	v2 := approvalModel(projectID, "v2", "hold-revised")
	v2ID, err := svc.DeployDefinition(ctx, &v2, false)
	if err != nil {
		t.Fatalf("stage v2: %v", err)
	}

	if err := svc.DeleteDefinition(ctx, v2ID); err != nil {
		t.Fatalf("a staged version nothing has run should be deletable: %v", err)
	}

	versions, err := svc.ListDefinitionVersions(ctx, projectID, "expense-approval")
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != 1 || versions[0].Version != 1 {
		t.Fatalf("only v1 should remain, got %d versions", len(versions))
	}
	// And the live version still starts.
	if got := startedVersion(t, ctx, svc, projectID); got != 1 {
		t.Fatalf("v1 should still be live and startable, got v%d", got)
	}
}

// TestDeletingAVersionAScheduledCutoverNeedsIsRefused.
//
// Root cause: "safe to delete" was decided from instance counts alone, and a
// version scheduled for next week has run nothing yet — so it deleted cleanly
// while the release timeline still named it. At the scheduled moment the reader
// finds no such version and falls back to the highest one, so whichever draft
// happened to be deployed in the meantime goes live instead: a silent version
// swap, unattended, at an hour nobody is watching.
func TestDeletingAVersionAScheduledCutoverNeedsIsRefused(t *testing.T) {
	svc, projectID, ctx := releaseFixture(t)

	v1 := approvalModel(projectID, "v1", "hold")
	if _, err := svc.CreateDefinition(ctx, &v1); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	v2 := approvalModel(projectID, "v2", "hold")
	v2ID, err := svc.CreateDefinition(ctx, &v2)
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	// The cutover is arranged for next week. Nothing has run on v2, so every
	// other check in DeleteDefinition passes it.
	activateAt := time.Now().UTC().Add(7 * 24 * time.Hour)
	if err := svc.ScheduleDefinitionVersion(ctx, projectID, "expense-approval", 2, activateAt); err != nil {
		t.Fatalf("schedule the cutover: %v", err)
	}

	err = svc.DeleteDefinition(ctx, v2ID)
	if err == nil {
		t.Fatal("a version a scheduled cutover is waiting on was deleted; the timeline now names a version that is not there")
	}
	if !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("expected an invalid-argument refusal, got %v", err)
	}

	// And the version is still there to go live when its time comes.
	versions, err := svc.ListDefinitionVersions(ctx, projectID, "expense-approval")
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	var found bool
	for _, version := range versions {
		if version.Version == 2 {
			found = true
		}
	}
	if !found {
		t.Fatal("version 2 is gone after a refused delete")
	}
}

// TestDeletingAnUnscheduledVersionIsStillAllowed keeps the refusal narrow: a
// version nothing has run and no cutover is waiting on is still deletable.
func TestDeletingAnUnscheduledVersionIsStillAllowed(t *testing.T) {
	svc, projectID, ctx := releaseFixture(t)

	v1 := approvalModel(projectID, "v1", "hold")
	if _, err := svc.CreateDefinition(ctx, &v1); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	v2 := approvalModel(projectID, "v2", "hold")
	v2ID, err := svc.CreateDefinition(ctx, &v2)
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	if err := svc.DeleteDefinition(ctx, v2ID); err != nil {
		t.Fatalf("deleting an unused, unscheduled version was refused: %v", err)
	}
}
