package connector_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	serviceimpl "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/tests/testutils"
)

// The built-in connector catalogue must exist in the database actually in use.
//
// Seeding ran once, from the service constructor, at startup — which is before
// setup has chosen a database. Setup then swapped the connection to the target
// database, leaving the catalogue behind in the bootstrap one. A configured
// installation therefore opened the connector page to an empty list, with no
// Slack, no email and no HTTP for any service task to call, and nothing
// anywhere saying why.
//
// Seeding is a callable operation for that reason: the swap calls it again.
func TestEnsureDefaultConnectors_PopulatesTheDatabaseInUse(t *testing.T) {
	// A second, empty database standing in for the one setup swaps to.
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewConnectorService(repo)
	ctx := t.Context()

	if err := svc.EnsureDefaultConnectors(ctx); err != nil {
		t.Fatalf("seeding the catalogue: %v", err)
	}

	got, err := svc.ListConnectors(ctx)
	if err != nil {
		t.Fatalf("list connectors: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("the catalogue is empty after seeding")
	}

	// The ones a service task is most likely to reach for.
	for _, key := range []string{"http-json", "email-smtp", "slack-message"} {
		if !hasConnector(got, key) {
			t.Errorf("built-in connector %q is missing from the catalogue", key)
		}
	}
}

// The swap calls this on a database that may already hold the catalogue — for
// instance when an existing installation restarts.
func TestEnsureDefaultConnectors_IsSafeToRunTwice(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewConnectorService(repo)
	ctx := t.Context()

	if err := svc.EnsureDefaultConnectors(ctx); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	first, _ := svc.ListConnectors(ctx)

	if err := svc.EnsureDefaultConnectors(ctx); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	second, _ := svc.ListConnectors(ctx)

	if len(second) != len(first) {
		t.Fatalf("seeding twice changed the catalogue from %d to %d connectors", len(first), len(second))
	}
}

// An instance belongs to a project and configures a connector. One with
// neither is unreachable: it cannot be listed, because listing filters by
// project, and it cannot be executed, because there is no connector to run.
//
// The UI sent {project_id, connector_id} while the API reads nested project and
// connector objects, so the ids were dropped and every instance created through
// the connector page was stored orphaned — with a 200 and a real id in reply.
func TestCreateConnectorInstance_RequiresAProjectAndAConnector(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewConnectorService(repo)
	ctx := t.Context()

	cases := []struct {
		name     string
		instance entities.ConnectorInstance
		wants    string
	}{
		{
			name:     "neither",
			instance: entities.ConnectorInstance{Name: "My Slack"},
			wants:    "project",
		},
		{
			name: "no connector",
			instance: entities.ConnectorInstance{
				Name:    "My Slack",
				Project: &entities.Project{ID: uuid.Must(uuid.NewV7())},
			},
			wants: "connector",
		},
		{
			name: "no project",
			instance: entities.ConnectorInstance{
				Name:      "My Slack",
				Connector: &entities.Connector{ID: uuid.Must(uuid.NewV7())},
			},
			wants: "project",
		},
		{
			name: "present but empty",
			instance: entities.ConnectorInstance{
				Name:      "My Slack",
				Project:   &entities.Project{},
				Connector: &entities.Connector{},
			},
			wants: "project",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateConnectorInstance(ctx, tc.instance)
			if err == nil {
				t.Fatal("an orphaned connector instance was accepted")
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Fatalf("error does not name the missing field %q: %v", tc.wants, err)
			}
		})
	}
}

func TestCreateConnectorInstance_AcceptsAWiredUpInstance(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewConnectorService(repo)
	ctx := t.Context()

	if err := svc.EnsureDefaultConnectors(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	catalogue, _ := svc.ListConnectors(ctx)
	if len(catalogue) == 0 {
		t.Fatal("no connectors to attach an instance to")
	}

	projectID := uuid.Must(uuid.NewV7())
	got, err := svc.CreateConnectorInstance(ctx, entities.ConnectorInstance{
		Name:      "Ops channel",
		Project:   &entities.Project{ID: projectID},
		Connector: &entities.Connector{ID: catalogue[0].ID},
		Config:    map[string]any{"webhook_url": "https://example.invalid/hook"},
	})
	if err != nil {
		t.Fatalf("a fully specified instance was rejected: %v", err)
	}
	if got.ID == uuid.Nil {
		t.Fatal("no id was assigned")
	}

	// It has to come back from the project-scoped list, which is the only way
	// the UI ever looks for it.
	listed, err := svc.ListConnectorInstances(ctx, projectID)
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("the project lists %d instances, want 1 — it was stored outside the project", len(listed))
	}
}

func hasConnector(list []entities.Connector, key string) bool {
	for _, c := range list {
		if c.Key == key {
			return true
		}
	}
	return false
}

// A listed instance should say which connector it configures.
//
// The row stores a connector id, and the adapter turned that into a Connector
// with nothing but the id set — so every instance came back with an empty key
// and name. The connectors page copes because it matches on the id against the
// catalogue it already has, but anything else reading the API sees an instance
// that names nothing, and the key is what a service task refers to.
func TestListConnectorInstances_NamesTheConnectorEachOneConfigures(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewConnectorService(repo)
	ctx := t.Context()

	if err := svc.EnsureDefaultConnectors(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	catalogue, _ := svc.ListConnectors(ctx)
	var slack entities.Connector
	for _, c := range catalogue {
		if c.Key == "slack-message" {
			slack = c
		}
	}
	if slack.ID == uuid.Nil {
		t.Fatal("the catalogue has no slack connector to attach to")
	}

	projectID := uuid.Must(uuid.NewV7())
	if _, err := svc.CreateConnectorInstance(ctx, entities.ConnectorInstance{
		Name:      "Ops channel",
		Project:   &entities.Project{ID: projectID},
		Connector: &entities.Connector{ID: slack.ID},
	}); err != nil {
		t.Fatalf("create instance: %v", err)
	}

	listed, err := svc.ListConnectorInstances(ctx, projectID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("got %d instances, want 1", len(listed))
	}
	got := listed[0]
	if got.Connector == nil || got.Connector.Key != "slack-message" {
		t.Errorf("connector key = %q, want \"slack-message\"", connectorKey(got.Connector))
	}
	if got.Connector.Name == "" {
		t.Error("connector name is empty, so a list cannot show what an instance is")
	}

	// And one fetched on its own, which is what an editor opens.
	one, err := svc.GetConnectorInstance(ctx, got.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if one.Connector == nil || one.Connector.Key != "slack-message" {
		t.Errorf("fetched instance names connector %q", connectorKey(one.Connector))
	}
}

func connectorKey(c *entities.Connector) string {
	if c == nil {
		return ""
	}
	return c.Key
}
