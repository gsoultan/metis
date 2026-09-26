package connector_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A connector template (`POST /api/v1/connectors`) is installation-wide like a
// manifest: it has no organization, its key is unique across the installation,
// and every organization's connections are configured through its schema — the
// schema is what says which of a connection's settings is a password. The
// administrator of any one organization could rewrite or remove the templates
// every other organization's connections use.

// templateChange is one way of changing the templates every organization uses.
type templateChange struct {
	name string
	as   func(token string) (int, string)
}

func (h *platformHarness) templateChanges(t *testing.T, target entities.Connector) []templateChange {
	renamed := target
	renamed.Name = "Renamed by one organization"
	return []templateChange{
		{"add a connector template", func(token string) (int, string) {
			return h.post(t, token, "/api/v1/connectors", map[string]any{"connector": entities.Connector{
				Key: "crm.template-" + uuid.NewString(), Name: "Another CRM", Type: "utility",
			}})
		}},
		{"change a connector template", func(token string) (int, string) {
			return h.put(t, token, "/api/v1/connectors/"+target.ID.String(), map[string]any{"connector": renamed})
		}},
		{"remove a connector template", func(token string) (int, string) {
			return h.remove(t, token, "/api/v1/connectors/"+target.ID.String())
		}},
	}
}

// sharedTemplate is the template the installation already has when a test
// starts; the change and the removal act on it.
func (h *platformHarness) sharedTemplate(t *testing.T) entities.Connector {
	t.Helper()
	created, err := h.svc.CreateConnector(entities.WithSystemContext(t.Context()), entities.Connector{
		Key:  "crm.shared-template",
		Name: "Shared CRM",
		Type: "utility",
		Schema: []entities.ConnectorProperty{
			{Key: "api_key", Label: "API key", Type: "password", Required: true},
		},
	})
	if err != nil {
		t.Fatalf("create the shared template: %v", err)
	}
	return created
}

func (h *platformHarness) put(t *testing.T, token, path string, body any) (int, string) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, h.server.URL+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(raw)
}

func TestOnAnInstallationOfSeveralOrganizationsOnlyAPlatformAdministratorChangesItsConnectorTemplates(t *testing.T) {
	platformAdministrator := uuid.Must(uuid.NewV7())
	t.Setenv("METIS_PLATFORM_ADMINS", platformAdministrator.String())
	h := newPlatformHarness(t, "Acme", "Globex")
	organizationAdministrator := h.administrator(t, uuid.Must(uuid.NewV7()), "globex-admin", "Globex")
	operatorsChoice := h.administrator(t, platformAdministrator, "platform-admin", "Acme")
	shared := h.sharedTemplate(t)
	changes := h.templateChanges(t, shared)

	for _, change := range changes {
		status, body := change.as(organizationAdministrator)
		if status != http.StatusForbidden {
			t.Errorf("an administrator of one organization could %s every organization uses: status %d (%s)",
				change.name, status, body)
			continue
		}
		if !strings.Contains(body, "METIS_PLATFORM_ADMINS") {
			t.Errorf("refused to %s without saying who can grant it: %s", change.name, body)
		}
	}

	templates, err := h.svc.ListConnectors(entities.WithSystemContext(t.Context()))
	if err != nil {
		t.Fatalf("list the templates: %v", err)
	}
	var kept *entities.Connector
	for i := range templates {
		if templates[i].ID == shared.ID {
			kept = &templates[i]
		}
		if strings.HasPrefix(templates[i].Key, "crm.template-") {
			t.Errorf("a refused request still added the template %q", templates[i].Key)
		}
	}
	if kept == nil || kept.Name != shared.Name {
		t.Fatalf("after the refusals the shared template is %+v, want it unchanged", kept)
	}

	// The administrator the operator named can do every one of them.
	for _, change := range changes {
		if status, body := change.as(operatorsChoice); status != http.StatusOK {
			t.Errorf("the platform administrator could not %s: status %d (%s)", change.name, status, body)
		}
	}
}

func TestOnAnInstallationOfOneOrganizationItsAdministratorsStillChangeItsConnectorTemplates(t *testing.T) {
	t.Setenv("METIS_PLATFORM_ADMINS", "")
	h := newPlatformHarness(t, "Acme")
	administrator := h.administrator(t, uuid.Must(uuid.NewV7()), "acme-admin", "Acme")
	shared := h.sharedTemplate(t)

	for _, change := range h.templateChanges(t, shared) {
		if status, body := change.as(administrator); status != http.StatusOK {
			t.Errorf("the administrator of the only organization could not %s: status %d (%s)", change.name, status, body)
		}
	}
}
