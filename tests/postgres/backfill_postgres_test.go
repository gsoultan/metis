package postgres_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	handlersimpl "github.com/gsoultan/gobpm/server/domains/handlers/impl"
	observersimpl "github.com/gsoultan/gobpm/server/domains/observers/impl"
	serviceimpl "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/tests/testutils"
	"gorm.io/gorm"
)

// The correlation-key backfill against the database it will actually run on.
//
// It executes at startup over real deployment data and its rewrite is one-way,
// so "the SQL looks portable" is not good enough. The query it depends on —
// correlation_key LIKE '%${%' — behaves differently across engines in ways that
// are easy to assume away: wildcard handling, escape requirements, and case
// sensitivity all differ between SQLite, PostgreSQL and MySQL.
func newPostgresEngine(t *testing.T, db *gorm.DB) (repositories.Repository, *serviceimpl.Engine, uuid.UUID) {
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

	orgSvc := serviceimpl.NewOrganizationService(repo)
	projectSvc := serviceimpl.NewProjectService(repo)
	org, err := orgSvc.CreateOrganization(ctx, "PG Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	proj, err := projectSvc.CreateProject(ctx, org.ID, "PG Project", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return repo, engine, proj.ID
}

func paymentDefinition(projID uuid.UUID, key string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projID},
		Key:     key,
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
}

// The migration rehearsal: a stranded instance on PostgreSQL, repaired.
func TestCorrelationBackfillOnPostgres(t *testing.T) {
	db := testutils.SetupPostgresDB(t, 4)
	ctx := t.Context()
	repo, engine, projID := newPostgresEngine(t, db)

	defSvc := serviceimpl.NewDefinitionService(repo)
	if _, err := defSvc.CreateDefinition(ctx, paymentDefinition(projID, "order-payment-pg")); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := engine.StartProcess(ctx, projID, "order-payment-pg", map[string]any{"orderId": "order-1"})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	// Put the subscription back into the shape the pre-fix code wrote.
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

	// This is the query the whole migration rests on: does LIKE '%${%' select
	// the templated row on PostgreSQL?
	templated, err := repo.Subscription().ListTemplatedMessageSubscriptions(ctx)
	if err != nil {
		t.Fatalf("list templated subscriptions: %v", err)
	}
	if len(templated) != 1 {
		t.Fatalf("the LIKE pattern found %d templated subscriptions on PostgreSQL, expected 1", len(templated))
	}

	result, err := serviceimpl.BackfillMessageCorrelationKeys(ctx, repo)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if result.Scanned != 1 || result.Rewritten != 1 || result.Unresolved != 0 {
		t.Errorf("expected 1 scanned / 1 rewritten / 0 unresolved, got %+v", result)
	}

	// And the repaired instance now correlates.
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
}

// A key with no placeholder must not be selected, and the run must be a no-op
// the second time. Idempotence matters here because this executes on every boot.
func TestCorrelationBackfillOnPostgresIsIdempotent(t *testing.T) {
	db := testutils.SetupPostgresDB(t, 4)
	ctx := t.Context()
	repo, engine, projID := newPostgresEngine(t, db)

	defSvc := serviceimpl.NewDefinitionService(repo)
	if _, err := defSvc.CreateDefinition(ctx, paymentDefinition(projID, "order-payment-pg-idem")); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	if _, err := engine.StartProcess(ctx, projID, "order-payment-pg-idem", map[string]any{"orderId": "order-9"}); err != nil {
		t.Fatalf("start process: %v", err)
	}

	// Written by the fixed code, so already resolved and out of scope.
	first, err := serviceimpl.BackfillMessageCorrelationKeys(ctx, repo)
	if err != nil {
		t.Fatalf("first backfill: %v", err)
	}
	if first.Scanned != 0 {
		t.Errorf("a resolved key was selected for backfill on PostgreSQL: %+v", first)
	}

	second, err := serviceimpl.BackfillMessageCorrelationKeys(ctx, repo)
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if second.Scanned != 0 || second.Rewritten != 0 {
		t.Errorf("a repeat run did work it should not have: %+v", second)
	}
}
