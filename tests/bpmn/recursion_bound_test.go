package bpmn_test

import (
	"strings"
	"testing"

	"github.com/gsoultan/gobpm/server/domains/entities"
	handlersimpl "github.com/gsoultan/gobpm/server/domains/handlers/impl"
	observerimpl "github.com/gsoultan/gobpm/server/domains/observers/impl"
	serviceimpl "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/tests/testutils"
)

// TestUnboundedLoopIsRejectedNotStackOverflow guards the mutual recursion
// ExecuteNode → handler → Proceed → followOutgoingFlows → ExecuteNode.
//
// It has no natural base case. A definition that loops back to an earlier node
// — a retry loop, an entirely ordinary modelling pattern — recursed once per
// iteration inside a single transaction, and enough iterations overflowed the
// stack. Because this runs on the HTTP handler goroutine, that took down the
// whole server rather than failing one request.
//
// The engine now stops at a depth bound and reports a BPMN error naming the
// node that looped, which is diagnosable instead of fatal.
func TestUnboundedLoopIsRejectedNotStackOverflow(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ctx := t.Context()

	dispatcher := observerimpl.NewEventDispatcher()
	engine := serviceimpl.NewExecutionEngine(repo, dispatcher)
	defSvc := serviceimpl.NewDefinitionService(repo)
	orgSvc := serviceimpl.NewOrganizationService(repo)
	projectSvc := serviceimpl.NewProjectService(repo)
	taskSvc := serviceimpl.NewTaskService(repo, engine, serviceimpl.NewAuditWriter(repo.Audit()))
	connectorSvc := serviceimpl.NewConnectorService(repo)
	externalTaskSvc := serviceimpl.NewExternalTaskService(repo, engine)
	decisionSvc := serviceimpl.NewDecisionService(repo, serviceimpl.NewDecisionTableEvaluator(serviceimpl.NewFEELEvaluator()))
	jobSvc := testutils.NewSynchronousJobService(engine, repo)

	engine.Apply(
		serviceimpl.WithHandlerFactory(handlersimpl.NewNodeHandlerFactory(
			engine, taskSvc, jobSvc, externalTaskSvc, decisionSvc, connectorSvc,
			repo.Subscription(),
		)),
		serviceimpl.WithJobService(jobSvc),
	)

	org, err := orgSvc.CreateOrganization(ctx, "Loop Co", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	proj, err := projectSvc.CreateProject(ctx, org.ID, "Loops", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	projectID := proj.ID

	// start → gwA → gwB → gwA → … Gateways always advance, so this is a cycle
	// the engine follows synchronously with nothing to stop it. Modelling a
	// retry loop this way is entirely ordinary.
	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "infinite-loop",
		Name:    "Infinite Loop",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{ID: "loop", Type: entities.ExclusiveGateway, Name: "Loop Gateway"},
			{ID: "back", Type: entities.ExclusiveGateway, Name: "Back Gateway"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "loop"},
			{ID: "f2", SourceRef: "loop", TargetRef: "back"},
			{ID: "f3", SourceRef: "back", TargetRef: "loop"},
		},
	}
	if _, err := defSvc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	// Before the bound existed this call did not return — it grew the stack
	// until the process died.
	_, err = engine.StartProcess(ctx, projectID, "infinite-loop", map[string]any{})
	if err == nil {
		t.Fatal("an unbounded loop completed successfully; the depth bound is not being applied")
	}
	if !strings.Contains(err.Error(), "execution exceeded") {
		t.Fatalf("got %v, want an execution-depth error naming the looping node", err)
	}
	if !strings.Contains(err.Error(), "loop") {
		t.Fatalf("error does not name the offending node, so it is not diagnosable: %v", err)
	}
}
