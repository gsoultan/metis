package connectors

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A small but realistic specification: a path parameter, a query parameter, a
// JSON body, a response schema, and bearer auth.
const petstoreSpec = `
openapi: 3.0.3
info:
  title: Petstore
  version: "1.0"
servers:
  - url: https://api.petstore.example/v1
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
paths:
  /pets/{petId}:
    get:
      operationId: getPet
      summary: Fetch one pet
      tags: [pets]
      parameters:
        - name: petId
          in: path
          required: true
          schema: { type: string }
        - name: verbose
          in: query
          schema: { type: boolean }
      responses:
        "200":
          description: the pet
          content:
            application/json:
              schema:
                type: object
                properties:
                  id: { type: string }
                  name: { type: string }
  /pets:
    post:
      operationId: createPet
      summary: Add a pet
      tags: [pets]
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [name]
              properties:
                name: { type: string }
                tag: { type: string }
      responses:
        "201":
          description: created
          content:
            application/json:
              schema:
                type: object
                properties:
                  id: { type: string }
`

func importSpec(t *testing.T, document string) map[string]Manifest {
	t.Helper()
	manifests, err := ImportOpenAPI([]byte(document))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	byKey := make(map[string]Manifest, len(manifests))
	for _, manifest := range manifests {
		byKey[manifest.Key] = manifest
	}
	return byKey
}

// "Does this integrate with X?" has been a roadmap question. Almost every API
// worth integrating with publishes one of these.
func TestImportingASpecificationProducesConnectors(t *testing.T) {
	byKey := importSpec(t, petstoreSpec)

	if len(byKey) != 2 {
		t.Fatalf("imported %d connectors, want one per operation: %v", len(byKey), keysOf(byKey))
	}

	get, found := byKey["petstore.getpet"]
	if !found {
		t.Fatalf("no connector named after the operation id: %v", keysOf(byKey))
	}
	if get.Name != "Fetch one pet" || get.Category != "pets" {
		t.Errorf("name/category = %q/%q", get.Name, get.Category)
	}
	if get.Method() != "GET" {
		t.Errorf("method = %q", get.Method())
	}
}

// A path parameter becomes an input template, using the name the spec already
// uses — which is the name the API's own documentation uses.
func TestAPathParameterBecomesATemplate(t *testing.T) {
	get := importSpec(t, petstoreSpec)["petstore.getpet"]

	if get.Request.URL != "https://api.petstore.example/v1/pets/{{input.petId}}" {
		t.Errorf("url = %q", get.Request.URL)
	}
	if get.Request.Query["verbose"] != "{{input.verbose}}" {
		t.Errorf("query = %v", get.Request.Query)
	}
}

// The generated input schema is what the designer draws a form from, so it has
// to carry everything the caller must supply — from the path, the query and the
// body alike.
func TestTheInputSchemaCollectsEverythingTheCallerSupplies(t *testing.T) {
	byKey := importSpec(t, petstoreSpec)

	get := byKey["petstore.getpet"]
	properties, _ := get.InputSchema["properties"].(map[string]any)
	if _, has := properties["petId"]; !has {
		t.Errorf("input schema = %v, want the path parameter", properties)
	}
	if _, has := properties["verbose"]; !has {
		t.Errorf("input schema = %v, want the query parameter", properties)
	}
	required, _ := get.InputSchema["required"].([]string)
	if len(required) != 1 || required[0] != "petId" {
		t.Errorf("required = %v, want just the path parameter", required)
	}

	post := byKey["petstore.createpet"]
	postProperties, _ := post.InputSchema["properties"].(map[string]any)
	if _, has := postProperties["name"]; !has {
		t.Errorf("input schema = %v, want the body's fields", postProperties)
	}
	postRequired, _ := post.InputSchema["required"].([]string)
	if len(postRequired) != 1 || postRequired[0] != "name" {
		t.Errorf("required = %v, want the body's required field", postRequired)
	}
}

func TestABodySchemaBecomesABodyTemplate(t *testing.T) {
	post := importSpec(t, petstoreSpec)["petstore.createpet"]

	if post.Request.Body["name"] != "{{input.name}}" || post.Request.Body["tag"] != "{{input.tag}}" {
		t.Errorf("body = %v", post.Request.Body)
	}
}

func TestAResponseSchemaBecomesOutputs(t *testing.T) {
	byKey := importSpec(t, petstoreSpec)

	get := byKey["petstore.getpet"]
	if get.Response.Outputs["id"] != "body.id" || get.Response.Outputs["name"] != "body.name" {
		t.Errorf("outputs = %v", get.Response.Outputs)
	}
	// A 201 counts as success just as much as a 200.
	post := byKey["petstore.createpet"]
	if post.Response.Outputs["id"] != "body.id" {
		t.Errorf("outputs = %v, want the 201's schema read", post.Response.Outputs)
	}
}

func TestSecurityBecomesAuth(t *testing.T) {
	if got := importSpec(t, petstoreSpec)["petstore.getpet"].Auth.Type; got != authBearer {
		t.Errorf("auth = %q, want bearer", got)
	}

	apiKeySpec := `
openapi: 3.0.3
info: {title: T, version: "1"}
components:
  securitySchemes:
    key:
      type: apiKey
      in: header
      name: X-API-Key
paths:
  /x:
    get:
      operationId: x
      responses: {"200": {description: ok}}
`
	auth := importSpec(t, apiKeySpec)["t.x"].Auth
	if auth.Type != authAPIKey || auth.Header != "X-API-Key" {
		t.Errorf("auth = %+v", auth)
	}
}

// The same spec is used against production and a sandbox, so the base is
// configuration rather than something baked into a connector.
func TestAMissingOrTemplatedServerBecomesConfiguration(t *testing.T) {
	noServer := `
openapi: 3.0.3
info: {title: T, version: "1"}
paths:
  /x:
    get: {operationId: x, responses: {"200": {description: ok}}}
`
	if url := importSpec(t, noServer)["t.x"].Request.URL; url != "{{config.base_url}}/x" {
		t.Errorf("url = %q, want the base to be configurable", url)
	}

	templated := `
openapi: 3.0.3
info: {title: T, version: "1"}
servers: [{url: "https://{region}.api.example.com"}]
paths:
  /x:
    get: {operationId: x, responses: {"200": {description: ok}}}
`
	if url := importSpec(t, templated)["t.x"].Request.URL; url != "{{config.base_url}}/x" {
		t.Errorf("url = %q; a server URL with its own placeholders is not usable as-is", url)
	}
}

// Every API fails in these two ways, and a boundary event that can catch them is
// worth more than a connector that treats every failure alike.
func TestGeneratedConnectorsKnowTheUniversalFailures(t *testing.T) {
	codes := map[string]bool{}
	for _, rule := range importSpec(t, petstoreSpec)["petstore.getpet"].Errors {
		codes[rule.BPMNError] = rule.Retryable
	}
	if retryable, found := codes["AUTH_FAILED"]; !found || retryable {
		t.Error("a generated connector does not treat an auth failure as fatal")
	}
	if retryable, found := codes["RATE_LIMITED"]; !found || !retryable {
		t.Error("a generated connector does not treat a rate limit as worth retrying")
	}
}

func TestASpecificationWithNothingToImportIsRefused(t *testing.T) {
	for name, document := range map[string]string{
		"no paths":     `{"openapi":"3.0.3","info":{"title":"T"},"paths":{}}`,
		"not a spec":   `just some text`,
		"no operation": `{"openapi":"3.0.3","info":{"title":"T"},"paths":{"/x":{"summary":"not a method"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ImportOpenAPI([]byte(document)); err == nil {
				t.Error("a document with nothing to import was accepted")
			}
		})
	}
}

// A list that reshuffles on every import is one nobody can diff.
func TestTheOrderIsStable(t *testing.T) {
	first, err := ImportOpenAPI([]byte(petstoreSpec))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	for range 5 {
		again, importErr := ImportOpenAPI([]byte(petstoreSpec))
		if importErr != nil {
			t.Fatalf("import: %v", importErr)
		}
		for i := range first {
			if again[i].Key != first[i].Key {
				t.Fatalf("the order changed between imports: %q then %q", first[i].Key, again[i].Key)
			}
		}
	}
}

// The point of the whole exercise: what comes out actually calls the API.
func TestAnImportedConnectorMakesTheCall(t *testing.T) {
	allowLoopback(t)

	var gotPath, gotQuery, gotAuth string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "p-1", "name": "Rex"})
	}))
	defer api.Close()

	manifest := importSpec(t, petstoreSpec)["petstore.getpet"]
	// The generated base points at the real API; a test points it at this one.
	manifest.Request.URL = api.URL + "/pets/{{input.petId}}"

	outputs, err := RunManifest(t.Context(), manifest,
		map[string]any{"token": "s3cret"},
		map[string]any{"petId": "p-1", "verbose": true}, api.Client())
	if err != nil {
		t.Fatalf("run imported connector: %v", err)
	}

	if gotPath != "/pets/p-1" {
		t.Errorf("path = %q; the imported path template did not fill in", gotPath)
	}
	if gotQuery != "verbose=true" {
		t.Errorf("query = %q", gotQuery)
	}
	if gotAuth != "Bearer s3cret" {
		t.Errorf("authorization = %q", gotAuth)
	}
	if outputs["name"] != "Rex" {
		t.Errorf("outputs = %v; the imported response mapping did not run", outputs)
	}
}

func keysOf(byKey map[string]Manifest) []string {
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	return keys
}
