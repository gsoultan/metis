package services

import (
	serviceContracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// ServiceFacade is the main interface for the Metis system, aggregating all sub-services.
type ServiceFacade interface {
	serviceContracts.OrganizationService
	serviceContracts.ProjectService
	serviceContracts.DefinitionService

	// EnvironmentService manages the runtimes a project deploys into. Each names
	// its own database, so what one environment holds is absent from another
	// rather than filtered out of it.
	serviceContracts.EnvironmentService

	// WorkflowUserService manages the people a project's processes assign work
	// to — a different population from the accounts that administer Metis.
	serviceContracts.WorkflowUserService

	// ParticipantSyncService manages the directories a project keeps its
	// participants in step with, and runs them.
	serviceContracts.ParticipantSyncService

	// PlatformUserService manages the accounts that administer Metis, and what
	// each of them may do. Administrative in full: every method here changes who
	// can reconfigure the installation.
	serviceContracts.PlatformUserService

	// PeopleSignInService signs People in to their project, apart from the
	// accounts above: a People token is accepted by People routes alone.
	serviceContracts.PeopleSignInService
	serviceContracts.TaskService

	// PeopleTaskService is the work a signed-in person does, in their project.
	serviceContracts.PeopleTaskService

	// TaskLinkService lets a person act on one task, once, through a link.
	serviceContracts.TaskLinkService
	// PersonAPIKeyService is the keys People give their own systems.
	serviceContracts.PersonAPIKeyService
	// PeopleSwitchOverService finds and fills what People lack before Users
	// stop doing tasks.
	serviceContracts.PeopleSwitchOverService
	serviceContracts.ExecutionEngine
	serviceContracts.JobService
	serviceContracts.ExternalTaskService
	serviceContracts.DecisionService
	serviceContracts.MigrationService
	serviceContracts.ConnectorService
	serviceContracts.CollaborationService
	serviceContracts.MessagingService

	// WebhookService receives events partners post to the public hook endpoint.
	serviceContracts.WebhookService

	// OutgoingWebhookService manages the addresses a project posts its events
	// to, and OutgoingWebhookDeliverer posts what each is owed.
	serviceContracts.OutgoingWebhookService
	serviceContracts.OutgoingWebhookDeliverer
	serviceContracts.AdHocActivator
	serviceContracts.UserService
	serviceContracts.GroupService
	serviceContracts.SetupService
	serviceContracts.NotificationService

	// SimulationService runs a deployed definition on the real engine against a
	// virtual clock, persisting nothing and calling nothing. It is on the facade
	// because it is a first-class product surface — the Go SDK calls it, and CI
	// gates on it — not a test affordance.
	serviceContracts.SimulationService
	// SimulationCaseService keeps the test cases each process is simulated against.
	serviceContracts.SimulationCaseService
	// ReleaseService moves process and decision versions from one environment
	// to the next together: cut, import (staged), go live, roll back.
	serviceContracts.ReleaseService

	// DeviationService reads what was done to one instance that its process did
	// not decide, and waives, cancels or holds one instance where it stands
	// (DeviateInstance). Writing a ledger row is not on the facade: every
	// writer, the in-place command included, records through DeviationRecorder
	// inside its own change.
	serviceContracts.DeviationService

	// RetentionHoldService keeps instances out of the finished-instance
	// sweep, an administrator's act with a reason, written to the trail.
	serviceContracts.RetentionHoldService
	// BatchOperationService retries or cancels many instances at once, and
	// keeps what came of each.
	serviceContracts.BatchOperationService
	// OperationLogService reads the audit log: who did what across the
	// organization.
	serviceContracts.OperationLogService

	// RetentionPeriodService gives a process a retention period of its own,
	// an administrator's act with a reason, every change kept.
	serviceContracts.RetentionPeriodService
	// ArchiveService reads the finished instances the retention sweep
	// archived.
	serviceContracts.ArchiveService

	// InstallationSettingService reads and changes the server settings on the
	// Settings page: a platform administrator's act with a reason, every
	// change kept, no secret ever returned.
	serviceContracts.InstallationSettingService
}
