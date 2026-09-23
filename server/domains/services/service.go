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
	contracts.EnvironmentService
	contracts.WorkflowUserService
	contracts.ParticipantSyncService
	contracts.PlatformUserService
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
	contracts.SimulationService
}

type ServiceParams struct {
	OrganizationService    contracts.OrganizationService
	ProjectService         contracts.ProjectService
	DefinitionService      contracts.DefinitionService
	EnvironmentService     contracts.EnvironmentService
	WorkflowUserService    contracts.WorkflowUserService
	ParticipantSyncService contracts.ParticipantSyncService
	PlatformUserService    contracts.PlatformUserService
	TaskService            contracts.TaskService
	ExecutionEngine        contracts.ExecutionEngine
	JobService             contracts.JobService
	ExternalTaskService    contracts.ExternalTaskService
	DecisionService        contracts.DecisionService
	MigrationService       contracts.MigrationService
	ConnectorService       contracts.ConnectorService
	CollaborationService   contracts.CollaborationService
	MessagingService       contracts.MessagingService
	WebhookService         contracts.WebhookService
	AdHocActivator         contracts.AdHocActivator
	UserService            contracts.UserService
	GroupService           contracts.GroupService
	SetupService           contracts.SetupService
	NotificationService    contracts.NotificationService
	SimulationService      contracts.SimulationService
}

func NewService(p ServiceParams) ServiceFacade {
	return &service{
		OrganizationService:    p.OrganizationService,
		ProjectService:         p.ProjectService,
		DefinitionService:      p.DefinitionService,
		EnvironmentService:     p.EnvironmentService,
		WorkflowUserService:    p.WorkflowUserService,
		ParticipantSyncService: p.ParticipantSyncService,
		PlatformUserService:    p.PlatformUserService,
		TaskService:            p.TaskService,
		ExecutionEngine:        p.ExecutionEngine,
		JobService:             p.JobService,
		ExternalTaskService:    p.ExternalTaskService,
		DecisionService:        p.DecisionService,
		MigrationService:       p.MigrationService,
		ConnectorService:       p.ConnectorService,
		CollaborationService:   p.CollaborationService,
		MessagingService:       p.MessagingService,
		WebhookService:         p.WebhookService,
		AdHocActivator:         p.AdHocActivator,
		UserService:            p.UserService,
		GroupService:           p.GroupService,
		SetupService:           p.SetupService,
		NotificationService:    p.NotificationService,
		SimulationService:      p.SimulationService,
	}
}

// NewServiceFacade creates and wires all sub-service implementations.
func NewServiceFacade(
	repo repositories.Repository,
	dispatcher observercontracts.EventDispatcher,
	sseObserver *observerimpl.SSEObserver,
	jwtSecret string,
	// participants is the storm-backed directory, nil on any engine but
	// PostgreSQL. Passed in rather than constructed here because the storm
	// connection belongs to the composition root, which is the only place that
	// knows whether there is one.
	participants contracts.WorkflowUserService,
	// sync is the directory registry, nil for the same reason participants can
	// be: it is built on the storm connection.
	sync contracts.ParticipantSyncService,
	// accounts manages platform administrators, nil for the same reason: it is
	// storm-backed.
	accounts contracts.PlatformUserService,
	setupCallback func(*gorm.DB),
) ServiceFacade {
	if participants == nil {
		participants = serviceimpl.NewUnavailableWorkflowUserService()
	}
	if sync == nil {
		sync = serviceimpl.NewUnavailableParticipantSyncService()
	}
	if accounts == nil {
		accounts = serviceimpl.NewUnavailablePlatformUserService()
	}
	orgSvc := serviceimpl.NewOrganizationService(repo)
	projectSvc := serviceimpl.NewProjectService(repo)
	defSvc := serviceimpl.NewDefinitionService(repo)
	environmentSvc := serviceimpl.NewEnvironmentService(repo)
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
	// After the engine, because a migration that skips a node advances the
	// instance through the engine rather than reimplementing the advance.
	migrationSvc := serviceimpl.NewMigrationService(repo, engine)

	engine.Apply(
		serviceimpl.WithVariableHistoryService(varHistorySvc),
		serviceimpl.WithJobService(jobSvc),
		serviceimpl.WithHandlerFactory(handlerFactory),
	)
	// Deleting a definition drops the engine's decoded copy of it. Wired here
	// because this is the only place holding both, and a cache that outlives
	// its source keeps running something an administrator removed.
	defSvc.InvalidateWith(engine.ForgetDefinitions)

	// Simulation gets a *fresh* engine per run, built from the same parts as the
	// real one so it cannot quietly differ from it, but with two of them
	// swapped: its own dispatcher, so nothing fans out to browsers, webhooks or
	// notifications, and its own job service, so no connector is ever called and
	// no timer ever sleeps. The run itself is rolled back; see simulation.go.
	simulationSvc := serviceimpl.NewSimulationService(repo, func(
		dispatcher observercontracts.EventDispatcher,
		jobs contracts.JobService,
	) contracts.ExecutionEngine {
		simEngine := serviceimpl.NewExecutionEngine(repo, dispatcher)
		simTaskSvc := serviceimpl.NewTaskService(repo, simEngine, auditWriter)
		simExternalTaskSvc := serviceimpl.NewExternalTaskService(repo, simEngine)
		simEngine.Apply(
			serviceimpl.WithJobService(jobs),
			serviceimpl.WithHandlerFactory(impl.NewNodeHandlerFactory(
				simEngine, simTaskSvc, jobs, simExternalTaskSvc,
				decisionSvc, connectorSvc, repo.Subscription(), auditWriter,
			)),
		)
		return simEngine
	})

	return NewService(ServiceParams{
		OrganizationService:    orgSvc,
		ProjectService:         projectSvc,
		DefinitionService:      defSvc,
		EnvironmentService:     environmentSvc,
		WorkflowUserService:    participants,
		ParticipantSyncService: sync,
		PlatformUserService:    accounts,
		TaskService:            taskSvc,
		ExecutionEngine:        engine,
		JobService:             jobSvc,
		ExternalTaskService:    externalTaskSvc,
		DecisionService:        decisionSvc,
		MigrationService:       migrationSvc,
		ConnectorService:       connectorSvc,
		CollaborationService:   collaborationSvc,
		MessagingService:       messagingSvc,
		WebhookService:         webhookSvc,
		AdHocActivator:         adHocActivator,
		UserService:            userSvc,
		GroupService:           groupSvc,
		SetupService:           setupSvc,
		NotificationService:    notificationSvc,
		SimulationService:      simulationSvc,
	})
}

// Ensure service implements ServiceFacade
var _ ServiceFacade = (*service)(nil)
