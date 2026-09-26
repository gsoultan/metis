// Package sqlconnector_test runs the database lookup the way a process does:
// deployed as a step, executed by the job service, its answer read by the
// gateway after it.
package sqlconnector_test

import (
	"context"
	"maps"
	"slices"
	"testing"

	"github.com/google/uuid"

	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	handlersimpl "github.com/gsoultan/metis/server/domains/handlers/impl"
	observerimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/domains/services/impl/sqlconnector"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

var customers = []string{
	"CREATE TABLE customers (id int PRIMARY KEY, name text NOT NULL, tier text NOT NULL)",
	"INSERT INTO customers VALUES (7, 'Acme', 'gold'), (8, 'Globex', 'silver')",
}

// The feature, end to end: a step looks a customer up in the customer's own
// database, and the gateway after it routes on what came back.
func TestALookupDecidesTheRoute(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		name       string
		customerID float64
		wantTask   string
	}{
		{"a gold customer", 7, "Offer gold terms"},
		{"a silver customer", 8, "Standard review"},
		// Nobody found is an answer: customer.row is absent, the condition is
		// not met, and the default route is taken — no incident.
		{"nobody", 404, "Standard review"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			instance := h.run(t, map[string]any{"customerId": tc.customerID})

			tasks := h.tasksFor(t, instance.ID)
			if len(tasks) != 1 || tasks[0].Name != tc.wantTask {
				t.Fatalf("the process is waiting on %v, want one task %q", taskNames(tasks), tc.wantTask)
			}
			// Exactly one new variable. The engine writes every key a connector
			// returns into the process's variables; the lookup returns one.
			if keys := slices.Sorted(maps.Keys(instance.Variables)); !slices.Equal(keys, []string{"customer", "customerId"}) {
				t.Fatalf("the lookup left the variables %v; it may only add customer", keys)
			}
		})
	}
}

type harness struct {
	ctx         context.Context
	engine      *serviceimpl.Engine
	jobSvc      contracts.JobService
	taskSvc     contracts.TaskService
	defSvc      contracts.DefinitionService
	projectID   uuid.UUID
	definitionK string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	target := testutils.PostgresLookupDatabase(t, customers...)
	repo := repositories.NewRepository(testutils.SetupTestConn(t))
	ctx, _, projectID := testutils.ScopedProject(t, repo)

	engine := serviceimpl.NewExecutionEngine(repo, observerimpl.NewEventDispatcher())
	connectorSvc := serviceimpl.NewConnectorService(repo)
	audit := serviceimpl.NewAuditWriter(repo.Audit())
	taskSvc := serviceimpl.NewTaskService(repo, engine, audit)
	decisionSvc := serviceimpl.NewDecisionService(repo, serviceimpl.NewDecisionTableEvaluator(serviceimpl.NewFEELEvaluator()))
	jobSvc := serviceimpl.NewJobService(repo, engine, connectorSvc, serviceimpl.NewNoOpLocker(), handlersimpl.NewErrorBoundaryMatcher())
	engine.Apply(
		serviceimpl.WithHandlerFactory(handlersimpl.NewNodeHandlerFactory(
			engine, taskSvc, jobSvc, serviceimpl.NewExternalTaskService(repo, engine), decisionSvc,
			repo.Subscription(), audit,
		)),
		serviceimpl.WithJobService(jobSvc),
	)

	// The administrator's part: the project's connection to its database.
	if _, err := connectorSvc.CreateConnectorInstance(ctx, entities.ConnectorInstance{
		Name:      "Customer database",
		Project:   &entities.Project{ID: projectID},
		Connector: &entities.Connector{ID: sqlconnector.CatalogueEntry().ID},
		Config:    map[string]any{"driver": "postgres", "dsn": target.DSN},
	}); err != nil {
		t.Fatalf("configure the connection: %v", err)
	}

	h := &harness{ctx: ctx, engine: engine, jobSvc: jobSvc, taskSvc: taskSvc,
		defSvc: serviceimpl.NewDefinitionService(repo), projectID: projectID, definitionK: "customer-routing"}
	// A designer who may also author queries: deploying a lookup needs both.
	if _, err := h.defSvc.CreateDefinition(as(ctx, entities.RoleDesigner, entities.RoleQueryAuthor), h.definition()); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	return h
}

// as is ctx on behalf of somebody holding roles.
func as(ctx context.Context, roles ...string) context.Context {
	return context.WithValue(ctx, pkgauth.UserContextKey, entities.User{ID: uuid.New(), Roles: roles})
}

// definition is the designer's part: the lookup step and the decision after it.
func (h *harness) definition() *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projectID},
		Key:     h.definitionK,
		Name:    "Route by customer tier",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Order received"},
			{ID: "lookup", Type: entities.ServiceTask, Name: "Look up the customer", Properties: map[string]any{
				"connector_id":                   sqlconnector.CatalogueEntry().ID.String(),
				contracts.StatementProperty:      "SELECT name, tier FROM customers WHERE id = :customer_id",
				contracts.ParamsProperty:         map[string]any{"customer_id": "customerId"},
				contracts.ResultVariableProperty: "customer",
			}},
			{ID: "choose", Type: entities.ExclusiveGateway, Name: "Gold?", DefaultFlow: "to-standard"},
			{ID: "gold", Type: entities.UserTask, Name: "Offer gold terms"},
			{ID: "standard", Type: entities.UserTask, Name: "Standard review"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "lookup"},
			{ID: "f2", SourceRef: "lookup", TargetRef: "choose"},
			{ID: "to-gold", SourceRef: "choose", TargetRef: "gold", Condition: `customer.row.tier = "gold"`},
			{ID: "to-standard", SourceRef: "choose", TargetRef: "standard"},
		},
	}
}

// run starts the process and drains the job queue, so the lookup has run.
func (h *harness) run(t *testing.T, variables map[string]any) entities.ProcessInstance {
	t.Helper()
	instanceID, err := h.engine.StartProcess(h.ctx, h.projectID, h.definitionK, variables)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	for range 10 {
		if err := h.jobSvc.ProcessPendingJobs(h.ctx); err != nil {
			t.Fatalf("process jobs: %v", err)
		}
	}
	instance, err := h.engine.GetInstance(h.ctx, instanceID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	return instance
}

func (h *harness) tasksFor(t *testing.T, instanceID uuid.UUID) []entities.Task {
	t.Helper()
	all, err := h.taskSvc.ListTasks(h.ctx, h.projectID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	var mine []entities.Task
	for _, task := range all {
		if task.Instance != nil && task.Instance.ID == instanceID {
			mine = append(mine, task)
		}
	}
	return mine
}

func taskNames(tasks []entities.Task) []string {
	names := make([]string, len(tasks))
	for i, task := range tasks {
		names[i] = task.Name
	}
	return names
}
