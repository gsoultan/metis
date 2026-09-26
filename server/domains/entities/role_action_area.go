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
	{"ParticipantSource", areaDirectories},
	{"Participant", areaPeople},
	{"Connector", areaConnectors},
	{"Environment", areaEnvironments},
	{"Definition", areaProcesses},
	{"Script", areaProcesses},
	{"Simulate", areaProcesses},
	{"Decision", areaDecisions},
	{"Webhook", areaWebhooks},
	{"LegacySignatures", areaWebhooks},
	{"Incident", areaInstances},
	{"Instance", areaInstances},
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
