package connector_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/configsecret"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/endpoints/connector"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// A RabbitMQ connection is configured with one url, and the broker's password
// is inside it: amqp://user:password@host. The API masks a connection's
// settings by the names of their keys, and "url" names nothing secret, so
// listing a project's connections — which any signed-in account may do —
// returned the broker's password in clear.
//
// Root cause: a credential was recognised by where it was stored, never by what
// it was.
func TestABrokerPasswordInsideAURLNeverReachesTheBrowser(t *testing.T) {
	const password = "hunter2-broker"
	stored := "amqp://metis:" + password + "@broker.internal:5672/orders"

	svc, eps, ctx, projectID := urlCredentialFixture(t)
	instance := createInstance(t, svc, ctx, projectID, "rabbitmq-publish", map[string]any{
		"url": stored, "exchange": "orders", "routing_key": "created",
	})

	listed := listInstances(t, eps, ctx, projectID)
	if len(listed) != 1 {
		t.Fatalf("expected the one connection, got %d", len(listed))
	}
	if listed[0].Config["url"] != configsecret.Sentinel {
		t.Fatalf("the broker url came back as %v", listed[0].Config["url"])
	}
	if listed[0].Config["exchange"] != "orders" {
		t.Fatalf("a setting with nothing secret in it was masked: %v", listed[0].Config["exchange"])
	}

	// Saving the form back unchanged sends the placeholder, and must keep the
	// stored url rather than store the placeholder as the broker's address.
	if _, err := eps.UpdateConnectorInstance(ctx, connector.UpdateConnectorInstanceRequest{
		Instance: entities.ConnectorInstance{
			ID: instance.ID, Name: "Orders broker", Project: &entities.Project{ID: projectID},
			Connector: instance.Connector,
			Config:    map[string]any{"url": configsecret.Sentinel, "exchange": "orders", "routing_key": "created"},
		},
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	kept, err := svc.GetConnectorInstance(ctx, instance.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if kept.Config["url"] != stored {
		t.Fatalf("saving the form unchanged replaced the url with %v", kept.Config["url"])
	}
}

func urlCredentialFixture(t *testing.T) (services.ServiceFacade, connector.Endpoints, context.Context, uuid.UUID) {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	svc := services.NewServiceFacade(repo, nil, nil, "url-credential-test", nil, nil, nil)
	org, err := svc.CreateOrganization(context.Background(), "Broker Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	ctx := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: org.ID.String()})
	project, err := svc.CreateProject(ctx, org.ID, "Broker Project", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := svc.EnsureDefaultConnectors(ctx); err != nil {
		t.Fatalf("seed connectors: %v", err)
	}
	return svc, connector.MakeEndpoints(svc), ctx, project.ID
}

func createInstance(t *testing.T, svc services.ServiceFacade, ctx context.Context, projectID uuid.UUID, key string, config map[string]any) entities.ConnectorInstance {
	t.Helper()
	catalogue, err := svc.ListConnectors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range catalogue {
		if c.Key != key {
			continue
		}
		created, err := svc.CreateConnectorInstance(ctx, entities.ConnectorInstance{
			Name: "Orders broker", Project: &entities.Project{ID: projectID},
			Connector: &entities.Connector{ID: c.ID}, Config: config,
		})
		if err != nil {
			t.Fatalf("create instance: %v", err)
		}
		return created
	}
	t.Fatalf("no %s connector in the catalogue", key)
	return entities.ConnectorInstance{}
}

func listInstances(t *testing.T, eps connector.Endpoints, ctx context.Context, projectID uuid.UUID) []entities.ConnectorInstance {
	t.Helper()
	out, err := eps.ListConnectorInstances(ctx, connector.ListConnectorInstancesRequest{ProjectID: projectID.String()})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	response, ok := out.(connector.ListConnectorInstancesResponse)
	if !ok {
		t.Fatalf("list returned %T", out)
	}
	return response.Instances
}
