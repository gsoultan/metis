package entities_test

import (
	"slices"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A gated method is named for the code: "SetConnectorManifestEnabled". The
// legend shows it to whoever asks what a role allows, so it has to read as
// words, and it has to be placed somewhere a person would look for it.
func TestAGatedMethodReadsAsWords(t *testing.T) {
	cases := []struct {
		method string
		label  string
		area   string
	}{
		{"CreateDefinition", "Create definition", "processes"},
		{"CancelScheduledDefinition", "Cancel scheduled definition", "processes"},
		{"ListScriptTasks", "List script tasks", "processes"},
		{"SimulateBatch", "Simulate batch", "processes"},
		{"UpdateDecision", "Update decision", "decisions"},
		{"CreateConnectorInstance", "Create connector instance", "connectors"},
		{"SetConnectorManifestEnabled", "Set connector manifest enabled", "connectors"},
		{"SetWebhookEnabled", "Set webhook enabled", "webhooks"},
		{"ActivateAdHocTask", "Activate ad hoc task", "instances"},
		{"MigrateInstances", "Migrate instances", "instances"},
		{"ResolveIncident", "Resolve incident", "instances"},
		{"ImportParticipants", "Import participants", "people"},
		// The longer noun first: a directory is a participant *source*.
		{"SyncParticipantSource", "Sync participant source", "directories"},
		{"CreateProject", "Create project", "projects"},
		{"DeleteOrganization", "Delete organization", "organizations"},
		{"AddMembership", "Add membership", "groups"},
		{"UpdateUser", "Update user", "accounts"},
		// And here: a platform account is not an organization's account.
		{"ListPlatformUsers", "List platform users", "platform"},
		{"SetPlatformRoles", "Set platform roles", "platform"},
		{"TestEnvironmentConnection", "Test environment connection", "environments"},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			action := entities.NewRoleAction(tc.method)
			if action.Method != tc.method {
				t.Errorf("method: got %q, want %q", action.Method, tc.method)
			}
			if action.Label != tc.label {
				t.Errorf("label: got %q, want %q", action.Label, tc.label)
			}
			if action.Area != tc.area {
				t.Errorf("area: got %q, want %q", action.Area, tc.area)
			}
		})
	}
}

// A run of capitals is an abbreviation and stays one.
func TestAnAbbreviationInAMethodStaysOne(t *testing.T) {
	if label := entities.NewRoleAction("ExportOCEL").Label; label != "Export OCEL" {
		t.Fatalf("got %q, want %q", label, "Export OCEL")
	}
}

// A name no noun places has no area rather than a wrong one, so the wiring
// test can say which gate the legend could not place.
func TestAMethodNothingPlacesHasNoArea(t *testing.T) {
	if area := entities.NewRoleAction("Frobnicate").Area; area != "" {
		t.Fatalf("an unplaceable method was put in %q", area)
	}
}

func TestEveryAreaTheLegendPlacesIsListedOnce(t *testing.T) {
	areas := entities.RoleActionAreas()
	for _, method := range []string{"CreateDefinition", "UpdateDecision", "TryConnectorStep", "DeleteWebhook",
		"BroadcastSignal", "RemoveParticipant", "SaveParticipantSource", "UpdateProject", "CreateOrganization",
		"DeleteGroup", "CreateUser", "SavePlatformUser", "SaveEnvironment"} {
		if area := entities.NewRoleAction(method).Area; !slices.Contains(areas, area) {
			t.Errorf("%s is placed in %q, which RoleActionAreas does not list", method, area)
		}
	}
	sorted := slices.Clone(areas)
	slices.Sort(sorted)
	if len(slices.Compact(sorted)) != len(areas) {
		t.Fatalf("RoleActionAreas lists an area twice: %v", areas)
	}
}

// The legend groups by area in the order RoleActionAreas gives, then by label,
// so the answer reads the same on every start.
func TestRoleActionsSortByAreaThenLabel(t *testing.T) {
	actions := []entities.RoleAction{
		entities.NewRoleAction("DeleteGroup"),
		entities.NewRoleAction("UpdateDecision"),
		entities.NewRoleAction("CreateGroup"),
		entities.NewRoleAction("Frobnicate"),
		entities.NewRoleAction("CreateDefinition"),
	}
	slices.SortFunc(actions, entities.CompareRoleActions)

	var got []string
	for _, action := range actions {
		got = append(got, action.Method)
	}
	want := []string{"CreateDefinition", "UpdateDecision", "CreateGroup", "DeleteGroup", "Frobnicate"}
	if !slices.Equal(got, want) {
		t.Fatalf("sorted as %v, want %v", got, want)
	}
}
