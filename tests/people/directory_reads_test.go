package people_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// TestTheDesignerReadsTheProjectsPeopleGroups is what the step's picker
// lists for "anyone in these teams": the project's People groups, and only to
// somebody the project is visible to.
func TestTheDesignerReadsTheProjectsPeopleGroups(t *testing.T) {
	w := newWorld(t)
	w.seedGroup(t, "approvers", w.personID(t, "ada"))
	w.seedGroup(t, "finance")

	code, body := w.do(t, http.MethodGet, "/api/v1/participant-groups?project_id="+w.project.String(), w.userToken, nil)
	if code != http.StatusOK {
		t.Fatalf("list the groups: %d %s", code, body)
	}
	var out struct {
		Groups []entities.WorkflowGroup `json:"groups"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Groups) != 2 || out.Groups[0].Name != "approvers" || out.Groups[1].Name != "finance" {
		t.Fatalf("groups = %s, want approvers then finance", body)
	}

	// A User of another organization sees none of them.
	system := entities.WithSystemContext(t.Context())
	otherOrg := uuid.Must(uuid.NewV7())
	if err := w.repo.Organization().Create(system, models.OrganizationModel{
		Base: models.Base{ID: models.UUID(otherOrg)}, Name: "Other Org",
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.CreateUser(system, entities.User{
		ID: uuid.Must(uuid.NewV7()), Username: "eve", Roles: []string{entities.RoleUser},
		Organizations: []*entities.Organization{{ID: otherOrg}},
	}, userPassword); err != nil {
		t.Fatal(err)
	}
	_, eve, err := w.svc.Login(t.Context(), "eve", userPassword)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/participant-groups", "/api/v1/participants"} {
		code, body := w.do(t, http.MethodGet, path+"?project_id="+w.project.String(), eve, nil)
		if code == http.StatusOK && (strings.Contains(body, "approvers") || strings.Contains(body, "ada")) {
			t.Errorf("a User of another organization read %s of this project: %s", path, body)
		}
	}
	// And a People token reaches neither.
	if code, _ := w.do(t, http.MethodGet, "/api/v1/participant-groups?project_id="+w.project.String(), w.signIn(t), nil); code != http.StatusUnauthorized {
		t.Errorf("a People token on the groups read = %d, want 401", code)
	}
}
