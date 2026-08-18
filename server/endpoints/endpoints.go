package endpoints

import (
	"github.com/go-kit/kit/endpoint"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/domains/services"
	"github.com/gsoultan/gobpm/server/endpoints/collaboration"
	"github.com/gsoultan/gobpm/server/endpoints/connector"
	"github.com/gsoultan/gobpm/server/endpoints/decision"
	"github.com/gsoultan/gobpm/server/endpoints/definition"
	"github.com/gsoultan/gobpm/server/endpoints/external_task"
	"github.com/gsoultan/gobpm/server/endpoints/group"
	"github.com/gsoultan/gobpm/server/endpoints/incident"
	"github.com/gsoultan/gobpm/server/endpoints/notification"
	"github.com/gsoultan/gobpm/server/endpoints/organization"
	"github.com/gsoultan/gobpm/server/endpoints/process"
	"github.com/gsoultan/gobpm/server/endpoints/project"
	"github.com/gsoultan/gobpm/server/endpoints/setup"
	"github.com/gsoultan/gobpm/server/endpoints/task"
	"github.com/gsoultan/gobpm/server/endpoints/user"
	"github.com/gsoultan/gobpm/server/endpoints/webhook"
	"github.com/gsoultan/gobpm/server/interceptors"
)

type Endpoints struct {
	Collaboration collaboration.Endpoints
	Connector     connector.Endpoints
	Decision      decision.Endpoints
	Webhook       webhook.Endpoints
	Definition    definition.Endpoints
	ExternalTask  external_task.Endpoints
	Incident      incident.Endpoints
	Organization  organization.Endpoints
	Process       process.Endpoints
	Project       project.Endpoints
	Setup         setup.Endpoints
	Task          task.Endpoints
	User          user.Endpoints
	Group         group.Endpoints
	Notification  notification.Endpoints
}

// Failer is an interface that should be implemented by response types that can fail.
type Failer interface {
	Failed() error
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	f := interceptors.NewInterceptorFactory(s)
	// protected proves only that the caller is signed in. The three chains
	// below additionally prove *who* they are.
	//
	// Everything not listed here stays on `protected` deliberately: task
	// inbox actions, reading instances and starting a process are the daily
	// work of an ordinary participant, and requiring a role for them would
	// break the primary flow rather than secure it. The role gates sit on the
	// endpoints where a compromised ordinary account would otherwise be able
	// to escalate — managing identities and tenancy, authoring code the engine
	// executes, and changing the fate of running work.
	protected := f.ProtectedChain
	adminOnly := func(method string) func(endpoint.Endpoint) endpoint.Endpoint {
		return f.ProtectedChainWithRoles(method, entities.RoleAdmin)
	}
	designer := func(method string) func(endpoint.Endpoint) endpoint.Endpoint {
		return f.ProtectedChainWithRoles(method, entities.RoleAdmin, entities.RoleDesigner)
	}
	operator := func(method string) func(endpoint.Endpoint) endpoint.Endpoint {
		return f.ProtectedChainWithRoles(method, entities.RoleAdmin, entities.RoleOperator)
	}
	public := f.PublicChain

	collaborationEndpoints := collaboration.MakeEndpoints(s)
	collaborationEndpoints.BroadcastCollaboration = protected("BroadcastCollaboration")(collaborationEndpoints.BroadcastCollaboration)

	connectorEndpoints := connector.MakeEndpoints(s)
	connectorEndpoints.ListConnectors = protected("ListConnectors")(connectorEndpoints.ListConnectors)
	connectorEndpoints.ListConnectorInstances = protected("ListConnectorInstances")(connectorEndpoints.ListConnectorInstances)
	connectorEndpoints.CreateConnectorInstance = adminOnly("CreateConnectorInstance")(connectorEndpoints.CreateConnectorInstance)
	connectorEndpoints.UpdateConnectorInstance = adminOnly("UpdateConnectorInstance")(connectorEndpoints.UpdateConnectorInstance)
	connectorEndpoints.DeleteConnectorInstance = adminOnly("DeleteConnectorInstance")(connectorEndpoints.DeleteConnectorInstance)
	connectorEndpoints.ExecuteConnector = protected("ExecuteConnector")(connectorEndpoints.ExecuteConnector)

	decisionEndpoints := decision.MakeEndpoints(s)
	decisionEndpoints.ListDecisions = protected("ListDecisions")(decisionEndpoints.ListDecisions)
	decisionEndpoints.GetDecision = protected("GetDecision")(decisionEndpoints.GetDecision)
	decisionEndpoints.CreateDecision = designer("CreateDecision")(decisionEndpoints.CreateDecision)
	decisionEndpoints.DeleteDecision = designer("DeleteDecision")(decisionEndpoints.DeleteDecision)
	decisionEndpoints.EvaluateDecision = protected("EvaluateDecision")(decisionEndpoints.EvaluateDecision)
	decisionEndpoints.DecisionImpact = protected("DecisionImpact")(decisionEndpoints.DecisionImpact)
	decisionEndpoints.RunTests = protected("RunDecisionTests")(decisionEndpoints.RunTests)

	// Registering a webhook creates a public address into this installation, so
	// it takes the same authority as changing a process: designer, not viewer.
	webhookEndpoints := webhook.MakeEndpoints(s)
	webhookEndpoints.ListWebhooks = protected("ListWebhooks")(webhookEndpoints.ListWebhooks)
	webhookEndpoints.CreateWebhook = designer("CreateWebhook")(webhookEndpoints.CreateWebhook)
	webhookEndpoints.SetWebhookEnabled = designer("SetWebhookEnabled")(webhookEndpoints.SetWebhookEnabled)
	webhookEndpoints.DeleteWebhook = designer("DeleteWebhook")(webhookEndpoints.DeleteWebhook)

	definitionEndpoints := definition.MakeEndpoints(s)
	definitionEndpoints.ListDefinitions = protected("ListDefinitions")(definitionEndpoints.ListDefinitions)
	definitionEndpoints.CreateDefinition = designer("CreateDefinition")(definitionEndpoints.CreateDefinition)
	definitionEndpoints.GetDefinition = protected("GetDefinition")(definitionEndpoints.GetDefinition)
	definitionEndpoints.DeleteDefinition = designer("DeleteDefinition")(definitionEndpoints.DeleteDefinition)
	definitionEndpoints.ExportDefinition = protected("ExportDefinition")(definitionEndpoints.ExportDefinition)
	definitionEndpoints.ImportDefinition = designer("ImportDefinition")(definitionEndpoints.ImportDefinition)

	externalTaskEndpoints := external_task.MakeEndpoints(s)
	externalTaskEndpoints.FetchAndLockExternal = protected("FetchAndLockExternal")(externalTaskEndpoints.FetchAndLockExternal)
	externalTaskEndpoints.CompleteExternal = protected("CompleteExternal")(externalTaskEndpoints.CompleteExternal)
	externalTaskEndpoints.HandleExternalFailure = protected("HandleExternalFailure")(externalTaskEndpoints.HandleExternalFailure)

	incidentEndpoints := incident.MakeEndpoints(s)
	incidentEndpoints.ListIncidents = protected("ListIncidents")(incidentEndpoints.ListIncidents)
	incidentEndpoints.ResolveIncident = operator("ResolveIncident")(incidentEndpoints.ResolveIncident)

	organizationEndpoints := organization.MakeEndpoints(s)
	organizationEndpoints.CreateOrganization = public("CreateOrganization")(organizationEndpoints.CreateOrganization)
	organizationEndpoints.GetOrganization = protected("GetOrganization")(organizationEndpoints.GetOrganization)
	organizationEndpoints.ListOrganizations = protected("ListOrganizations")(organizationEndpoints.ListOrganizations)
	organizationEndpoints.UpdateOrganization = adminOnly("UpdateOrganization")(organizationEndpoints.UpdateOrganization)
	organizationEndpoints.DeleteOrganization = adminOnly("DeleteOrganization")(organizationEndpoints.DeleteOrganization)

	processEndpoints := process.MakeEndpoints(s)
	processEndpoints.StartProcess = protected("StartProcess")(processEndpoints.StartProcess)
	processEndpoints.GetInstance = protected("GetInstance")(processEndpoints.GetInstance)
	processEndpoints.ListInstances = protected("ListInstances")(processEndpoints.ListInstances)
	processEndpoints.GetExecutionPath = protected("GetExecutionPath")(processEndpoints.GetExecutionPath)
	processEndpoints.GetAuditLogs = protected("GetAuditLogs")(processEndpoints.GetAuditLogs)
	processEndpoints.GetProcessStatistics = protected("GetProcessStatistics")(processEndpoints.GetProcessStatistics)
	processEndpoints.ActivateAdHocTask = operator("ActivateAdHocTask")(processEndpoints.ActivateAdHocTask)
	processEndpoints.BroadcastSignal = operator("BroadcastSignal")(processEndpoints.BroadcastSignal)
	processEndpoints.SendMessage = protected("SendMessage")(processEndpoints.SendMessage)
	processEndpoints.ExecuteScript = designer("ExecuteScript")(processEndpoints.ExecuteScript)
	processEndpoints.ListSubProcesses = protected("ListSubProcesses")(processEndpoints.ListSubProcesses)

	projectEndpoints := project.MakeEndpoints(s)
	projectEndpoints.CreateProject = adminOnly("CreateProject")(projectEndpoints.CreateProject)
	projectEndpoints.GetProject = protected("GetProject")(projectEndpoints.GetProject)
	projectEndpoints.ListProjects = protected("ListProjects")(projectEndpoints.ListProjects)
	projectEndpoints.UpdateProject = adminOnly("UpdateProject")(projectEndpoints.UpdateProject)
	projectEndpoints.DeleteProject = adminOnly("DeleteProject")(projectEndpoints.DeleteProject)

	setupEndpoints := setup.MakeEndpoints(s)
	setupEndpoints.GetSetupStatusEndpoint = public("GetSetupStatus")(setupEndpoints.GetSetupStatusEndpoint)
	setupEndpoints.SetupEndpoint = public("Setup")(setupEndpoints.SetupEndpoint)
	setupEndpoints.TestConnectionEndpoint = public("TestConnection")(setupEndpoints.TestConnectionEndpoint)

	taskEndpoints := task.MakeEndpoints(s)
	taskEndpoints.GetTask = protected("GetTask")(taskEndpoints.GetTask)
	taskEndpoints.ListTasks = protected("ListTasks")(taskEndpoints.ListTasks)
	taskEndpoints.ListTasksByAssignee = protected("ListTasksByAssignee")(taskEndpoints.ListTasksByAssignee)
	taskEndpoints.ListTasksByCandidates = protected("ListTasksByCandidates")(taskEndpoints.ListTasksByCandidates)
	taskEndpoints.ClaimTask = protected("ClaimTask")(taskEndpoints.ClaimTask)
	taskEndpoints.UnclaimTask = protected("UnclaimTask")(taskEndpoints.UnclaimTask)
	taskEndpoints.DelegateTask = protected("DelegateTask")(taskEndpoints.DelegateTask)
	taskEndpoints.CompleteTask = protected("CompleteTask")(taskEndpoints.CompleteTask)
	taskEndpoints.UpdateTask = protected("UpdateTask")(taskEndpoints.UpdateTask)
	taskEndpoints.AssignTask = protected("AssignTask")(taskEndpoints.AssignTask)

	userEndpoints := user.MakeEndpoints(s)
	userEndpoints.GetUser = protected("GetUser")(userEndpoints.GetUser)
	// Creating a user is at least as privileged as updating one, and both of
	// those are adminOnly. This was public: the endpoint chain applied logging
	// and nothing else, so any authenticated caller — at any privilege level —
	// could post a user carrying roles:["admin"] and an organization of their
	// choosing, then log in as an administrator of someone else's tenant.
	//
	// The initial administrator is seeded by setup directly, not through this
	// endpoint, so nothing legitimate depended on it being open.
	userEndpoints.CreateUser = adminOnly("CreateUser")(userEndpoints.CreateUser)
	userEndpoints.UpdateUser = adminOnly("UpdateUser")(userEndpoints.UpdateUser)
	userEndpoints.DeleteUser = adminOnly("DeleteUser")(userEndpoints.DeleteUser)
	userEndpoints.Login = public("Login")(userEndpoints.Login)
	userEndpoints.ListUsers = protected("ListUsers")(userEndpoints.ListUsers)

	groupEndpoints := group.MakeEndpoints(s)
	groupEndpoints.ListGroups = protected("ListGroups")(groupEndpoints.ListGroups)
	groupEndpoints.CreateGroup = adminOnly("CreateGroup")(groupEndpoints.CreateGroup)
	groupEndpoints.GetGroup = protected("GetGroup")(groupEndpoints.GetGroup)
	groupEndpoints.UpdateGroup = adminOnly("UpdateGroup")(groupEndpoints.UpdateGroup)
	groupEndpoints.DeleteGroup = adminOnly("DeleteGroup")(groupEndpoints.DeleteGroup)
	groupEndpoints.ListGroupMembers = protected("ListGroupMembers")(groupEndpoints.ListGroupMembers)
	groupEndpoints.AddMembership = adminOnly("AddMembership")(groupEndpoints.AddMembership)
	groupEndpoints.RemoveMembership = adminOnly("RemoveMembership")(groupEndpoints.RemoveMembership)
	groupEndpoints.ListUserGroups = protected("ListUserGroups")(groupEndpoints.ListUserGroups)

	notificationEndpoints := notification.MakeEndpoints(s)
	notificationEndpoints.ListNotifications = protected("ListNotifications")(notificationEndpoints.ListNotifications)
	notificationEndpoints.MarkAsRead = protected("MarkAsRead")(notificationEndpoints.MarkAsRead)
	notificationEndpoints.MarkAllAsRead = protected("MarkAllAsRead")(notificationEndpoints.MarkAllAsRead)
	notificationEndpoints.DeleteNotification = protected("DeleteNotification")(notificationEndpoints.DeleteNotification)

	return Endpoints{
		Collaboration: collaborationEndpoints,
		Connector:     connectorEndpoints,
		Decision:      decisionEndpoints,
		Webhook:       webhookEndpoints,
		Definition:    definitionEndpoints,
		ExternalTask:  externalTaskEndpoints,
		Incident:      incidentEndpoints,
		Organization:  organizationEndpoints,
		Process:       processEndpoints,
		Project:       projectEndpoints,
		Setup:         setupEndpoints,
		Task:          taskEndpoints,
		User:          userEndpoints,
		Group:         groupEndpoints,
		Notification:  notificationEndpoints,
	}
}
