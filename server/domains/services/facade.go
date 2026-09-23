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
	serviceContracts.TaskService
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
}
