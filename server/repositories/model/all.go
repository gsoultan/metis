package model

// All is every table, in the order the generator should see them.
//
// A list rather than discovery-by-package, because adding a struct to this
// directory is not the same act as adding a table: the payload shapes that live
// inside jsonb columns — FlowNode, Token, DecisionRule and the rest — are
// structs here too, and must not become tables.
func All() []any {
	return []any{
		// Identity and configuration. These belong to the main database: who
		// may sign in, what they own, and where each runtime lives.
		&Organization{},
		&User{},
		&UserOrganization{},
		&UserProject{},
		&Group{},
		&Membership{},
		&Project{},
		&Environment{},

		// Who administers the installation, and what they may do. Platform
		// accounts are a different population from the people named in
		// processes: see PlatformUser for why they are not one table.
		&PlatformUser{},
		&PlatformRole{},
		&PlatformRoleAssignment{},

		// The connector catalogue is definitional and lives with the platform;
		// a ConnectorInstance is per project, and so per runtime.
		&Connector{},
		&ConnectorManifest{},
		&ConnectorInstance{},

		// Authored models: what a project has deployed.
		&ProcessDefinition{},
		&ProcessDefinitionRelease{},
		&DecisionDefinition{},
		&DecisionRelease{},
		&Form{},
		&Deployment{},
		&Resource{},

		// Who a process can assign work to. These live with the runtime, so a
		// project's environments each have their own participants.
		&WorkflowUser{},
		&WorkflowGroup{},
		&WorkflowGroupMembership{},
		&ParticipantSource{},

		// The runtime: what is actually executing, and what it produced.
		&ProcessInstance{},
		&Task{},
		&Job{},
		&Incident{},
		&Subscription{},
		&ExternalTask{},
		// Who has been fetching it.
		&ExternalTaskWorker{},
		&AuditEntry{},
		&VariableSnapshot{},
		&CompensatableActivity{},
		&ServiceCall{},
		// What was done to an instance that its process did not decide.
		&InstanceDeviation{},
		// Requests waiting for a second administrator.
		&DeviationRequest{},
		// Instances kept out of the retention sweep.
		&RetentionHold{},
		&RetentionPeriod{},
		// Saved simulation test cases, per process.
		&SimulationCase{},
		// One-use links that let a person act on a task without signing in.
		&TaskLink{},
		&PersonAPIKey{},
		// Acts asked for on many instances at once, and what came of each.
		&BatchOperation{},
		&BatchOperationItem{},

		// The inbound surface and the machinery underneath it.
		&Webhook{},
		&WebhookDelivery{},
		// The outbound surface: where a project's events are posted, and the
		// queue they are posted from.
		&OutgoingWebhook{},
		&OutgoingWebhookDelivery{},
		&Notification{},
		&IdempotencyRecord{},
		&SharedCounter{},
		&BroadcastEvent{},
	}
}
