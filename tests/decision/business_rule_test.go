package decision_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	handlersimpl "github.com/gsoultan/gobpm/server/domains/handlers/impl"
	"github.com/gsoultan/gobpm/server/domains/observers/impl"
	"github.com/gsoultan/gobpm/server/domains/services"
	service_impl2 "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/tests/testutils"
)

func TestBusinessRuleTaskMapping(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()

	repo := repositories.NewRepository(db)
	dispatcher := impl.NewEventDispatcher()

	orgSvc := service_impl2.NewOrganizationService(repo)
	projectSvc := service_impl2.NewProjectService(repo)
	defSvc := service_impl2.NewDefinitionService(repo)
	connectorSvc := service_impl2.NewConnectorService(repo)
	engine := service_impl2.NewExecutionEngine(repo, dispatcher)
	taskSvc := service_impl2.NewTaskService(repo, engine, service_impl2.NewAuditWriter(repo.Audit()))
	jobSvc := service_impl2.NewJobService(repo, engine, connectorSvc, service_impl2.NewNoOpLocker(), handlersimpl.NewErrorBoundaryMatcher())
	externalTaskSvc := service_impl2.NewExternalTaskService(repo, engine)
	decisionSvc := service_impl2.NewDecisionService(repo, service_impl2.NewDecisionTableEvaluator(service_impl2.NewFEELEvaluator()))
	migrationSvc := service_impl2.NewMigrationService(repo)
	sse := impl.NewSSEObserver()
	collaborationSvc := service_impl2.NewCollaborationService(sse)

	handlerFactory := handlersimpl.NewNodeHandlerFactory(engine, taskSvc, jobSvc, externalTaskSvc, decisionSvc, connectorSvc, service_impl2.NewFEELEvaluator(), repo.Subscription())
	engine.Apply(
		service_impl2.WithHandlerFactory(handlerFactory),
		service_impl2.WithJobService(jobSvc),
	)

	messagingSvc := service_impl2.NewMessagingService(engine, externalTaskSvc)
	userSvc := service_impl2.NewUserService(repo, "test-jwt-secret")
	setupSvc := service_impl2.NewSetupService(nil)
	svc := services.NewService(services.ServiceParams{
		OrganizationService:  orgSvc,
		ProjectService:       projectSvc,
		DefinitionService:    defSvc,
		TaskService:          taskSvc,
		ExecutionEngine:      engine,
		JobService:           jobSvc,
		ExternalTaskService:  externalTaskSvc,
		DecisionService:      decisionSvc,
		MigrationService:     migrationSvc,
		ConnectorService:     connectorSvc,
		CollaborationService: collaborationSvc,
		MessagingService:     messagingSvc,
		UserService:          userSvc,
		SetupService:         setupSvc,
	})

	org, _ := svc.CreateOrganization(ctx, "Test Org", "")
	proj, _ := svc.CreateProject(ctx, org.ID, "Test Project", "")

	// 1. Setup Decision
	decision := entities.DecisionDefinition{
		ID:      uuid.Must(uuid.NewV7()),
		Project: &entities.Project{ID: proj.ID},
		Key:     "checkAmount",
		Name:    "Check Amount",
		Inputs: []entities.DecisionInput{
			{ID: "in1", Label: "Amount", Expression: "amount", Type: "number"},
		},
		Outputs: []entities.DecisionOutput{
			{ID: "out1", Label: "Approved", Name: "approved", Type: "boolean"},
		},
		Rules: []entities.DecisionRule{
			{ID: "r1", Inputs: []string{"> 100"}, Outputs: []any{true}},
			{ID: "r2", Inputs: []string{"<= 100"}, Outputs: []any{false}},
		},
	}
	_, err := svc.CreateDecision(ctx, decision)
	if err != nil {
		t.Fatalf("failed to create decision: %v", err)
	}

	// 2. Setup Process with Business Rule Task and Mapping
	nodes := []*entities.Node{
		{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
		{
			ID:       "rule1",
			Type:     entities.BusinessRuleTask,
			Incoming: []string{"f1"},
			Outgoing: []string{"f2"},
			Properties: map[string]any{
				"decision_key": "checkAmount",
				"input_mapping": map[string]any{
					"amount": "orderAmount", // Map process var 'orderAmount' to decision input 'amount'
				},
				"output_mapping": map[string]any{
					"isApproved": "approved", // Map decision output 'approved' to process var 'isApproved'
				},
			},
		},
		{ID: "end", Type: entities.EndEvent, Incoming: []string{"f2"}},
	}

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: proj.ID},
		Key:     "ruleProcess",
		Name:    "Rule Process",
		Nodes:   nodes,
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "rule1"},
			{ID: "f2", SourceRef: "rule1", TargetRef: "end"},
		},
	}
	_, err = svc.CreateDefinition(ctx, &def)
	if err != nil {
		t.Fatalf("failed to create definition: %v", err)
	}

	// 3. Start Process with variables
	vars := map[string]any{"orderAmount": 150}
	instanceID, err := svc.StartProcess(ctx, proj.ID, "ruleProcess", vars)
	if err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	// 4. Verify results
	instance, _ := svc.GetInstance(ctx, instanceID)
	if instance.Status != "completed" {
		t.Errorf("expected instance to be completed, got %s", instance.Status)
	}

	isApproved, ok := instance.Variables["isApproved"].(bool)
	if !ok || !isApproved {
		t.Errorf("expected isApproved to be true, got %v", instance.Variables["isApproved"])
	}

	// 5. Test another case (not approved)
	vars2 := map[string]any{"orderAmount": 50}
	instanceID2, _ := svc.StartProcess(ctx, proj.ID, "ruleProcess", vars2)
	instance2, _ := svc.GetInstance(ctx, instanceID2)

	isApproved2, _ := instance2.Variables["isApproved"].(bool)
	if isApproved2 {
		t.Errorf("expected isApproved to be false for amount 50, got %v", isApproved2)
	}
}
