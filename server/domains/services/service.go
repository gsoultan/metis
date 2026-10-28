package services

import (
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/server/domains/handlers/impl"
	observercontracts "github.com/gsoultan/metis/server/domains/observers/contracts"
	observerimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
)

type service struct {
	contracts.OrganizationService
	contracts.ProjectService
	contracts.DefinitionService
	contracts.EnvironmentService
	contracts.WorkflowUserService
	contracts.ParticipantSyncService
	contracts.PlatformUserService
	contracts.PeopleSignInService
	contracts.TaskService
	contracts.PeopleTaskService
	contracts.TaskLinkService
	contracts.PeopleSwitchOverService
	contracts.PersonAPIKeyService
	contracts.ExecutionEngine
	contracts.JobService
	contracts.ExternalTaskService
	contracts.DecisionService
	contracts.MigrationService
	contracts.ConnectorService
	contracts.CollaborationService
	contracts.MessagingService
	contracts.WebhookService
	contracts.OutgoingWebhookService
	contracts.OutgoingWebhookDeliverer
	contracts.AdHocActivator
	contracts.UserService
	contracts.GroupService
	contracts.SetupService
	contracts.NotificationService
	contracts.SimulationService
	contracts.SimulationCaseService
	contracts.DeviationService
	contracts.RetentionHoldService
	contracts.RetentionPeriodService
	contracts.InstallationSettingService
	contracts.BatchOperationService
	contracts.OperationLogService
}

type ServiceParams struct {
	OrganizationService    contracts.OrganizationService
	ProjectService         contracts.ProjectService
	DefinitionService      contracts.DefinitionService
	EnvironmentService     contracts.EnvironmentService
	WorkflowUserService    contracts.WorkflowUserService
	ParticipantSyncService contracts.ParticipantSyncService
	PlatformUserService    contracts.PlatformUserService
	PeopleSignInService    contracts.PeopleSignInService
	TaskService            contracts.TaskService
	PeopleTaskService      contracts.PeopleTaskService
	TaskLinkService        contracts.TaskLinkService
	PeopleSwitchOver       contracts.PeopleSwitchOverService
	PersonAPIKeys          contracts.PersonAPIKeyService
	ExecutionEngine        contracts.ExecutionEngine
	JobService             contracts.JobService
	ExternalTaskService    contracts.ExternalTaskService
	DecisionService        contracts.DecisionService
	MigrationService       contracts.MigrationService
	ConnectorService       contracts.ConnectorService
	CollaborationService   contracts.CollaborationService
	MessagingService       contracts.MessagingService
	WebhookService         contracts.WebhookService
	OutgoingWebhooks       contracts.OutgoingWebhookService
	OutgoingDeliveries     contracts.OutgoingWebhookDeliverer
	AdHocActivator         contracts.AdHocActivator
	UserService            contracts.UserService
	GroupService           contracts.GroupService
	SetupService           contracts.SetupService
	NotificationService    contracts.NotificationService
	SimulationService      contracts.SimulationService
	SimulationCaseService  contracts.SimulationCaseService
	DeviationService       contracts.DeviationService
	RetentionHoldService   contracts.RetentionHoldService
	RetentionPeriodService contracts.RetentionPeriodService
	// InstallationSettingService is the Settings page's server settings.
	InstallationSettingService contracts.InstallationSettingService
	BatchOperationService      contracts.BatchOperationService
	OperationLogService        contracts.OperationLogService
}

func NewService(p ServiceParams) ServiceFacade {
	// The facade embeds each service as an interface, and a call through one
	// that was not given is a nil dereference. The deviation service decides
	// who may loosen a rule: built without it, the facade answers every one
	// of its methods with a plain error that says so.
	if p.DeviationService == nil {
		p.DeviationService = serviceimpl.NewDeviationService(nil, nil, nil)
	}
	if p.RetentionHoldService == nil {
		p.RetentionHoldService = serviceimpl.NewUnavailableRetentionHoldService()
	}
	if p.RetentionPeriodService == nil {
		p.RetentionPeriodService = serviceimpl.NewUnavailableRetentionPeriodService()
	}
	if p.InstallationSettingService == nil {
		p.InstallationSettingService = serviceimpl.NewUnavailableInstallationSettingService()
	}
	if p.BatchOperationService == nil {
		p.BatchOperationService = serviceimpl.NewUnavailableBatchOperationService()
	}
	if p.OperationLogService == nil {
		p.OperationLogService = serviceimpl.NewUnavailableOperationLogService()
	}
	if p.PeopleTaskService == nil {
		p.PeopleTaskService = serviceimpl.NewUnavailablePeopleTaskService()
	}
	if p.TaskLinkService == nil {
		p.TaskLinkService = serviceimpl.NewUnavailableTaskLinkService()
	}
	if p.PersonAPIKeys == nil {
		p.PersonAPIKeys = serviceimpl.PersonAPIKeysOf(p.PeopleSignInService)
	}
	if p.PeopleSwitchOver == nil {
		p.PeopleSwitchOver = serviceimpl.NewUnavailablePeopleSwitchOverService()
	}
	if p.PeopleSignInService == nil {
		p.PeopleSignInService = serviceimpl.NewUnavailablePeopleSignInService(
			"people cannot sign in: this server was wired without the People sign-in")
	}
	if p.SimulationCaseService == nil {
		p.SimulationCaseService = serviceimpl.NewUnavailableSimulationCaseService()
	}
	return &service{
		OrganizationService:        p.OrganizationService,
		ProjectService:             p.ProjectService,
		DefinitionService:          p.DefinitionService,
		EnvironmentService:         p.EnvironmentService,
		WorkflowUserService:        p.WorkflowUserService,
		ParticipantSyncService:     p.ParticipantSyncService,
		PlatformUserService:        p.PlatformUserService,
		PeopleSignInService:        p.PeopleSignInService,
		TaskService:                p.TaskService,
		PeopleTaskService:          p.PeopleTaskService,
		TaskLinkService:            p.TaskLinkService,
		PeopleSwitchOverService:    p.PeopleSwitchOver,
		PersonAPIKeyService:        p.PersonAPIKeys,
		ExecutionEngine:            p.ExecutionEngine,
		JobService:                 p.JobService,
		ExternalTaskService:        p.ExternalTaskService,
		DecisionService:            p.DecisionService,
		MigrationService:           p.MigrationService,
		ConnectorService:           p.ConnectorService,
		CollaborationService:       p.CollaborationService,
		MessagingService:           p.MessagingService,
		WebhookService:             p.WebhookService,
		OutgoingWebhookService:     p.OutgoingWebhooks,
		OutgoingWebhookDeliverer:   p.OutgoingDeliveries,
		AdHocActivator:             p.AdHocActivator,
		UserService:                p.UserService,
		GroupService:               p.GroupService,
		SetupService:               p.SetupService,
		NotificationService:        p.NotificationService,
		SimulationService:          p.SimulationService,
		SimulationCaseService:      p.SimulationCaseService,
		DeviationService:           p.DeviationService,
		RetentionHoldService:       p.RetentionHoldService,
		RetentionPeriodService:     p.RetentionPeriodService,
		InstallationSettingService: p.InstallationSettingService,
		BatchOperationService:      p.BatchOperationService,
		OperationLogService:        p.OperationLogService,
	}
}

// facadeSettings is what the services are built with beyond their
// collaborators: what whoever puts the server together read of its
// configuration and hands on. The services read no setting of this kind
// themselves.
type facadeSettings struct {
	// soleAdministratorOrganizations are the organizations whose only
	// administrator may approve a request they asked for themselves.
	soleAdministratorOrganizations []uuid.UUID

	// peopleSignIn signs People in. Nil leaves them unable to: see
	// WithPeopleSignIn.
	peopleSignIn contracts.PeopleSignInService

	// taskLinkLifetime is how long a task link is good for. Zero is
	// serviceimpl.DefaultTaskLinkLifetime: see WithTaskLinkLifetime.
	taskLinkLifetime time.Duration
}

// FacadeOption changes what the services are built with.
type FacadeOption func(*facadeSettings)

// WithSoleAdministratorOrganizations names the organizations whose only
// administrator may approve a request they asked for themselves
// (serviceimpl.EnvSoleAdministratorOrganizations). Built without it, the
// services allow that in no organization, whatever the environment says:
// the server reads the list once, when it starts, announces it, and passes
// that same reading here.
func WithSoleAdministratorOrganizations(organizations []uuid.UUID) FacadeOption {
	return func(settings *facadeSettings) { settings.soleAdministratorOrganizations = organizations }
}

// WithPeopleSignIn is how People sign in. Built without it, nobody can sign in
// as a person: the server has no key to seal their tokens with, or no People
// directory to check them against, and refuses rather than guessing.
func WithPeopleSignIn(signIn contracts.PeopleSignInService) FacadeOption {
	return func(settings *facadeSettings) { settings.peopleSignIn = signIn }
}

// WithTaskLinkLifetime is how long a task link is good for once issued. Built
// without it, a link lasts serviceimpl.DefaultTaskLinkLifetime.
func WithTaskLinkLifetime(lifetime time.Duration) FacadeOption {
	return func(settings *facadeSettings) { settings.taskLinkLifetime = lifetime }
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
	options ...FacadeOption,
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
	groupSvc := serviceimpl.NewGroupService(repo, serviceimpl.PrincipalForgetter(userSvc))
	messagingSvc := serviceimpl.NewMessagingService(engine, externalTaskSvc)
	webhookSvc := serviceimpl.NewWebhookService(repo, engine)
	adHocActivator := serviceimpl.NewAdHocActivator(engine, repo)
	setupSvc := serviceimpl.NewSetupService(repo.User(), envvar.Get(serviceimpl.EnvSetupToken))
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
	handlerFactory := impl.NewNodeHandlerFactory(engine, taskSvc, jobSvc, externalTaskSvc, decisionSvc, repo.Subscription(), auditWriter)
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
				decisionSvc, repo.Subscription(), auditWriter,
			)),
		)
		return simEngine
	})

	deviationLedger := serviceimpl.NewDeviationLedger(repo)
	instanceDeviator := serviceimpl.NewInstanceDeviationService(repo, engine)
	var settings facadeSettings
	for _, option := range options {
		option(&settings)
	}

	return NewService(ServiceParams{
		OrganizationService:    orgSvc,
		ProjectService:         projectSvc,
		DefinitionService:      defSvc,
		EnvironmentService:     environmentSvc,
		WorkflowUserService:    participants,
		ParticipantSyncService: sync,
		PlatformUserService:    accounts,
		PeopleSignInService:    settings.peopleSignIn,
		TaskService:            taskSvc,
		PeopleTaskService:      serviceimpl.PeopleTasksOf(taskSvc),
		TaskLinkService:        serviceimpl.TaskLinksOf(taskSvc, settings.taskLinkLifetime),
		PeopleSwitchOver:       serviceimpl.PeopleSwitchOverOf(repo, participants),
		ExecutionEngine:        engine,
		JobService:             jobSvc,
		ExternalTaskService:    externalTaskSvc,
		DecisionService:        decisionSvc,
		MigrationService:       migrationSvc,
		ConnectorService:       connectorSvc,
		CollaborationService:   collaborationSvc,
		MessagingService:       messagingSvc,
		WebhookService:         webhookSvc,
		OutgoingWebhooks: serviceimpl.NewOutgoingWebhookService(repo.OutgoingWebhook(),
			serviceimpl.ResolveOutgoingWebhookSettings()),
		OutgoingDeliveries: serviceimpl.NewOutgoingWebhookDeliverer(repo.OutgoingWebhookQueue(),
			serviceimpl.ResolveOutgoingWebhookSettings()),
		AdHocActivator:        adHocActivator,
		UserService:           userSvc,
		GroupService:          groupSvc,
		SetupService:          setupSvc,
		NotificationService:   notificationSvc,
		SimulationService:     simulationSvc,
		SimulationCaseService: serviceimpl.NewSimulationCaseService(repo),
		DeviationService: serviceimpl.NewDeviationService(deviationLedger,
			instanceDeviator,
			serviceimpl.NewDeviationRequestService(repo, engine,
				serviceimpl.WithSoleAdministratorOrganizations(settings.soleAdministratorOrganizations))),
		RetentionHoldService:       serviceimpl.NewRetentionHoldService(repo, auditWriter),
		RetentionPeriodService:     serviceimpl.NewRetentionPeriodService(repo),
		InstallationSettingService: serviceimpl.NewInstallationSettingService(repo.InstallationSetting()),
		BatchOperationService: serviceimpl.NewBatchOperationService(repo, instanceDeviator, jobSvc,
			serviceimpl.AccountLoader(userSvc), serviceimpl.DefaultBatchOperationSettings()),
		OperationLogService: serviceimpl.NewOperationLogService(repo),
	})
}

// Ensure service implements ServiceFacade
var _ ServiceFacade = (*service)(nil)
