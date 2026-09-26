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
		&AuditEntry{},
		&VariableSnapshot{},
		&CompensatableActivity{},
		&ServiceCall{},

		// The inbound surface and the machinery underneath it.
		&Webhook{},
		&WebhookDelivery{},
		&Notification{},
		&IdempotencyRecord{},
		&SharedCounter{},
		&BroadcastEvent{},
	}
}
