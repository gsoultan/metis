package repositories

import (
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/gorms"
	"gorm.io/gorm"
)

type gormRepository struct {
	audit                 contracts.AuditRepository
	broadcast             contracts.BroadcastRepository
	connector             contracts.ConnectorRepository
	connectorInstance     contracts.ConnectorInstanceRepository
	decision              contracts.DecisionRepository
	definition            contracts.DefinitionRepository
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
func NewRepository(db *gorm.DB) Repository {
	return &gormRepository{
		audit:                 gorms.NewAuditRepository(db),
		broadcast:             gorms.NewBroadcastRepository(db),
		connector:             gorms.NewConnectorRepository(db),
		connectorInstance:     gorms.NewConnectorInstanceRepository(db),
		decision:              gorms.NewDecisionRepository(db),
		definition:            gorms.NewDefinitionRepository(db),
		deployment:            gorms.NewDeploymentRepository(db),
		externalTask:          gorms.NewExternalTaskRepository(db),
		form:                  gorms.NewFormRepository(db),
		incident:              gorms.NewIncidentRepository(db),
		job:                   gorms.NewJobRepository(db),
		organization:          gorms.NewOrganizationRepository(db),
		process:               gorms.NewProcessRepository(db),
		serviceCall:           gorms.NewServiceCallRepository(db),
		webhook:               gorms.NewWebhookRepository(db),
		connectorManifest:     gorms.NewConnectorManifestRepository(db),
		project:               gorms.NewProjectRepository(db),
		subscription:          gorms.NewSubscriptionRepository(db),
		task:                  gorms.NewTaskRepository(db),
		user:                  gorms.NewUserRepository(db),
		group:                 gorms.NewGroupRepository(db),
		notification:          gorms.NewNotificationRepository(db),
		compensatableActivity: gorms.NewCompensatableActivityRepository(db),
		variableSnapshot:      gorms.NewVariableSnapshotRepository(db),
		uow:                   gorms.NewUnitOfWork(db),
	}
}

func (r *gormRepository) Audit() contracts.AuditRepository         { return r.audit }
func (r *gormRepository) Broadcast() contracts.BroadcastRepository { return r.broadcast }
func (r *gormRepository) Connector() contracts.ConnectorRepository { return r.connector }
func (r *gormRepository) ConnectorInstance() contracts.ConnectorInstanceRepository {
	return r.connectorInstance
}
func (r *gormRepository) Decision() contracts.DecisionRepository         { return r.decision }
func (r *gormRepository) Definition() contracts.DefinitionRepository     { return r.definition }
func (r *gormRepository) Deployment() contracts.DeploymentRepository     { return r.deployment }
func (r *gormRepository) ExternalTask() contracts.ExternalTaskRepository { return r.externalTask }
func (r *gormRepository) Form() contracts.FormRepository                 { return r.form }
func (r *gormRepository) Incident() contracts.IncidentRepository         { return r.incident }
func (r *gormRepository) Job() contracts.JobRepository                   { return r.job }
func (r *gormRepository) Organization() contracts.OrganizationRepository { return r.organization }
func (r *gormRepository) Process() contracts.ProcessRepository           { return r.process }
func (r *gormRepository) ServiceCall() contracts.ServiceCallRepository   { return r.serviceCall }
func (r *gormRepository) Webhook() contracts.WebhookRepository           { return r.webhook }
func (r *gormRepository) ConnectorManifest() contracts.ConnectorManifestRepository {
	return r.connectorManifest
}
func (r *gormRepository) Project() contracts.ProjectRepository           { return r.project }
func (r *gormRepository) Subscription() contracts.SubscriptionRepository { return r.subscription }
func (r *gormRepository) Task() contracts.TaskRepository                 { return r.task }
func (r *gormRepository) User() contracts.UserRepository                 { return r.user }
func (r *gormRepository) Group() contracts.GroupRepository               { return r.group }
func (r *gormRepository) Notification() contracts.NotificationRepository { return r.notification }
func (r *gormRepository) CompensatableActivity() contracts.CompensatableActivityRepository {
	return r.compensatableActivity
}
func (r *gormRepository) VariableSnapshot() contracts.VariableSnapshotRepository {
	return r.variableSnapshot
}
func (r *gormRepository) UnitOfWork() contracts.UnitOfWork { return r.uow }
