package services

import (
	"github.com/gsoultan/metis/server/domains/handlers/impl"
	observercontracts "github.com/gsoultan/metis/server/domains/observers/contracts"
	observerimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/domains/services/impl/connectors"
	"github.com/gsoultan/metis/server/repositories"
	"gorm.io/gorm"
)

type service struct {
	contracts.OrganizationService
	contracts.ProjectService
	contracts.DefinitionService
	contracts.TaskService
	contracts.ExecutionEngine
	contracts.JobService
	contracts.ExternalTaskService
	contracts.DecisionService
	contracts.MigrationService
	contracts.ConnectorService
	contracts.CollaborationService
	contracts.MessagingService
	contracts.WebhookService
	contracts.AdHocActivator
	contracts.UserService
	contracts.GroupService
	contracts.SetupService
	contracts.NotificationService
}

type ServiceParams struct {
	OrganizationService  contracts.OrganizationService
	ProjectService       contracts.ProjectService
	DefinitionService    contracts.DefinitionService
	TaskService          contracts.TaskService
	ExecutionEngine      contracts.ExecutionEngine
	JobService           contracts.JobService
	ExternalTaskService  contracts.ExternalTaskService
	DecisionService      contracts.DecisionService
	MigrationService     contracts.MigrationService
	ConnectorService     contracts.ConnectorService
	CollaborationService contracts.CollaborationService
	MessagingService     contracts.MessagingService
	WebhookService       contracts.WebhookService
	AdHocActivator       contracts.AdHocActivator
	UserService          contracts.UserService
	GroupService         contracts.GroupService
	SetupService         contracts.SetupService
	NotificationService  contracts.NotificationService
}

func NewService(p ServiceParams) ServiceFacade {
	return &service{
		OrganizationService:  p.OrganizationService,
		ProjectService:       p.ProjectService,
		DefinitionService:    p.DefinitionService,
		TaskService:          p.TaskService,
		ExecutionEngine:      p.ExecutionEngine,
		JobService:           p.JobService,
		ExternalTaskService:  p.ExternalTaskService,
		DecisionService:      p.DecisionService,
		MigrationService:     p.MigrationService,
		ConnectorService:     p.ConnectorService,
		CollaborationService: p.CollaborationService,
		MessagingService:     p.MessagingService,
		WebhookService:       p.WebhookService,
		AdHocActivator:       p.AdHocActivator,
		UserService:          p.UserService,
		GroupService:         p.GroupService,
		SetupService:         p.SetupService,
		NotificationService:  p.NotificationService,
	}
}

// NewServiceFacade creates and wires all sub-service implementations.
func NewServiceFacade(
	repo repositories.Repository,
	dispatcher observercontracts.EventDispatcher,
	sseObserver *observerimpl.SSEObserver,
	jwtSecret string,
	setupCallback func(*gorm.DB),
) ServiceFacade {
	orgSvc := serviceimpl.NewOrganizationService(repo)
	projectSvc := serviceimpl.NewProjectService(repo)
	defSvc := serviceimpl.NewDefinitionService(repo)
	migrationSvc := serviceimpl.NewMigrationService(repo)
	connectorSvc := serviceimpl.NewConnectorService(repo)
	connectorSvc.RegisterExecutor(connectors.HTTPConnectorKey, connectors.NewHTTPConnector(nil))
	connectorSvc.RegisterExecutor(connectors.SlackConnectorKey, connectors.NewSlackConnector())
	connectorSvc.RegisterExecutor(connectors.EmailConnectorKey, connectors.NewEmailConnector())
	feelEval := serviceimpl.NewFEELEvaluator()
	tableEval := serviceimpl.NewDecisionTableEvaluator(feelEval)
	collaborationSvc := serviceimpl.NewCollaborationService(sseObserver)

	// Create the engine with its non-circular mandatory dependencies.
	// NewExecutionEngine returns the concrete *Engine so the composition root
	// can call Apply() to inject circular collaborators after all are built.
	engine := serviceimpl.NewExecutionEngine(repo, dispatcher)
	varHistorySvc := serviceimpl.NewVariableHistoryService(repo.VariableSnapshot())

	auditWriter := serviceimpl.NewAuditWriter(repo.Audit())
	taskSvc := serviceimpl.NewTaskService(repo, engine, auditWriter)
	externalTaskSvc := serviceimpl.NewExternalTaskService(repo, engine)
	decisionSvc := serviceimpl.NewDecisionService(repo, tableEval)
	userSvc := serviceimpl.NewUserService(repo, jwtSecret)
	groupSvc := serviceimpl.NewGroupService(repo)
	messagingSvc := serviceimpl.NewMessagingService(engine, externalTaskSvc)
	webhookSvc := serviceimpl.NewWebhookService(repo, engine)
	adHocActivator := serviceimpl.NewAdHocActivator(engine)
	setupSvc := serviceimpl.NewSetupService(setupCallback)
	notificationSvc := serviceimpl.NewNotificationService(repo.Notification())

	// Resolve circular collaborators via functional options so the wiring is
	// grouped in one explicit call instead of scattered Set* method calls.
	//
	// NoOpLocker is a deliberate choice, not a placeholder. Job claiming is made
	// exactly-once by the conditional row update in jobRepository.Lock, which
	// holds on every supported dialect; a distributed lock on top would add a
	// round trip per job to the poll loop and decide nothing the row update has
	// not already decided. serviceimpl.PostgresLocker exists for work that has
	// no such row to arbitrate it — a single-owner background consumer — and is
	// the intended mechanism there. See docs/recovery.md §2.1.
	jobSvc := serviceimpl.NewJobService(repo, engine, connectorSvc, serviceimpl.NewNoOpLocker(), impl.NewErrorBoundaryMatcher())
	handlerFactory := impl.NewNodeHandlerFactory(engine, taskSvc, jobSvc, externalTaskSvc, decisionSvc, connectorSvc, repo.Subscription(), auditWriter)
	engine.Apply(
		serviceimpl.WithVariableHistoryService(varHistorySvc),
		serviceimpl.WithJobService(jobSvc),
		serviceimpl.WithHandlerFactory(handlerFactory),
	)

	return NewService(ServiceParams{
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
		WebhookService:       webhookSvc,
		AdHocActivator:       adHocActivator,
		UserService:          userSvc,
		GroupService:         groupSvc,
		SetupService:         setupSvc,
		NotificationService:  notificationSvc,
	})
}

// Ensure service implements ServiceFacade
var _ ServiceFacade = (*service)(nil)
