package services

import (
	serviceContracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// ServiceFacade is the main interface for the Metis system, aggregating all sub-services.
type ServiceFacade interface {
	serviceContracts.OrganizationService
	serviceContracts.ProjectService
	serviceContracts.DefinitionService
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
}
