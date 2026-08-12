package impl

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	handlersimpl "github.com/gsoultan/gobpm/server/domains/handlers/impl"
	observerimpl "github.com/gsoultan/gobpm/server/domains/observers/impl"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/tests/testutils"
)

// A service task that names a connector it cannot reach must fail, not proceed.
//
// Resolution fell through to the HTTP runner when no connector instance
// matched, and that runner treats a node with no http_url as a no-op:
//
//	url := node.GetStringProperty("http_url")
//	if url == "" {
//	    return nil, nil // simulated / no-op task
//	}
//
// A node configured to post to Slack has no http_url, so deleting its connector
// instance — or moving it to another project — turned "Send the notification"
// into a step that sent nothing. The engine then proceeded, and the instance
// finished as completed with no incident. Observed on a running server:
//
//	instance status : completed
//	variables       : {"message": "deploy finished"}
//	incidents       : 0
//
// A process that reports success for work it did not do is worse than one that
// fails, because nothing prompts anyone to look. The no-op is kept for a task
// that configures nothing at all, which is a legitimate way to model a step
// that is not built yet; naming a connector is a statement that the call
// matters.
func TestServiceTask_FailsWhenItsConnectorInstanceIsMissing(t *testing.T) {
	svc, projectID := jobServiceForConnectorTest(t)
	def := &entities.ProcessDefinition{Project: &entities.Project{ID: projectID}}

	node := entities.Node{
		ID:   "notify",
		Name: "Send the notification",
		Type: entities.ServiceTask,
		Properties: map[string]any{
			// An instance that does not exist: deleted, or never created.
			"connector_instance_id": uuid.Must(uuid.NewV7()).String(),
		},
	}

	_, err := svc.resolveAndExecuteConnector(context.Background(), def, node, map[string]any{"message": "deploy finished"})
	if err == nil {
		t.Fatal("a service task naming a missing connector instance was allowed to proceed")
	}
	if !strings.Contains(err.Error(), "connector") {
		t.Fatalf("the error does not say what could not be resolved: %v", err)
	}
}

// The same holds for the other way a node names one.
func TestServiceTask_FailsWhenItsConnectorIsNotConfiguredForTheProject(t *testing.T) {
	svc, projectID := jobServiceForConnectorTest(t)
	def := &entities.ProcessDefinition{Project: &entities.Project{ID: projectID}}

	node := entities.Node{
		ID:   "notify",
		Type: entities.ServiceTask,
		Properties: map[string]any{
			// A real connector, but this project has no instance of it.
			"connector_id": uuid.Must(uuid.NewV7()).String(),
		},
	}

	if _, err := svc.resolveAndExecuteConnector(context.Background(), def, node, nil); err == nil {
		t.Fatal("a service task naming an unconfigured connector was allowed to proceed")
	}
}

// An instance belonging to a different project is not this project's to use.
// Silently ignoring it would be a tenant leak in the other direction: the
// process would carry on as though the call had been made.
func TestServiceTask_FailsWhenTheInstanceBelongsToAnotherProject(t *testing.T) {
	svc, projectID := jobServiceForConnectorTest(t)
	ctx := context.Background()

	if err := svc.connectorSvc.EnsureDefaultConnectors(ctx); err != nil {
		t.Fatalf("seed catalogue: %v", err)
	}
	catalogue, _ := svc.connectorSvc.ListConnectors(ctx)
	if len(catalogue) == 0 {
		t.Fatal("no connectors to configure")
	}

	otherProject := uuid.Must(uuid.NewV7())
	instance, err := svc.connectorSvc.CreateConnectorInstance(ctx, entities.ConnectorInstance{
		Name:      "Someone else's Slack",
		Project:   &entities.Project{ID: otherProject},
		Connector: &entities.Connector{ID: catalogue[0].ID},
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	def := &entities.ProcessDefinition{Project: &entities.Project{ID: projectID}}
	node := entities.Node{
		ID:         "notify",
		Type:       entities.ServiceTask,
		Properties: map[string]any{"connector_instance_id": instance.ID.String()},
	}

	if _, err := svc.resolveAndExecuteConnector(ctx, def, node, nil); err == nil {
		t.Fatal("a service task used a connector instance belonging to another project")
	}
}

// A task that configures nothing is still allowed to do nothing: that is how a
// step which is not built yet gets modelled, and it is the case the fallback
// exists for.
func TestServiceTask_WithNoConnectorAndNoURLIsStillANoOp(t *testing.T) {
	svc, projectID := jobServiceForConnectorTest(t)
	def := &entities.ProcessDefinition{Project: &entities.Project{ID: projectID}}

	node := entities.Node{ID: "placeholder", Type: entities.ServiceTask}

	result, err := svc.resolveAndExecuteConnector(context.Background(), def, node, nil)
	if err != nil {
		t.Fatalf("an unconfigured service task was rejected: %v", err)
	}
	if result != nil {
		t.Fatalf("an unconfigured service task produced %#v, want nil so the HTTP runner is tried", result)
	}
}

// A definition with no project cannot have its connectors resolved, and used to
// be dereferenced without checking.
func TestServiceTask_ToleratesADefinitionWithNoProject(t *testing.T) {
	svc, _ := jobServiceForConnectorTest(t)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("resolving a connector for a project-less definition panicked: %v", r)
		}
	}()

	node := entities.Node{
		ID:         "notify",
		Type:       entities.ServiceTask,
		Properties: map[string]any{"connector_instance_id": uuid.Must(uuid.NewV7()).String()},
	}
	if _, err := svc.resolveAndExecuteConnector(context.Background(), &entities.ProcessDefinition{}, node, nil); err == nil {
		t.Fatal("a definition with no project resolved a connector anyway")
	}
}

func jobServiceForConnectorTest(t *testing.T) (*jobService, uuid.UUID) {
	t.Helper()
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	engine := NewExecutionEngine(repo, observerimpl.NewEventDispatcher())
	svc := NewJobService(repo, engine, NewConnectorService(repo), NewNoOpLocker(), handlersimpl.NewErrorBoundaryMatcher())
	js, ok := svc.(*jobService)
	if !ok {
		t.Fatalf("NewJobService returned %T, want *jobService", svc)
	}
	return js, uuid.Must(uuid.NewV7())
}
