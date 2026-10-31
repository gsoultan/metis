package connector_test

import (
	"errors"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// A model promoted to another environment names its connection by the name
// an administrator gave it there, because ids differ in every environment.
// The name finds exactly one connection, or the step is refused: never a
// guess between two.
func TestAConnectionIsFoundByItsNameAndOnlyWhenTheNameIsUnique(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestConn(t))
	svc := serviceimpl.NewConnectorService(repo)
	if err := svc.EnsureDefaultConnectors(t.Context()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	catalogue, _ := svc.ListConnectors(t.Context())
	ctx, _, projectID := testutils.ScopedProject(t, repo)
	save := func(name string) entities.ConnectorInstance {
		t.Helper()
		got, err := svc.CreateConnectorInstance(ctx, entities.ConnectorInstance{
			Name: name, Project: &entities.Project{ID: projectID},
			Connector: &entities.Connector{ID: catalogue[0].ID},
			Config:    map[string]any{"webhook_url": "https://example.invalid/" + name},
		})
		if err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
		return got
	}
	payments := save("payments-api")
	save("ops-channel")

	got, err := svc.GetConnectorInstanceByName(ctx, projectID, " payments-api ")
	if err != nil {
		t.Fatalf("find payments-api: %v", err)
	}
	if got.ID != payments.ID {
		t.Fatalf("found %s, want the connection named payments-api (%s)", got.ID, payments.ID)
	}

	if _, err := svc.GetConnectorInstanceByName(ctx, projectID, "billing"); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("a name with no connection got %v, want not found", err)
	}

	save("ops-channel")
	if _, err := svc.GetConnectorInstanceByName(ctx, projectID, "ops-channel"); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("a name two connections share got %v, want a refusal", err)
	}
}
