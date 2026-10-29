package auth_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
)

// Whoever creates an organization administers it.
//
// It used to be created with nobody in it. Its creator could not reach it, did
// not see it in their list of organizations, and was refused with "that
// organization is not yours" when they tried to add the first member — so the
// only way in was a row written into the database by hand.
func TestTheCreatorOfAnOrganizationAdministersIt(t *testing.T) {
	h := newLocalHarness(t)
	acme := h.organization("Acme")
	admin := entities.User{
		ID:            uuid.Must(uuid.NewV7()),
		Username:      "grace",
		Email:         "grace@acme.example",
		Roles:         []string{entities.RoleAdmin},
		Organizations: []*entities.Organization{{ID: acme}},
	}
	if err := h.svc.CreateUser(inOrganization(t.Context(), acme), admin, localPassword); err != nil {
		t.Fatalf("create the administrator: %v", err)
	}
	token := h.login("grace")

	status, body := h.do(http.MethodPost, "/api/v1/organizations", token, acme.String(),
		map[string]string{"name": "Globex"})
	if status != http.StatusOK {
		t.Fatalf("create Globex: status %d (%s), want 200", status, body)
	}
	var created struct {
		Organization struct {
			ID uuid.UUID `json:"id"`
		} `json:"organization"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil || created.Organization.ID == uuid.Nil {
		t.Fatalf("create Globex returned no organization: %v (%s)", err, body)
	}
	globex := created.Organization.ID

	if members := h.members(globex); !slices.Equal(members, []string{"grace"}) {
		t.Fatalf("Globex's members: %v, want [grace]", members)
	}
	var roles string
	if err := h.db.WithContext(t.Context()).Raw(
		`SELECT roles::text FROM user_organizations WHERE user_id = ? AND organization_id = ?`,
		admin.ID, globex).Scan(&roles).Error; err != nil {
		t.Fatalf("read grace's roles in Globex: %v", err)
	}
	if roles != `["`+entities.RoleAdmin+`"]` {
		t.Fatalf("grace's roles in Globex: %s, want [%q]", roles, entities.RoleAdmin)
	}

	if status, body := h.get("/api/v1/organizations/"+globex.String(), token, globex.String()); status != http.StatusOK {
		t.Fatalf("read Globex as its creator: status %d (%s), want 200", status, body)
	}
	status, body = h.do(http.MethodPost, "/api/v1/users", token, globex.String(), map[string]any{
		"user": map[string]any{
			"username":      "hopper",
			"email":         "hopper@globex.example",
			"organizations": []map[string]string{{"id": globex.String()}},
		},
		"password": localPassword,
	})
	if status != http.StatusOK {
		t.Fatalf("add the first member to Globex: status %d (%s), want 200", status, body)
	}
}
