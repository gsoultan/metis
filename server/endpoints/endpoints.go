package endpoints

import (
	"github.com/go-kit/kit/endpoint"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/endpoints/collaboration"
	"github.com/gsoultan/metis/server/endpoints/connector"
	"github.com/gsoultan/metis/server/endpoints/decision"
	"github.com/gsoultan/metis/server/endpoints/definition"
	"github.com/gsoultan/metis/server/endpoints/environment"
	"github.com/gsoultan/metis/server/endpoints/external_task"
	"github.com/gsoultan/metis/server/endpoints/group"
	"github.com/gsoultan/metis/server/endpoints/incident"
	"github.com/gsoultan/metis/server/endpoints/notification"
	"github.com/gsoultan/metis/server/endpoints/organization"
	"github.com/gsoultan/metis/server/endpoints/participant"
	"github.com/gsoultan/metis/server/endpoints/participantsource"
	"github.com/gsoultan/metis/server/endpoints/platformuser"
	"github.com/gsoultan/metis/server/endpoints/process"
	"github.com/gsoultan/metis/server/endpoints/project"
	"github.com/gsoultan/metis/server/endpoints/role"
	"github.com/gsoultan/metis/server/endpoints/setup"
	"github.com/gsoultan/metis/server/endpoints/simulation"
	"github.com/gsoultan/metis/server/endpoints/task"
	"github.com/gsoultan/metis/server/endpoints/user"
	"github.com/gsoultan/metis/server/endpoints/webhook"
	"github.com/gsoultan/metis/server/interceptors"
)

type Endpoints struct {
	Collaboration     collaboration.Endpoints
	Connector         connector.Endpoints
	Decision          decision.Endpoints
	Webhook           webhook.Endpoints
	Definition        definition.Endpoints
	Environment       environment.Endpoints
	Participant       participant.Endpoints
	ParticipantSource participantsource.Endpoints
	PlatformUser      platformuser.Endpoints
	ExternalTask      external_task.Endpoints
	Incident          incident.Endpoints
	Organization      organization.Endpoints
	Process           process.Endpoints
	Project           project.Endpoints
	Setup             setup.Endpoints
	Task              task.Endpoints
	User              user.Endpoints
	Group             group.Endpoints
	Notification      notification.Endpoints
	Simulation        simulation.Endpoints
	Role              role.Endpoints
}

// Failer is an interface that should be implemented by response types that can fail.
type Failer interface {
	Failed() error
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	f := interceptors.NewInterceptorFactory(s, s)
	// protected proves only that the caller is signed in. The chains below it
	// additionally prove *who* they are.
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
	// platformAdmin is for what every organization on the installation shares.
	// adminOnly admits the administrator of any one organization, because roles
	// are global; this admits, where there is more than one organization, only
	// the administrators the operator named in METIS_PLATFORM_ADMINS.
	platformAdmin := f.PlatformChain
	public := f.PublicChain

	collaborationEndpoints := collaboration.MakeEndpoints(s)
	collaborationEndpoints.BroadcastCollaboration = protected("BroadcastCollaboration")(collaborationEndpoints.BroadcastCollaboration)

	connectorEndpoints := connector.MakeEndpoints(s)
	connectorEndpoints.ListConnectors = protected("ListConnectors")(connectorEndpoints.ListConnectors)
	connectorEndpoints.ListConnectorInstances = protected("ListConnectorInstances")(connectorEndpoints.ListConnectorInstances)
	connectorEndpoints.CreateConnectorInstance = adminOnly("CreateConnectorInstance")(connectorEndpoints.CreateConnectorInstance)
	connectorEndpoints.UpdateConnectorInstance = adminOnly("UpdateConnectorInstance")(connectorEndpoints.UpdateConnectorInstance)
	connectorEndpoints.DeleteConnectorInstance = adminOnly("DeleteConnectorInstance")(connectorEndpoints.DeleteConnectorInstance)
	// Administrators only. It runs a connector with a configuration the caller
	// writes — any host, any port — and the SMTP and AMQP connectors dial it
	// directly, with no egress guard. Behind a login alone, every signed-in
	// account could make the server connect wherever it liked, and read from the
	// error whether something was listening there. The Connectors page's
	// connection test is its only working caller, and that page is theirs; an
	// administrator can already point a saved connection anywhere.
	connectorEndpoints.ExecuteConnector = adminOnly("ExecuteConnector")(connectorEndpoints.ExecuteConnector)
	// Designers write the steps, and the connection a step is tried against is
	// the project's saved one — never the caller's to choose, which is what
	// lets this be theirs where ExecuteConnector cannot be.
	connectorEndpoints.TryConnectorStep = designer("TryConnectorStep")(connectorEndpoints.TryConnectorStep)

	// The connector *templates*, as opposed to the instances above. These three
	// were routed and never wrapped, so any authenticated account could add,
	// rewrite or delete one — while creating an instance of the same connector
	// needed an administrator. A connector describes what the engine calls out
	// to and with which credentials, which is the definition of "authoring code
	// the engine executes". And a template has no organization: its key is
	// unique across the installation, and every organization's connections are
	// configured through its schema, which is what marks a setting as a
	// password. So, like the manifests below, it is the platform's to change.
	connectorEndpoints.CreateConnector = platformAdmin("CreateConnector")(connectorEndpoints.CreateConnector)
	connectorEndpoints.UpdateConnector = platformAdmin("UpdateConnector")(connectorEndpoints.UpdateConnector)
	connectorEndpoints.DeleteConnector = platformAdmin("DeleteConnector")(connectorEndpoints.DeleteConnector)

	// Installing a connector adds an address this engine will call with the
	// tenant's credentials attached — every tenant's: a manifest is
	// installation-wide, and a step in any organization that names its key runs
	// it. So installing, switching and removing one is the platform's decision
	// rather than one organization's administrator's. Reading the catalogue is
	// neither.
	connectorEndpoints.InstallManifest = platformAdmin("InstallConnectorManifest")(connectorEndpoints.InstallManifest)
	connectorEndpoints.SetManifestEnabled = platformAdmin("SetConnectorManifestEnabled")(connectorEndpoints.SetManifestEnabled)
	connectorEndpoints.DeleteManifest = platformAdmin("DeleteConnectorManifest")(connectorEndpoints.DeleteManifest)
	connectorEndpoints.ListManifests = protected("ListConnectorManifests")(connectorEndpoints.ListManifests)
	connectorEndpoints.GetManifest = protected("GetConnectorManifest")(connectorEndpoints.GetManifest)

	decisionEndpoints := decision.MakeEndpoints(s)
	decisionEndpoints.ListDecisions = protected("ListDecisions")(decisionEndpoints.ListDecisions)
	decisionEndpoints.ListSummaries = protected("ListDecisionSummaries")(decisionEndpoints.ListSummaries)
	decisionEndpoints.GetDecision = protected("GetDecision")(decisionEndpoints.GetDecision)
	decisionEndpoints.CreateDecision = designer("CreateDecision")(decisionEndpoints.CreateDecision)
	decisionEndpoints.DeleteDecision = designer("DeleteDecision")(decisionEndpoints.DeleteDecision)
	// Routed and never wrapped, between two neighbours that are gated. Creating
	// a decision needed the designer role and deleting one did too, while
	// rewriting one needed nothing beyond a login — and rewriting is the more
	// powerful of the three. Proven against a running server: the same account
	// was refused a create with 401 and allowed an update with 200, which left
	// the table with zero rules and zero inputs. A DMN table with no rules
	// matches nothing, and a decision point that matches nothing is an incident
	// on every instance that reaches it.
	decisionEndpoints.UpdateDecision = designer("UpdateDecision")(decisionEndpoints.UpdateDecision)
	decisionEndpoints.EvaluateDecision = protected("EvaluateDecision")(decisionEndpoints.EvaluateDecision)
	decisionEndpoints.DecisionImpact = protected("DecisionImpact")(decisionEndpoints.DecisionImpact)
	decisionEndpoints.RunTests = protected("RunDecisionTests")(decisionEndpoints.RunTests)
	decisionEndpoints.ListDecisionVersions = protected("ListDecisionVersions")(decisionEndpoints.ListDecisionVersions)
	// Making a version live changes what every step with no version binding
	// decides from then on — the same bar as saving one, and as promoting a
	// process version.
	decisionEndpoints.PromoteDecision = designer("PromoteDecision")(decisionEndpoints.PromoteDecision)

	// Registering a webhook creates a public address into this installation, so
	// it takes the same authority as changing a process: designer, not viewer.
	webhookEndpoints := webhook.MakeEndpoints(s)
	webhookEndpoints.ListWebhooks = protected("ListWebhooks")(webhookEndpoints.ListWebhooks)
	webhookEndpoints.CreateWebhook = designer("CreateWebhook")(webhookEndpoints.CreateWebhook)
	webhookEndpoints.SetWebhookEnabled = designer("SetWebhookEnabled")(webhookEndpoints.SetWebhookEnabled)
	webhookEndpoints.CloseLegacySignatures = designer("CloseLegacySignatures")(webhookEndpoints.CloseLegacySignatures)
	webhookEndpoints.DeleteWebhook = designer("DeleteWebhook")(webhookEndpoints.DeleteWebhook)

	definitionEndpoints := definition.MakeEndpoints(s)

	// An environment names a database and the credentials to reach it, so every
	// operation on one is administrative — reading the list included, because
	// the list says where each runtime lives.
	environmentEndpoints := environment.MakeEndpoints(s)
	environmentEndpoints.ListEnvironments = adminOnly("ListEnvironments")(environmentEndpoints.ListEnvironments)
	environmentEndpoints.SaveEnvironment = adminOnly("SaveEnvironment")(environmentEndpoints.SaveEnvironment)
	environmentEndpoints.DeleteEnvironment = adminOnly("DeleteEnvironment")(environmentEndpoints.DeleteEnvironment)
	// Takes a host and a port and reports what happened to the attempt, which is
	// a network probe. The setup wizard's public equivalent closes as soon as the
	// installation is configured; this is the one that replaces it.
	environmentEndpoints.TestConnection = adminOnly("TestEnvironmentConnection")(environmentEndpoints.TestConnection)

	// Reading a project's participants is ordinary work — a designer picking an
	// assignee needs it. Importing is not: it takes an endpoint address or a
	// database connection and a query, which are a network probe and arbitrary
	// SQL respectively, so it takes the designer role at least.
	participantEndpoints := participant.MakeEndpoints(s)
	participantEndpoints.ListParticipants = protected("ListParticipants")(participantEndpoints.ListParticipants)
	participantEndpoints.ImportParticipants = designer("ImportParticipants")(participantEndpoints.ImportParticipants)
	// Removing somebody takes the same role importing does — they are the same
	// act in opposite directions, and the removal is reversible by an import.
	participantEndpoints.RemoveParticipant = designer("RemoveParticipant")(participantEndpoints.RemoveParticipant)

	// A directory source holds a connection string or an endpoint token, so
	// every operation on one is administrative — reading the list included,
	// because the list says where each directory lives.
	sourceEndpoints := participantsource.MakeEndpoints(s)
	sourceEndpoints.ListSources = adminOnly("ListParticipantSources")(sourceEndpoints.ListSources)
	sourceEndpoints.SaveSource = adminOnly("SaveParticipantSource")(sourceEndpoints.SaveSource)
	sourceEndpoints.DeleteSource = adminOnly("DeleteParticipantSource")(sourceEndpoints.DeleteSource)
	sourceEndpoints.SyncSource = adminOnly("SyncParticipantSource")(sourceEndpoints.SyncSource)

	// Platform accounts are administrative in full, listing included: the list
	// is who can reconfigure this installation, which is the first thing an
	// attacker with a designer's token would want to read.
	accountEndpoints := platformuser.MakeEndpoints(s)
	accountEndpoints.ListAccounts = adminOnly("ListPlatformUsers")(accountEndpoints.ListAccounts)
	accountEndpoints.SaveAccount = adminOnly("SavePlatformUser")(accountEndpoints.SaveAccount)
	accountEndpoints.DeleteAccount = adminOnly("DeletePlatformUser")(accountEndpoints.DeleteAccount)
	accountEndpoints.SetRoles = adminOnly("SetPlatformRoles")(accountEndpoints.SetRoles)
	definitionEndpoints.ListDefinitions = protected("ListDefinitions")(definitionEndpoints.ListDefinitions)
	definitionEndpoints.CreateDefinition = designer("CreateDefinition")(definitionEndpoints.CreateDefinition)
	definitionEndpoints.GetDefinition = protected("GetDefinition")(definitionEndpoints.GetDefinition)
	definitionEndpoints.DeleteDefinition = designer("DeleteDefinition")(definitionEndpoints.DeleteDefinition)
	definitionEndpoints.ExportDefinition = protected("ExportDefinition")(definitionEndpoints.ExportDefinition)
	definitionEndpoints.ImportDefinition = designer("ImportDefinition")(definitionEndpoints.ImportDefinition)
	definitionEndpoints.ListJavaScriptConditions = protected("ListJavaScriptConditions")(definitionEndpoints.ListJavaScriptConditions)
	// Deliberately stricter than the javascript-conditions worklist beside it.
	// That one reports expressions; this one returns every script *body* in the
	// tenant in a single call, which is the sort of aggregate an ordinary
	// approver has no reason to pull. Admin and designer are the roles that
	// author and manage models, and so the roles that would act on it.
	definitionEndpoints.ListScriptTasks = designer("ListScriptTasks")(definitionEndpoints.ListScriptTasks)
	// Promoting is a write that changes which model every future instance runs,
	// so it takes the designer role rather than the read role — the same bar as
	// deploying the version in the first place.
	definitionEndpoints.PromoteDefinition = designer("PromoteDefinition")(definitionEndpoints.PromoteDefinition)
	definitionEndpoints.ListDefinitionVersions = protected("ListDefinitionVersions")(definitionEndpoints.ListDefinitionVersions)
	definitionEndpoints.ListLiveVersions = protected("ListLiveVersions")(definitionEndpoints.ListLiveVersions)
	// Scheduling and cancelling change which model future instances run, so they
	// take the designer role — the same bar as promoting now.
	definitionEndpoints.ScheduleDefinition = designer("ScheduleDefinition")(definitionEndpoints.ScheduleDefinition)
	definitionEndpoints.CancelScheduledDefinition = designer("CancelScheduledDefinition")(definitionEndpoints.CancelScheduledDefinition)
	// Administrative rather than designer: this rewrites instances that have
	// already been started — somebody's purchase order, somebody's leave
	// request — where every other action on this page only decides what future
	// instances will run.
	definitionEndpoints.MigrateInstances = adminOnly("MigrateInstances")(definitionEndpoints.MigrateInstances)

	externalTaskEndpoints := external_task.MakeEndpoints(s)
	externalTaskEndpoints.FetchAndLockExternal = protected("FetchAndLockExternal")(externalTaskEndpoints.FetchAndLockExternal)
	externalTaskEndpoints.CompleteExternal = protected("CompleteExternal")(externalTaskEndpoints.CompleteExternal)
	externalTaskEndpoints.HandleExternalFailure = protected("HandleExternalFailure")(externalTaskEndpoints.HandleExternalFailure)

	incidentEndpoints := incident.MakeEndpoints(s)
	incidentEndpoints.ListIncidents = protected("ListIncidents")(incidentEndpoints.ListIncidents)
	incidentEndpoints.ResolveIncident = operator("ResolveIncident")(incidentEndpoints.ResolveIncident)

	organizationEndpoints := organization.MakeEndpoints(s)
	// Was public: logging and nothing else, so any signed-in account could
	// create organizations over HTTP, and anybody at all over the gRPC
	// listener, which authenticates nothing. Names are unique, so it doubled as
	// a way to squat on one and to learn which already existed.
	organizationEndpoints.CreateOrganization = adminOnly("CreateOrganization")(organizationEndpoints.CreateOrganization)
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
	processEndpoints.WaitingByStep = protected("WaitingByStep")(processEndpoints.WaitingByStep)
	processEndpoints.Deadlines = protected("Deadlines")(processEndpoints.Deadlines)
	processEndpoints.ActivateAdHocTask = operator("ActivateAdHocTask")(processEndpoints.ActivateAdHocTask)
	processEndpoints.BroadcastSignal = operator("BroadcastSignal")(processEndpoints.BroadcastSignal)
	processEndpoints.SendMessage = protected("SendMessage")(processEndpoints.SendMessage)
	processEndpoints.ExecuteScript = designer("ExecuteScript")(processEndpoints.ExecuteScript)
	processEndpoints.ListSubProcesses = protected("ListSubProcesses")(processEndpoints.ListSubProcesses)
	// ExportOCEL was registered as a route and never wrapped, so nothing resolved
	// a tenant for it. The transport chain still refused anonymous callers, so
	// this was never a disclosure — but the endpoint reached the repository with
	// no identity, and under METIS_FEATURE_STRICT_TENANT_SCOPE that is answered
	// with nothing. The export returned 200 and an empty log: every event and
	// object silently missing, on the endpoint whose whole purpose is to hand a
	// project's history to a mining tool.
	//
	// protected rather than a role-restricted chain, to match GetAuditLogs —
	// this is the same data at project scope rather than instance scope.
	processEndpoints.ExportOCEL = protected("ExportOCEL")(processEndpoints.ExportOCEL)

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
	// Self-service, so protected rather than adminOnly: changing your own
	// password is the one thing here every account must be able to do. It was
	// routed and wrapped by nothing at all, which is a different problem — the
	// transport chain still demanded a token, but no tenant was resolved, so
	// the endpoint reached the repository with no identity.
	userEndpoints.ChangePassword = protected("ChangePassword")(userEndpoints.ChangePassword)
	// Self-service too: the Profile page saved through UpdateUser above, which
	// is adminOnly, so for everybody else it failed. These act only on the
	// account the session names, and change only its names and email.
	userEndpoints.GetOwnProfile = protected("GetOwnProfile")(userEndpoints.GetOwnProfile)
	userEndpoints.UpdateOwnProfile = protected("UpdateOwnProfile")(userEndpoints.UpdateOwnProfile)

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
	// Everybody signed in has a bell. What it counts is the session's own, so
	// being signed in is the whole of the check.
	notificationEndpoints.CountUnreadNotifications = protected("CountUnreadNotifications")(notificationEndpoints.CountUnreadNotifications)
	notificationEndpoints.MarkAsRead = protected("MarkAsRead")(notificationEndpoints.MarkAsRead)
	notificationEndpoints.MarkAllAsRead = protected("MarkAllAsRead")(notificationEndpoints.MarkAllAsRead)
	notificationEndpoints.DeleteNotification = protected("DeleteNotification")(notificationEndpoints.DeleteNotification)

	// Simulation runs an authored definition and reports which gateway fired and
	// which rule matched — the contents of the model, not just its name. That is
	// the designer's own material, and simulating is an authoring activity, so it
	// sits on the same gate as authoring rather than on plain `protected`.
	//
	// It is also the most expensive endpoint here: each run holds a database
	// transaction for its length. Restricting it to the roles that have a reason
	// to call it is the cheap half of not letting it become an amplifier.
	simulationEndpoints := simulation.MakeEndpoints(s)
	simulationEndpoints.Simulate = designer("Simulate")(simulationEndpoints.Simulate)
	simulationEndpoints.SimulateBatch = designer("SimulateBatch")(simulationEndpoints.SimulateBatch)

	// What each role is required for, read from the gates above as the factory
	// built them. Last, so that every one of them is in it: a gate built after
	// this line would be enforced and missing from the legend, which is the
	// drift the legend exists to rule out. role_legend_test.go holds what it
	// serves to this file.
	//
	// protected rather than adminOnly: "what does Designer let me do" is a fair
	// question from anybody signed in, and the answer is the same for everybody
	// — the installation's own gates, with nothing from any organization in it.
	roleEndpoints := role.MakeEndpoints(f.RoleAccess())
	roleEndpoints.ListRoles = protected("ListRoles")(roleEndpoints.ListRoles)

	return Endpoints{
		Collaboration:     collaborationEndpoints,
		Connector:         connectorEndpoints,
		Decision:          decisionEndpoints,
		Webhook:           webhookEndpoints,
		Definition:        definitionEndpoints,
		Environment:       environmentEndpoints,
		Participant:       participantEndpoints,
		ParticipantSource: sourceEndpoints,
		PlatformUser:      accountEndpoints,
		ExternalTask:      externalTaskEndpoints,
		Incident:          incidentEndpoints,
		Organization:      organizationEndpoints,
		Process:           processEndpoints,
		Project:           projectEndpoints,
		Setup:             setupEndpoints,
		Task:              taskEndpoints,
		User:              userEndpoints,
		Group:             groupEndpoints,
		Notification:      notificationEndpoints,
		Simulation:        simulationEndpoints,
		Role:              roleEndpoints,
	}
}
