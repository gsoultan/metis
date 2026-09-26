package connector_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/transports/https/common"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// The gap this closes: the manifest executor was correct and tested, and
// nothing could reach it. `InstallManifest` lived on the concrete service rather
// than on the interface everything holds, no endpoint called it, and the
// registry was a map in memory — so a connector installed on one replica was
// unknown to the others and gone on the next deploy.
func TestAnInstalledManifestSurvivesARestart(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")

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
	installed, err := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.StormConn(db))).
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
	afterRestart := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.StormConn(db)))
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
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")

	var lastPath string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()

	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestConn(t)))
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
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()

	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestConn(t)))
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

// Installing again is how an author fixes a document. An administrator who
// switched a connector off — the partner asked us to stop calling it — and then
// fixed its document has not asked for it to be switched back on.
func TestReinstallingASwitchedOffManifestLeavesItOff(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")

	var called int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()

	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestConn(t)))
	ctx := t.Context()

	installed, err := svc.InstallManifest(ctx, []byte("key: crm.v\nversion: 1\nrequest:\n  url: \""+api.URL+"/old\"\n"))
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !installed.Enabled {
		t.Fatal("a manifest installed for the first time was switched off")
	}
	if err := svc.SetManifestEnabled(ctx, installed.ID, false); err != nil {
		t.Fatalf("switch off: %v", err)
	}

	fixed, err := svc.InstallManifest(ctx, []byte("key: crm.v\nversion: 1\nrequest:\n  url: \""+api.URL+"/fixed\"\n"))
	if err != nil {
		t.Fatalf("installing the fixed document: %v", err)
	}
	if fixed.Enabled {
		t.Error("installing a fixed document switched the connector back on")
	}
	if _, err := svc.ExecuteConnector(ctx, "crm.v", nil, nil); err == nil || called != 0 {
		t.Errorf("the switched-off connector was called %d times after its document was fixed (err=%v)", called, err)
	}
}

// An older document installed over a newer one quietly takes a connector back
// to behaviour somebody had moved on from — usually a stale copy found in a
// download folder. The same version again is how a document is fixed, and a
// later one is an upgrade; only going back is refused.
func TestAnOlderVersionIsNotInstalledOverANewerOne(t *testing.T) {
	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestConn(t)))
	ctx := t.Context()

	newer := "key: crm.u\nversion: 3\nrequest:\n  url: https://example.com/v3\n"
	if _, err := svc.InstallManifest(ctx, []byte(newer)); err != nil {
		t.Fatalf("install version 3: %v", err)
	}

	_, err := svc.InstallManifest(ctx, []byte("key: crm.u\nversion: 2\nrequest:\n  url: https://example.com/v2\n"))
	if err == nil {
		t.Fatal("version 2 was installed over version 3")
	}
	if !errors.Is(err, apierr.ErrInvalidArgument) || common.CodeFrom(err) != http.StatusBadRequest {
		t.Errorf("the refusal is not a 400 (status %d): %v", common.CodeFrom(err), err)
	}
	if !strings.Contains(err.Error(), "version 3") || !strings.Contains(err.Error(), "version 2") {
		t.Errorf("the refusal does not name both versions: %v", err)
	}
	if got, readErr := svc.GetManifestDocument(ctx, "crm.u"); readErr != nil || got != newer {
		t.Errorf("after the refusal the installed document is %q (err=%v), want version 3 untouched", got, readErr)
	}

	for _, document := range []string{
		"key: crm.u\nversion: 3\nrequest:\n  url: https://example.com/v3-fixed\n",
		"key: crm.u\nversion: 4\nrequest:\n  url: https://example.com/v4\n",
	} {
		if _, err := svc.InstallManifest(ctx, []byte(document)); err != nil {
			t.Fatalf("installing %q was refused: %v", document, err)
		}
	}
	manifests, err := svc.ListManifests(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(manifests) != 1 || manifests[0].Version != 4 {
		t.Errorf("catalogue = %+v, want one entry at version 4", manifests)
	}
}

// A manifest replaces a built-in under the same key, where the operator allows
// it. That is what "without a redeploy" means: the Go connector stays in the
// binary and stops being used.
func TestAManifestReplacesABuiltIn(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	t.Setenv("METIS_ALLOW_BUILTIN_CONNECTOR_OVERRIDE", "true")

	var called bool
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()

	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestConn(t)))
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

// A manifest under a built-in's key replaces that connector in every step of
// every organization that uses it, so it is not something installing a
// document may do by itself: without the operator's say-so it is refused, and
// the refusal names the key.
func TestAManifestUnderABuiltInsKeyIsRefusedUnlessTheOperatorAllowsIt(t *testing.T) {
	t.Setenv("METIS_ALLOW_BUILTIN_CONNECTOR_OVERRIDE", "")
	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestConn(t)))
	ctx := t.Context()

	for _, key := range serviceimpl.BuiltInConnectorKeys() {
		_, err := svc.InstallManifest(ctx, []byte("key: "+key+"\nversion: 1\nrequest:\n  url: https://example.com\n"))
		if err == nil {
			t.Errorf("a manifest took the built-in %q", key)
			continue
		}
		if common.CodeFrom(err) != http.StatusBadRequest {
			t.Errorf("%s: the refusal is not a 400 (status %d): %v", key, common.CodeFrom(err), err)
		}
		if !strings.Contains(err.Error(), `"`+key+`"`) || !strings.Contains(err.Error(), "METIS_ALLOW_BUILTIN_CONNECTOR_OVERRIDE") {
			t.Errorf("%s: the refusal does not name the key and the setting: %v", key, err)
		}
	}

	manifests, err := svc.ListManifests(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(manifests) != 0 {
		t.Errorf("after the refusals the catalogue holds %+v, want nothing", manifests)
	}
}

// Switching a manifest on puts it back in every step that names its key, which
// for a built-in's key is taking that connector over again: the same decision
// as installing it, and the operator's to make. A manifest installed under a
// built-in's key while overrides were allowed — or before this release — and
// then switched off is not switched back on once they are not.
func TestAManifestUnderABuiltInsKeyIsNotSwitchedBackOnUnlessTheOperatorAllowsIt(t *testing.T) {
	t.Setenv("METIS_ALLOW_BUILTIN_CONNECTOR_OVERRIDE", "true")
	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestConn(t)))
	ctx := t.Context()

	installed, err := svc.InstallManifest(ctx, []byte("key: http-json\nversion: 1\nrequest:\n  url: https://example.com\n"))
	if err != nil {
		t.Fatalf("install under the built-in's key while overrides are allowed: %v", err)
	}
	if err := svc.SetManifestEnabled(ctx, installed.ID, false); err != nil {
		t.Fatalf("switch off: %v", err)
	}

	t.Setenv("METIS_ALLOW_BUILTIN_CONNECTOR_OVERRIDE", "")
	err = svc.SetManifestEnabled(ctx, installed.ID, true)
	if err == nil {
		t.Fatal("a manifest under the built-in's key was switched back on without the operator allowing overrides")
	}
	if common.CodeFrom(err) != http.StatusBadRequest {
		t.Errorf("the refusal is not a 400 (status %d): %v", common.CodeFrom(err), err)
	}
	if !strings.Contains(err.Error(), `"http-json"`) || !strings.Contains(err.Error(), "METIS_ALLOW_BUILTIN_CONNECTOR_OVERRIDE") {
		t.Errorf("the refusal does not name the key and the setting: %v", err)
	}

	manifests, err := svc.ListManifests(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(manifests) != 1 || manifests[0].Enabled {
		t.Errorf("after the refusal the catalogue holds %+v, want the manifest still switched off", manifests)
	}

	// Switching it off is always allowed: it hands the key back to the built-in.
	if err := svc.SetManifestEnabled(ctx, installed.ID, false); err != nil {
		t.Errorf("switching an override off again was refused: %v", err)
	}
}

// An OpenAPI document installs one connector per operation, in one action.
func TestImportingASpecificationInstallsEveryOperation(t *testing.T) {
	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestConn(t)))

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

// Importing a specification is one action, so it lands whole or not at all.
// Operations the importer cannot read are still skipped, but an install that
// fails among what it generated used to leave the ones before it installed and
// the ones after it not — with an error that did not say which had failed.
func TestAnImportThatCannotInstallEveryOperationInstallsNone(t *testing.T) {
	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestConn(t)))
	ctx := t.Context()

	// Somebody took the imported createPet further and marked it version 2, so
	// the import's version 1 of it is refused.
	custom := "key: petstore.createpet\nversion: 2\nrequest:\n  url: https://api.petstore.example/pets\n"
	if _, err := svc.InstallManifest(ctx, []byte(custom)); err != nil {
		t.Fatalf("install the customised operation: %v", err)
	}

	spec := []byte(`
openapi: 3.0.3
info: {title: Petstore, version: "1"}
servers: [{url: "https://api.petstore.example"}]
paths:
  /pets:
    get: {operationId: listPets, responses: {"200": {description: ok}}}
    post: {operationId: createPet, responses: {"201": {description: ok}}}
`)
	_, err := svc.ImportOpenAPI(ctx, spec)
	if err == nil {
		t.Fatal("the import reported success although one of its operations could not be installed")
	}
	if !strings.Contains(err.Error(), "petstore.createpet") || !strings.Contains(err.Error(), "nothing") {
		t.Errorf("the error does not say which operation failed and that nothing was installed: %v", err)
	}

	manifests, err := svc.ListManifests(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(manifests) != 1 || manifests[0].Key != "petstore.createpet" || manifests[0].Version != 2 {
		t.Errorf("after the failed import the catalogue holds %+v, want only the customised createPet at version 2", manifests)
	}
}

// A manifest is stored as its author wrote it, so what an operator reads back is
// what they installed — comments and all.
func TestAManifestIsReadBackAsItWasWritten(t *testing.T) {
	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestConn(t)))
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
	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestConn(t)))

	if _, err := svc.InstallManifest(t.Context(), []byte("key: broken\nversion: 1\n")); err == nil {
		t.Error("a manifest with no request URL was installed")
	}
}

var _ = errors.Is
var _ = gorm.ErrRecordNotFound

// A document that is not a connector is the sender's mistake, and a 400 says
// so. It was a 500: that pages somebody over a typo, and spends the error
// budget the engine's own failures are measured against.
func TestADocumentThatIsNotAConnectorIsTheSendersMistake(t *testing.T) {
	db := testutils.SetupTestDB(t)
	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.StormConn(db)))
	ctx := t.Context()

	for name, install := range map[string]func() error{
		"a manifest with no key or address": func() error {
			_, err := svc.InstallManifest(ctx, []byte("version: 1\nname: Nothing to call\n"))
			return err
		},
		"a manifest that is not YAML": func() error {
			_, err := svc.InstallManifest(ctx, []byte("key: [unclosed"))
			return err
		},
		"a specification that is not OpenAPI": func() error {
			_, err := svc.ImportOpenAPI(ctx, []byte("{not json or yaml"))
			return err
		},
	} {
		if err := install(); !errors.Is(err, apierr.ErrInvalidArgument) {
			t.Errorf("%s: %v, want it refused as invalid input", name, err)
		}
	}
}
