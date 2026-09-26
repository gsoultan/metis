package connector_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/configsecret"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/endpoints/connector"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// A connector's configuration holds whatever it needs to authenticate. The API
// used to return that map verbatim, so opening the Connectors page put every
// third-party credential an organization had into the response and the
// browser's developer tools. These assert that it does not any more, and — the
// half that is easy to get wrong — that editing an unrelated field does not
// overwrite the password with the placeholder the browser was shown.

type secretFixture struct {
	svc        services.ServiceFacade
	eps        connector.Endpoints
	instanceID uuid.UUID
	projectID  uuid.UUID
	// The tenant this fixture acts inside; every call below carries it.
	ctx context.Context
}

func newSecretFixture(t *testing.T) *secretFixture {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	svc := services.NewServiceFacade(repo, nil, nil, "connector-secret-test", nil, nil, nil)
	eps := connector.MakeEndpoints(svc)

	ctx := context.Background()
	org, err := svc.CreateOrganization(ctx, "Secret Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	// From here this test stands in for a request from inside that
	// organization. It carried no identity at all, which only worked while
	// the repository scope failed open.
	ctx = entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()})
	project, err := svc.CreateProject(ctx, org.ID, "Secret Project", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := svc.EnsureDefaultConnectors(ctx); err != nil {
		t.Fatalf("seed connectors: %v", err)
	}
	catalogue, err := svc.ListConnectors(ctx)
	if err != nil || len(catalogue) == 0 {
		t.Fatalf("no connectors to configure: %v", err)
	}

	created, err := svc.CreateConnectorInstance(ctx, entities.ConnectorInstance{
		Name:      "Partner API",
		Project:   &entities.Project{ID: project.ID},
		Connector: &entities.Connector{ID: catalogue[0].ID},
		Config: map[string]any{
			"url":        "https://partner.example.com/hook",
			"api_key":    "sk-live-do-not-leak",
			"auth_token": "bearer-do-not-leak",
		},
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	return &secretFixture{
		ctx: ctx, svc: svc, eps: eps, instanceID: created.ID, projectID: project.ID}
}

func (f *secretFixture) list(t *testing.T) []entities.ConnectorInstance {
	t.Helper()
	out, err := f.eps.ListConnectorInstances(f.ctx,
		connector.ListConnectorInstancesRequest{ProjectID: f.projectID.String()})
	if err != nil {
		t.Fatalf("list endpoint: %v", err)
	}
	response, ok := out.(connector.ListConnectorInstancesResponse)
	if !ok {
		t.Fatalf("unexpected response %T", out)
	}
	if response.Err != nil {
		t.Fatalf("list refused: %v", response.Err)
	}
	return response.Instances
}

func TestStoredCredentialsNeverReachTheAPI(t *testing.T) {
	f := newSecretFixture(t)

	instances := f.list(t)
	if len(instances) != 1 {
		t.Fatalf("got %d instances, want 1", len(instances))
	}
	config := instances[0].Config

	for _, key := range []string{"api_key", "auth_token"} {
		if config[key] != configsecret.Sentinel {
			t.Errorf("%s was returned as %v, want the placeholder", key, config[key])
		}
	}
	// The settings somebody is on the page to read must still be readable.
	if config["url"] != "https://partner.example.com/hook" {
		t.Errorf("url was masked or altered: %v", config["url"])
	}
}

/*
 * The half that is easy to get wrong.
 *
 * The form was shown placeholders, and it sends the whole configuration back.
 * Saving that verbatim would replace a live credential with the literal string
 * "__unchanged__" — the connector would then fail to authenticate, and the real
 * secret would be gone with no way to recover it.
 */
func TestEditingAnotherFieldKeepsTheStoredCredential(t *testing.T) {
	f := newSecretFixture(t)
	shown := f.list(t)[0]

	shown.Config["url"] = "https://partner.example.com/changed"
	out, err := f.eps.UpdateConnectorInstance(f.ctx,
		connector.UpdateConnectorInstanceRequest{Instance: shown})
	if err != nil {
		t.Fatalf("update endpoint: %v", err)
	}
	if response, ok := out.(connector.UpdateConnectorInstanceResponse); ok && response.Err != nil {
		t.Fatalf("update refused: %v", response.Err)
	}

	// Read past the API, the way the executor does, to see what is really held.
	stored, err := f.svc.GetConnectorInstance(f.ctx, f.instanceID)
	if err != nil {
		t.Fatalf("read stored instance: %v", err)
	}
	if stored.Config["api_key"] != "sk-live-do-not-leak" {
		t.Fatalf("the stored api_key became %v", stored.Config["api_key"])
	}
	if stored.Config["url"] != "https://partner.example.com/changed" {
		t.Fatalf("the edited url was not saved: %v", stored.Config["url"])
	}
}

func TestATypedReplacementIsSaved(t *testing.T) {
	f := newSecretFixture(t)
	shown := f.list(t)[0]

	shown.Config["api_key"] = "sk-live-rotated"
	if _, err := f.eps.UpdateConnectorInstance(f.ctx,
		connector.UpdateConnectorInstanceRequest{Instance: shown}); err != nil {
		t.Fatalf("update endpoint: %v", err)
	}

	stored, err := f.svc.GetConnectorInstance(f.ctx, f.instanceID)
	if err != nil {
		t.Fatalf("read stored instance: %v", err)
	}
	if stored.Config["api_key"] != "sk-live-rotated" {
		t.Fatalf("the rotated key was not saved: %v", stored.Config["api_key"])
	}
}

// The column is encrypted, so a database backup or a read replica does not hand
// over every credential in the installation.
func TestTheStoredConfigIsNotReadableAsPlainText(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	svc := serviceimpl.NewConnectorService(repo)
	// A real project, and a context that names its organization: an instance
	// hangs off a project and a scoped read joins through it.
	ctx, _, projectID := testutils.ScopedProject(t, repo)

	if err := svc.EnsureDefaultConnectors(ctx); err != nil {
		t.Fatalf("seed connectors: %v", err)
	}
	catalogue, err := svc.ListConnectors(ctx)
	if err != nil || len(catalogue) == 0 {
		t.Fatalf("no connectors: %v", err)
	}
	if _, err := svc.CreateConnectorInstance(ctx, entities.ConnectorInstance{
		Name:      "Partner API",
		Project:   &entities.Project{ID: projectID},
		Connector: &entities.Connector{ID: catalogue[0].ID},
		Config:    map[string]any{"api_key": "sk-live-do-not-leak"},
	}); err != nil {
		t.Fatalf("create instance: %v", err)
	}

	var raw string
	if err := db.Raw("SELECT config FROM connector_instances LIMIT 1").Scan(&raw).Error; err != nil {
		t.Fatalf("read the raw column: %v", err)
	}
	if strings.Contains(raw, "sk-live-do-not-leak") {
		t.Fatalf("the credential is stored in clear text: %s", raw)
	}
	if raw == "" {
		t.Fatal("nothing was stored at all")
	}
}
