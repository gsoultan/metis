package repositories

import "github.com/gsoultan/metis/server/repositories/contracts"

// Repository defines the composite repository interface.
type Repository interface {
	Audit() contracts.AuditRepository

	// Deviation is what was done to an instance that its process did not decide;
	// written only inside the transaction that does it.
	Deviation() contracts.DeviationRepository

	// SharedCounter holds the counts that rate limits and circuit breakers
	// enforce across replicas rather than per process.
	SharedCounter() contracts.SharedCounterRepository

	// Broadcast is the SSE fan-out bus: it carries an event produced on one
	// replica to browsers connected to another.
	Broadcast() contracts.BroadcastRepository
	Connector() contracts.ConnectorRepository
	ConnectorInstance() contracts.ConnectorInstanceRepository
	Decision() contracts.DecisionRepository
	Definition() contracts.DefinitionRepository

	// Environment lists the runtimes a project deploys into. These rows live in
	// the main database; the data each environment holds is in the database it
	// names.
	Environment() contracts.EnvironmentRepository
	Deployment() contracts.DeploymentRepository
	ExternalTask() contracts.ExternalTaskRepository
	Form() contracts.FormRepository
	Incident() contracts.IncidentRepository
	Job() contracts.JobRepository
	Organization() contracts.OrganizationRepository
	Process() contracts.ProcessRepository

	// ServiceCall remembers a service task's outbound call across job attempts,
	// so a retry after a failed commit does not make it a second time.
	ServiceCall() contracts.ServiceCallRepository

	// Webhook stores the public addresses partners post events to, and the
	// deliveries already acted on.
	Webhook() contracts.WebhookRepository

	// ConnectorManifest stores connectors described by a document rather than
	// by Go.
	ConnectorManifest() contracts.ConnectorManifestRepository
	Project() contracts.ProjectRepository
	Subscription() contracts.SubscriptionRepository
	Task() contracts.TaskRepository
	User() contracts.UserRepository
	Group() contracts.GroupRepository
	Notification() contracts.NotificationRepository
	CompensatableActivity() contracts.CompensatableActivityRepository
	VariableSnapshot() contracts.VariableSnapshotRepository
	UnitOfWork() contracts.UnitOfWork
}
