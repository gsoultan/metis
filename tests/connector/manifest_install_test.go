package connector_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// The gap this closes: the manifest executor was correct and tested, and
// nothing could reach it. `InstallManifest` lived on the concrete service rather
// than on the interface everything holds, no endpoint called it, and the
// registry was a map in memory — so a connector installed on one replica was
// unknown to the others and gone on the next deploy.
func TestAnInstalledManifestSurvivesARestart(t *testing.T) {
	t.Setenv("GOBPM_HTTP_ALLOW_PRIVATE_NETWORKS", "true")

	var called int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"lead-1"}`))
	}))
	defer api.Close()

	db := testutils.SetupTestDB(t)
	ctx := t.Context()

	document := []byte(`
key: crm.create-lead
version: 1
name: Create a lead
request:
  method: POST
  url: "` + api.URL + `/leads"
  body:
    name: "{{input.name}}"
response:
  outputs:
    lead_id: body.id
`)

	// One service installs it…
	installed, err := serviceimpl.NewConnectorService(repositories.NewRepository(db)).
		InstallManifest(ctx, document)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if installed.Key != "crm.create-lead" {
		t.Fatalf("installed %+v", installed)
	}

	// …and a completely separate one — a different replica, or this one after a
	// restart — can call it, because the manifest is in the database rather than
	// in the first service's memory.
	afterRestart := serviceimpl.NewConnectorService(repositories.NewRepository(db))
	outputs, err := afterRestart.ExecuteConnector(ctx, "crm.create-lead", nil, map[string]any{"name": "Rex"})
	if err != nil {
		t.Fatalf("execute after restart: %v", err)
	}
	if outputs["lead_id"] != "lead-1" {
		t.Errorf("outputs = %v, want the response mapped back", outputs)
	}
	if called != 1 {
		t.Errorf("the endpoint was called %d times, want once", called)
	}
}

// Installing again is how an author fixes a manifest, so it must replace rather
// than fail — and the fix has to be what is called next.
func TestInstallingTheSameKeyAgainReplacesIt(t *testing.T) {
	t.Setenv("GOBPM_HTTP_ALLOW_PRIVATE_NETWORKS", "true")

	var lastPath string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()

	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestDB(t)))
	ctx := t.Context()

	first := []byte("key: crm.x\nversion: 1\nrequest:\n  url: \"" + api.URL + "/old\"\n")
	if _, err := svc.InstallManifest(ctx, first); err != nil {
		t.Fatalf("first install: %v", err)
	}

	second := []byte("key: crm.x\nversion: 2\nrequest:\n  url: \"" + api.URL + "/corrected\"\n")
	if _, err := svc.InstallManifest(ctx, second); err != nil {
		t.Fatalf("installing a correction failed instead of replacing: %v", err)
	}

	if _, err := svc.ExecuteConnector(ctx, "crm.x", nil, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if lastPath != "/corrected" {
		t.Errorf("called %q; the correction did not replace the original", lastPath)
	}

	// And there is one row, not two.
	manifests, err := svc.ListManifests(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(manifests) != 1 || manifests[0].Version != 2 {
		t.Errorf("catalogue = %+v, want one entry at version 2", manifests)
	}
}

// An operator needs to stop a connector without deleting the document that
// defines it — deleting loses the manifest, and a switched-off connector is one
// somebody can switch back on.
func TestASwitchedOffManifestIsNotUsed(t *testing.T) {
	t.Setenv("GOBPM_HTTP_ALLOW_PRIVATE_NETWORKS", "true")

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()

	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestDB(t)))
	ctx := t.Context()

	installed, err := svc.InstallManifest(ctx, []byte("key: crm.y\nversion: 1\nrequest:\n  url: \""+api.URL+"\"\n"))
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := svc.SetManifestEnabled(ctx, installed.ID, false); err != nil {
		t.Fatalf("switch off: %v", err)
	}

	// With no manifest and no built-in under that key, there is nothing to call.
	if _, err := svc.ExecuteConnector(ctx, "crm.y", nil, nil); err == nil {
		t.Error("a switched-off manifest was still used")
	}

	// And back on again.
	if err := svc.SetManifestEnabled(ctx, installed.ID, true); err != nil {
		t.Fatalf("switch on: %v", err)
	}
	if _, err := svc.ExecuteConnector(ctx, "crm.y", nil, nil); err != nil {
		t.Errorf("switching a manifest back on did not restore it: %v", err)
	}
}

// A manifest replaces a built-in under the same key. That is what "without a
// redeploy" means: the Go connector stays in the binary and stops being used.
func TestAManifestReplacesABuiltIn(t *testing.T) {
	t.Setenv("GOBPM_HTTP_ALLOW_PRIVATE_NETWORKS", "true")

	var called bool
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()

	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestDB(t)))
	ctx := t.Context()

	// "http-json" is registered as a built-in Go executor.
	if _, err := svc.InstallManifest(ctx, []byte("key: http-json\nversion: 1\nrequest:\n  url: \""+api.URL+"\"\n")); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := svc.ExecuteConnector(ctx, "http-json", nil, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !called {
		t.Error("the built-in answered instead of the installed manifest")
	}
}

// An OpenAPI document installs one connector per operation, in one action.
func TestImportingASpecificationInstallsEveryOperation(t *testing.T) {
	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestDB(t)))

	spec := []byte(`
openapi: 3.0.3
info: {title: Petstore, version: "1"}
servers: [{url: "https://api.petstore.example"}]
paths:
  /pets:
    get: {operationId: listPets, responses: {"200": {description: ok}}}
    post: {operationId: createPet, responses: {"201": {description: ok}}}
`)
	installed, err := svc.ImportOpenAPI(t.Context(), spec)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(installed) != 2 {
		t.Fatalf("installed %d connectors, want one per operation", len(installed))
	}

	catalogue, err := svc.ListManifests(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(catalogue) != 2 {
		t.Errorf("catalogue holds %d, want the two that were imported", len(catalogue))
	}
}

// A manifest is stored as its author wrote it, so what an operator reads back is
// what they installed — comments and all.
func TestAManifestIsReadBackAsItWasWritten(t *testing.T) {
	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestDB(t)))
	ctx := t.Context()

	document := "# the vendor's own notes\nkey: crm.z\nversion: 1\nrequest:\n  url: https://example.com\n"
	if _, err := svc.InstallManifest(ctx, []byte(document)); err != nil {
		t.Fatalf("install: %v", err)
	}

	got, err := svc.GetManifestDocument(ctx, "crm.z")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != document {
		t.Errorf("read back %q, want exactly what was installed", got)
	}
}

// A document that could not work is refused at install, not at 3am.
func TestABrokenManifestIsRefusedAtInstall(t *testing.T) {
	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestDB(t)))

	if _, err := svc.InstallManifest(t.Context(), []byte("key: broken\nversion: 1\n")); err == nil {
		t.Error("a manifest with no request URL was installed")
	}
}

var _ = errors.Is
var _ = gorm.ErrRecordNotFound
