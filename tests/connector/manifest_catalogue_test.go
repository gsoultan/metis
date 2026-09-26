package connector_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	handlersimpl "github.com/gsoultan/metis/server/domains/handlers/impl"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// A step names a catalogue entry, not a manifest. The designer offers what
// ListConnectors returns, a project's connection configures one of those
// entries, and the job resolves a step's connector_id to that connection and
// runs the entry's key — which is where an installed manifest is found. These
// tests take the same path: the catalogue, a connection, and TryConnectorStep,
// which resolves a step exactly as the job does.

// leadsManifest reads its address and its token from the connection.
const leadsManifest = `
key: crm.create-lead
version: 1
name: Create a lead
category: crm
auth:
  type: bearer
config_schema:
  type: object
  required: [base_url]
  properties:
    base_url: {type: string, title: CRM address}
request:
  method: POST
  url: "{{config.base_url}}/leads"
  body:
    name: "{{input.name}}"
response:
  outputs:
    lead_id: body.id
`

const leadsKey = "crm.create-lead"

type manifestCatalogueHarness struct {
	connectors servicecontracts.JobConnectorService
	jobs       servicecontracts.JobService
	ctx        context.Context
	projectID  uuid.UUID
}

func newManifestCatalogueHarness(t *testing.T) manifestCatalogueHarness {
	t.Helper()
	repo := repositories.NewRepository(testutils.SetupTestConn(t))
	connectors := serviceimpl.NewConnectorService(repo)
	engine := serviceimpl.NewExecutionEngine(repo, observersimpl.NewEventDispatcher())
	jobs := serviceimpl.NewJobService(repo, engine, connectors, serviceimpl.NewNoOpLocker(), handlersimpl.NewErrorBoundaryMatcher())
	ctx, _, projectID := testutils.ScopedProject(t, repo)
	return manifestCatalogueHarness{connectors: connectors, jobs: jobs, ctx: ctx, projectID: projectID}
}

// offered returns the catalogue entry under key, if the catalogue offers one.
func (h manifestCatalogueHarness) offered(t *testing.T, key string) (entities.Connector, bool) {
	t.Helper()
	catalogue, err := h.connectors.ListConnectors(h.ctx)
	if err != nil {
		t.Fatalf("list the catalogue: %v", err)
	}
	for _, c := range catalogue {
		if c.Key == key {
			return c, true
		}
	}
	return entities.Connector{}, false
}

// connect saves the project's connection to a catalogue entry.
func (h manifestCatalogueHarness) connect(t *testing.T, entry entities.Connector, config map[string]any) {
	t.Helper()
	if _, err := h.connectors.CreateConnectorInstance(h.ctx, entities.ConnectorInstance{
		Name:      "CRM",
		Project:   &entities.Project{ID: h.projectID},
		Connector: &entities.Connector{ID: entry.ID},
		Config:    config,
	}); err != nil {
		t.Fatalf("connect %s: %v", entry.Key, err)
	}
}

// tryStep runs a step that chose the catalogue entry id, as the job would.
func (h manifestCatalogueHarness) tryStep(id uuid.UUID) (map[string]any, error) {
	return h.jobs.TryConnectorStep(h.ctx, h.projectID, entities.Node{
		ID: "lead", Type: entities.ServiceTask, Properties: map[string]any{"connector_id": id.String()},
	}, map[string]any{"name": "Rex"})
}

// leadsCall is what the partner's API was last asked.
type leadsCall struct {
	path, authorization string
}

func leadsAPI(t *testing.T) (*httptest.Server, *leadsCall) {
	t.Helper()
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	received := &leadsCall{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.path, received.authorization = r.URL.Path, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"lead-1"}`))
	}))
	t.Cleanup(api.Close)
	return api, received
}

func hasProperty(schema []entities.ConnectorProperty, key string) bool {
	return slices.ContainsFunc(schema, func(p entities.ConnectorProperty) bool { return p.Key == key })
}

// Installing a connector has to make it usable: offered where a modeller picks
// connectors and where a project connects them, with a connection form that
// asks for what the manifest reads — and a step that chose it runs it.
func TestAnInstalledManifestCanBeChosenConnectedAndRun(t *testing.T) {
	api, received := leadsAPI(t)
	h := newManifestCatalogueHarness(t)

	if _, err := h.connectors.InstallManifest(h.ctx, []byte(leadsManifest)); err != nil {
		t.Fatalf("install: %v", err)
	}

	entry, offered := h.offered(t, leadsKey)
	if !offered {
		t.Fatal("the installed manifest is not in the catalogue the designer and the Connectors page offer")
	}
	if entry.Name != "Create a lead" || entry.Type != "crm" {
		t.Errorf("offered as %q in %q, want the manifest's name and category", entry.Name, entry.Type)
	}
	for _, setting := range []string{"base_url", "token"} {
		if !hasProperty(entry.Schema, setting) {
			t.Errorf("the connection form does not ask for %q, which the manifest reads: %+v", setting, entry.Schema)
		}
	}

	h.connect(t, entry, map[string]any{"base_url": api.URL, "token": "s3cret"})
	stored, err := h.tryStep(entry.ID)
	if err != nil {
		t.Fatalf("a step that chose the installed connector did not run: %v", err)
	}
	if received.path != "/leads" || received.authorization != "Bearer s3cret" {
		t.Errorf("called %q with %q, want the manifest's /leads with the connection's token",
			received.path, received.authorization)
	}
	if stored["lead_id"] != "lead-1" {
		t.Errorf("the step would store %v, want the lead id mapped back", stored)
	}
}

// Switching a connector off or removing it takes it out of the catalogue, so
// nobody builds a new step on something that will not run. Switching it back
// on, or installing it again, brings back the same entry, so the steps and
// connections that used it work again — which is what the Connectors page
// promises when it asks before removing one.
func TestAManifestLeavesTheCatalogueWhileOffOrRemovedAndComesBackAsItWas(t *testing.T) {
	api, _ := leadsAPI(t)
	h := newManifestCatalogueHarness(t)

	installed, err := h.connectors.InstallManifest(h.ctx, []byte(leadsManifest))
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	entry, offered := h.offered(t, leadsKey)
	if !offered {
		t.Fatal("the installed manifest is not in the catalogue")
	}
	h.connect(t, entry, map[string]any{"base_url": api.URL, "token": "s3cret"})

	// In order: it is switched off and on while installed, then removed.
	for _, round := range []struct {
		name             string
		leave, bringBack func() error
	}{
		{
			name:      "switched off",
			leave:     func() error { return h.connectors.SetManifestEnabled(h.ctx, installed.ID, false) },
			bringBack: func() error { return h.connectors.SetManifestEnabled(h.ctx, installed.ID, true) },
		},
		{
			name:  "removed",
			leave: func() error { return h.connectors.DeleteManifest(h.ctx, installed.ID) },
			bringBack: func() error {
				_, err := h.connectors.InstallManifest(h.ctx, []byte(leadsManifest))
				return err
			},
		},
	} {
		if err := round.leave(); err != nil {
			t.Fatalf("%s: %v", round.name, err)
		}
		if _, still := h.offered(t, leadsKey); still {
			t.Errorf("%s, it is still offered to modellers", round.name)
		}
		// A step that already uses it fails, and says why: its connection is
		// still there, and what it connects to was taken away.
		if _, err := h.tryStep(entry.ID); err == nil || !strings.Contains(err.Error(), "switched off or removed") {
			t.Errorf("%s, a step that uses it fails with %v, which does not say what happened", round.name, err)
		}
		if err := round.bringBack(); err != nil {
			t.Fatalf("bringing it back after it was %s: %v", round.name, err)
		}
		back, offered := h.offered(t, leadsKey)
		if !offered || back.ID != entry.ID {
			t.Fatalf("after it was %s and brought back, the catalogue offers %+v (offered=%v), want entry %s, "+
				"which the steps and connections that used it name", round.name, back, offered, entry.ID)
		}
		if _, err := h.tryStep(entry.ID); err != nil {
			t.Errorf("after it was %s and brought back, a step that chose it does not run: %v", round.name, err)
		}
	}
}

// A manifest may replace a built-in under the built-in's key where the operator
// allows it, as TestAManifestReplacesABuiltIn pins. The built-in's catalogue
// entry is not the manifest's to rewrite or withdraw: switched off or removed,
// the manifest stops answering and the built-in answers again, under the entry
// that was always there.
func TestAManifestUnderABuiltInsKeyLeavesTheBuiltInsEntryAlone(t *testing.T) {
	t.Setenv("METIS_ALLOW_BUILTIN_CONNECTOR_OVERRIDE", "true")
	h := newManifestCatalogueHarness(t)

	before, offered := h.offered(t, "http-json")
	if !offered {
		t.Fatal("the catalogue has no http-json entry to compare against")
	}
	installed, err := h.connectors.InstallManifest(h.ctx,
		[]byte("key: http-json\nversion: 1\nname: Not the built-in\nrequest:\n  url: https://example.com\n"))
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	// In order: each step starts from where the one before left the manifest.
	for _, step := range []struct {
		name   string
		change func() error
	}{
		{"installed", func() error { return nil }},
		{"switched off", func() error { return h.connectors.SetManifestEnabled(h.ctx, installed.ID, false) }},
		{"removed", func() error { return h.connectors.DeleteManifest(h.ctx, installed.ID) }},
	} {
		if err := step.change(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		after, offered := h.offered(t, "http-json")
		if !offered || !reflect.DeepEqual(after, before) {
			t.Errorf("with the manifest %s, the built-in's entry is %+v (offered=%v), want it untouched: %+v",
				step.name, after, offered, before)
		}
	}
}
