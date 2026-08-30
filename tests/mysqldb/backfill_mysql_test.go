package mysqldb_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	handlersimpl "github.com/gsoultan/metis/server/domains/handlers/impl"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// The correlation-key backfill on MySQL.
//
// The migration selects rows with correlation_key LIKE '%${%'. MySQL's default
// collation makes LIKE case-insensitive, and its wildcard and escape handling
// differ from PostgreSQL's — so the query that repairs production data gets its
// own run against the engine, rather than an argument that "${" contains no
// wildcards so it must be fine.
func newMySQLEngine(t *testing.T, db *gorm.DB) (repositories.Repository, *serviceimpl.Engine, uuid.UUID) {
	t.Helper()
	ctx := t.Context()

	repo := repositories.NewRepository(db)
	dispatcher := observersimpl.NewEventDispatcher()
	engine := serviceimpl.NewExecutionEngine(repo, dispatcher)
	connectorSvc := serviceimpl.NewConnectorService(repo)
	taskSvc := serviceimpl.NewTaskService(repo, engine, serviceimpl.NewAuditWriter(repo.Audit()))
	jobSvc := serviceimpl.NewJobService(repo, engine, connectorSvc, serviceimpl.NewNoOpLocker(), handlersimpl.NewErrorBoundaryMatcher())
	externalTaskSvc := serviceimpl.NewExternalTaskService(repo, engine)
	decisionSvc := serviceimpl.NewDecisionService(repo, serviceimpl.NewDecisionTableEvaluator(serviceimpl.NewFEELEvaluator()))

	engine.Apply(
		serviceimpl.WithHandlerFactory(handlersimpl.NewNodeHandlerFactory(
			engine, taskSvc, jobSvc, externalTaskSvc, decisionSvc, connectorSvc, repo.Subscription(), serviceimpl.NewAuditWriter(repo.Audit()))),
		serviceimpl.WithJobService(jobSvc),
	)

	org, err := serviceimpl.NewOrganizationService(repo).CreateOrganization(ctx, "MySQL Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	proj, err := serviceimpl.NewProjectService(repo).CreateProject(ctx, org.ID, "MySQL Project", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return repo, engine, proj.ID
}

func TestCorrelationBackfillOnMySQL(t *testing.T) {
	db := testutils.SetupMySQLDB(t, 4)
	ctx := t.Context()
	repo, engine, projID := newMySQLEngine(t, db)

	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: projID},
		Key:     "order-payment-mysql",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "await-payment", Type: entities.IntermediateCatchEvent, Properties: map[string]any{
				"message_name":    "PaymentReceived",
				"correlation_key": "${orderId}",
			}},
			{ID: "confirm", Type: entities.UserTask, Name: "Confirm Order"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "await-payment"},
			{ID: "f2", SourceRef: "await-payment", TargetRef: "confirm"},
			{ID: "f3", SourceRef: "confirm", TargetRef: "end"},
		},
	}
	if _, err := serviceimpl.NewDefinitionService(repo).CreateDefinition(ctx, def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := engine.StartProcess(ctx, projID, "order-payment-mysql", map[string]any{"orderId": "order-1"})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	subs, err := repo.Subscription().ListByInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected one subscription, got %d", len(subs))
	}
	if err := repo.Subscription().UpdateCorrelationKey(ctx, uuid.UUID(subs[0].ID), "${orderId}"); err != nil {
		t.Fatalf("stage the legacy key: %v", err)
	}

	templated, err := repo.Subscription().ListTemplatedMessageSubscriptions(ctx)
	if err != nil {
		t.Fatalf("list templated subscriptions: %v", err)
	}
	if len(templated) != 1 {
		t.Fatalf("the LIKE pattern found %d templated subscriptions on MySQL, expected 1", len(templated))
	}

	result, err := serviceimpl.BackfillMessageCorrelationKeys(ctx, repo)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if result.Scanned != 1 || result.Rewritten != 1 || result.Unresolved != 0 {
		t.Errorf("expected 1 scanned / 1 rewritten / 0 unresolved, got %+v", result)
	}

	if err := engine.SendMessage(ctx, projID, "PaymentReceived", "order-1", map[string]any{"amount": 4200}); err != nil {
		t.Fatalf("send message: %v", err)
	}
	after, err := repo.Subscription().ListByInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("re-list subscriptions: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("the message did not reach the repaired instance; %d subscriptions still waiting", len(after))
	}

	// Idempotence: the rewritten key holds no placeholder, so a second run has
	// nothing to select even under a case-insensitive collation.
	repeat, err := serviceimpl.BackfillMessageCorrelationKeys(ctx, repo)
	if err != nil {
		t.Fatalf("repeat backfill: %v", err)
	}
	if repeat.Scanned != 0 {
		t.Errorf("a repeat run selected %d rows on MySQL; the migration is not idempotent there", repeat.Scanned)
	}
}
