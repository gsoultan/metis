package models

// MigrationModels returns the ordered list of GORM models that must be
// auto-migrated on every supported database backend.
//
// This single source-of-truth is consumed by both the normal application
// startup path (app.go) and the first-time setup wizard (setup.go) so that
// the two never drift apart.
func MigrationModels() []any {
	return []any{
		new(OrganizationModel),
		new(ProcessInstanceModel),
		new(TaskModel),
		new(ProcessDefinitionModel),
		new(ProjectModel),
		new(AuditModel),
		new(JobModel),
		new(IncidentModel),
		new(UserModel),
		new(GroupModel),
		new(MembershipModel),
		new(ExternalTaskModel),
		new(Subscription),
		new(DecisionDefinitionModel),
		new(Connector),
		new(ConnectorInstance),
		// NotificationModel must be present in all environments.
		// It was previously omitted from setup.go's migrateTargetDatabase,
		// causing runtime failures the first time a notification was written.
		new(NotificationModel),

		// The SSE fan-out bus. Without it a browser only sees events produced
		// by the replica it happens to be connected to.
		new(BroadcastEventModel),

		// Rate limits and circuit breakers, shared across replicas.
		new(SharedCounterModel),

		// These five were declared, given repositories and used by the
		// application, and left out of this list — so on a fresh installation
		// their tables were never created and the first deployment, form or
		// compensation failed with "no such table". Every test harness built its
		// schema from a fuller list of its own, which is why nothing caught it.
		// Migration 5 creates them on installations that started without them.
		new(DeploymentModel),
		new(ResourceModel),
		new(FormModel),
		new(VariableSnapshotModel),
		new(CompensatableActivityModel),

		// A service task's outbound call, recorded so a retry after a failed
		// commit does not make it a second time.
		new(ServiceCallModel),

		// The public inbound surface: an address a partner posts to, and the
		// deliveries already acted on so its retries do not act twice.
		new(WebhookModel),
		new(WebhookDeliveryModel),

		// A connector described by a document rather than by Go.
		new(ConnectorManifestModel),

		// What a caller's Idempotency-Key already produced. In the database
		// rather than in the process, so a retry landing on another replica
		// gets the original answer instead of executing the write again.
		new(IdempotencyRecordModel),
	}
}
