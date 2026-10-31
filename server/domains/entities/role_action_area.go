package entities

import "strings"

// The parts of the product a gated action belongs to, as keys the interface
// translates.
const (
	areaProcesses     = "processes"
	areaDecisions     = "decisions"
	areaConnectors    = "connectors"
	areaWebhooks      = "webhooks"
	areaInstances     = "instances"
	areaPeople        = "people"
	areaDirectories   = "directories"
	areaProjects      = "projects"
	areaOrganizations = "organizations"
	areaGroups        = "groups"
	areaAccounts      = "accounts"
	areaPlatform      = "platform"
	areaEnvironments  = "environments"
)

// RoleActionAreas is every area an action can be placed in, in the order a
// legend lists them: building, then running, then administering — the order
// of the navigation.
func RoleActionAreas() []string {
	return []string{
		areaProcesses, areaDecisions, areaConnectors, areaWebhooks,
		areaInstances, areaPeople, areaDirectories,
		areaProjects, areaOrganizations, areaGroups, areaAccounts, areaPlatform, areaEnvironments,
	}
}

// actionNouns places a method in an area by a noun its name carries. The first
// match wins, so a noun comes before any shorter one inside it: a
// ParticipantSource is a directory, not a person, and a PlatformUser is not an
// organization's account.
//
// A method that carries none of them has no area. The wiring test refuses one,
// naming it, so a new gate is placed rather than listed under nothing.
var actionNouns = []struct{ noun, area string }{
	{"PlatformUser", areaPlatform},
	{"PlatformRole", areaPlatform},
	// The server's settings are every organization's.
	{"ServerSetting", areaPlatform},
	{"ParticipantSource", areaDirectories},
	{"Participant", areaPeople},
	// What a project's People lack before the switch-over, read and filled.
	{"DirectoryGap", areaPeople},
	{"Connector", areaConnectors},
	{"Environment", areaEnvironments},
	// A release moves a project's models from one environment to the next.
	{"Release", areaEnvironments},
	{"Definition", areaProcesses},
	{"Script", areaProcesses},
	{"Simulate", areaProcesses},
	{"SimulationCase", areaProcesses},
	{"Decision", areaDecisions},
	{"Webhook", areaWebhooks},
	{"LegacySignatures", areaWebhooks},
	{"Incident", areaInstances},
	// A request for a second administrator (ApproveDeviationRequest) is about
	// an instance's work, and its name carries no other noun here.
	{"Deviation", areaInstances},
	{"Instance", areaInstances},
	// A batch operation retries or cancels instances.
	{"BatchOperation", areaInstances},
	// The audit log: what people did across the organization.
	{"ListOperations", areaOrganizations},
	// A hold keeps an instance; the project's list of them names no instance.
	{"RetentionHold", areaInstances},
	// A retention period is a process's: how long its finished instances
	// are kept.
	{"RetentionPeriod", areaProcesses},
	{"Signal", areaInstances},
	{"AdHocTask", areaInstances},
	{"Organization", areaOrganizations},
	{"Project", areaProjects},
	{"Membership", areaGroups},
	{"Group", areaGroups},
	{"User", areaAccounts},
}

func actionArea(method string) string {
	for _, entry := range actionNouns {
		if strings.Contains(method, entry.noun) {
			return entry.area
		}
	}
	return ""
}
