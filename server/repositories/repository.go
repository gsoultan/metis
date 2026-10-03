package repositories

import (
	"github.com/gsoultan/metis/server/repositories/contracts"
	stormdb "github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/pg"
)

type repository struct {
	audit                 contracts.AuditRepository
	deviation             contracts.DeviationRepository
	broadcast             contracts.BroadcastRepository
	sharedCounter         contracts.SharedCounterRepository
	connector             contracts.ConnectorRepository
	connectorInstance     contracts.ConnectorInstanceRepository
	decision              contracts.DecisionRepository
	definition            contracts.DefinitionRepository
	environment           contracts.EnvironmentRepository
	deployment            contracts.DeploymentRepository
	externalTask          contracts.ExternalTaskRepository
	form                  contracts.FormRepository
	incident              contracts.IncidentRepository
	job                   contracts.JobRepository
	organization          contracts.OrganizationRepository
	process               contracts.ProcessRepository
	serviceCall           contracts.ServiceCallRepository
	webhook               contracts.WebhookRepository
	connectorManifest     contracts.ConnectorManifestRepository
	project               contracts.ProjectRepository
	subscription          contracts.SubscriptionRepository
	task                  contracts.TaskRepository
	user                  contracts.UserRepository
	group                 contracts.GroupRepository
	notification          contracts.NotificationRepository
	compensatableActivity contracts.CompensatableActivityRepository
	variableSnapshot      contracts.VariableSnapshotRepository
	uow                   contracts.UnitOfWork
}

// NewRepository creates a new composite repository.
//
// Two connections while the port is under way: the GORM one for the
// repositories that have not moved and the storm one for those that have. They
// are the same database — the composition root resolves the DSN once — which is
// what lets a repository move without the services calling it changing.
//
// A nil storm connection means the ported repositories are unavailable, which
// is a programming error rather than a configuration one now that PostgreSQL is
// the only engine. It panics rather than falling back to GORM: a fallback would
// mean the tests exercise one implementation and production the other.
func NewRepository(conn *stormdb.Conn) Repository {
	if conn == nil {
		panic("repositories: a storm connection is required; the ported repositories have no GORM implementation left")
	}
	return &repository{
		audit:                 pg.NewAuditRepository(conn),
		deviation:             pg.NewDeviationRepository(conn),
		broadcast:             pg.NewBroadcastRepository(conn),
		sharedCounter:         pg.NewSharedCounterRepository(conn),
		connector:             pg.NewConnectorRepository(conn),
		connectorInstance:     pg.NewConnectorInstanceRepository(conn),
		decision:              pg.NewDecisionRepository(conn),
		definition:            pg.NewDefinitionRepository(conn),
		environment:           pg.NewEnvironmentRepository(conn),
		deployment:            pg.NewDeploymentRepository(conn),
		externalTask:          pg.NewExternalTaskRepository(conn),
		form:                  pg.NewFormRepository(conn),
		incident:              pg.NewIncidentRepository(conn),
		job:                   pg.NewJobRepository(conn),
		organization:          pg.NewOrganizationRepository(conn),
		process:               pg.NewProcessRepository(conn),
		serviceCall:           pg.NewServiceCallRepository(conn),
		webhook:               pg.NewWebhookRepository(conn),
		connectorManifest:     pg.NewConnectorManifestRepository(conn),
		project:               pg.NewProjectRepository(conn),
		subscription:          pg.NewSubscriptionRepository(conn),
		task:                  pg.NewTaskRepository(conn),
		user:                  pg.NewUserRepository(conn),
		group:                 pg.NewGroupRepository(conn),
		notification:          pg.NewNotificationRepository(conn),
		compensatableActivity: pg.NewCompensatableActivityRepository(conn),
		variableSnapshot:      pg.NewVariableSnapshotRepository(conn),
		uow:                   newUnitOfWork(conn),
	}
}

func (r *repository) Audit() contracts.AuditRepository         { return r.audit }
func (r *repository) Deviation() contracts.DeviationRepository { return r.deviation }
func (r *repository) Broadcast() contracts.BroadcastRepository { return r.broadcast }
func (r *repository) SharedCounter() contracts.SharedCounterRepository {
	return r.sharedCounter
}
func (r *repository) Connector() contracts.ConnectorRepository { return r.connector }
func (r *repository) ConnectorInstance() contracts.ConnectorInstanceRepository {
	return r.connectorInstance
}
func (r *repository) Decision() contracts.DecisionRepository         { return r.decision }
func (r *repository) Definition() contracts.DefinitionRepository     { return r.definition }
func (r *repository) Environment() contracts.EnvironmentRepository   { return r.environment }
func (r *repository) Deployment() contracts.DeploymentRepository     { return r.deployment }
func (r *repository) ExternalTask() contracts.ExternalTaskRepository { return r.externalTask }
func (r *repository) Form() contracts.FormRepository                 { return r.form }
func (r *repository) Incident() contracts.IncidentRepository         { return r.incident }
func (r *repository) Job() contracts.JobRepository                   { return r.job }
func (r *repository) Organization() contracts.OrganizationRepository { return r.organization }
func (r *repository) Process() contracts.ProcessRepository           { return r.process }
func (r *repository) ServiceCall() contracts.ServiceCallRepository   { return r.serviceCall }
func (r *repository) Webhook() contracts.WebhookRepository           { return r.webhook }
func (r *repository) ConnectorManifest() contracts.ConnectorManifestRepository {
	return r.connectorManifest
}
func (r *repository) Project() contracts.ProjectRepository           { return r.project }
func (r *repository) Subscription() contracts.SubscriptionRepository { return r.subscription }
func (r *repository) Task() contracts.TaskRepository                 { return r.task }
func (r *repository) User() contracts.UserRepository                 { return r.user }
func (r *repository) Group() contracts.GroupRepository               { return r.group }
func (r *repository) Notification() contracts.NotificationRepository { return r.notification }
func (r *repository) CompensatableActivity() contracts.CompensatableActivityRepository {
	return r.compensatableActivity
}
func (r *repository) VariableSnapshot() contracts.VariableSnapshotRepository {
	return r.variableSnapshot
}
func (r *repository) UnitOfWork() contracts.UnitOfWork { return r.uow }
