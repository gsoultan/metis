package connectors

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The manifest from the plan, near enough: a real connector written as data.
const salesforceManifest = `
key: salesforce.create-lead
version: 2
name: Salesforce — Create Lead
category: crm
auth:
  type: bearer
config_schema:
  type: object
  required: [instance_url]
input_schema:
  type: object
  required: [last_name, company]
request:
  method: POST
  url: "{{config.instance_url}}/services/data/v60.0/sobjects/Lead"
  headers:
    X-Trace: "{{input.trace_id}}"
  body:
    LastName: "{{input.last_name}}"
    Company: "{{input.company}}"
    Score: "{{input.score}}"
response:
  success: "status >= 200 and status < 300"
  outputs:
    lead_id: "body.id"
errors:
  - when: "status = 401"
    bpmn_error: AUTH_FAILED
    retryable: false
  - when: "status = 429"
    bpmn_error: RATE_LIMITED
    retryable: true
    retry_after: "headers['Retry-After']"
`

// allowLoopback lets a test reach its own httptest server.
//
// The egress policy blocks loopback by default, and that default is load-bearing
// — a manifest is configuration, and configuration reaches places nobody
// intended. TestAManifestCannotReachAPrivateAddress is the test that proves it,
// and it deliberately does not call this.
func allowLoopback(t *testing.T) {
	t.Helper()
	t.Setenv("GOBPM_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
}

func parse(t *testing.T, document string) Manifest {
	t.Helper()
	manifest, err := ParseManifest([]byte(document))
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	return manifest
}

// A connector has been a Go switch branch: one function per vendor, in a file
// that only grows, and adding one means editing Go and redeploying. This is the
// same connector as a document.
func TestAManifestDescribesACall(t *testing.T) {
	manifest := parse(t, salesforceManifest)

	if manifest.Key != "salesforce.create-lead" || manifest.Version != 2 {
		t.Errorf("key/version = %q/%d", manifest.Key, manifest.Version)
	}
	if manifest.Method() != "POST" {
		t.Errorf("method = %q", manifest.Method())
	}
	if len(manifest.Errors) != 2 {
		t.Errorf("error rules = %d, want 2", len(manifest.Errors))
	}
}

// JSON is what a machine generates — an OpenAPI import, a catalogue download —
// and one parser rather than two is one fewer thing that can disagree.
func TestAManifestCanBeJSON(t *testing.T) {
	manifest, err := ParseManifest([]byte(`{"key":"x","request":{"method":"GET","url":"https://example.com"}}`))
	if err != nil {
		t.Fatalf("parse JSON manifest: %v", err)
	}
	if manifest.Key != "x" || manifest.Method() != "GET" {
		t.Errorf("parsed %+v", manifest)
	}
}

// A manifest is installed once and called thousands of times. The moment to
// discover it names no URL is when somebody installs it, not when an instance
// reaches it at 3am.
func TestAManifestThatCouldNotWorkIsRefused(t *testing.T) {
	cases := map[string]string{
		"no key":             `{"request":{"url":"https://example.com"}}`,
		"no URL":             `{"key":"x","request":{"method":"GET"}}`,
		"not a method":       `{"key":"x","request":{"method":"FETCH","url":"https://example.com"}}`,
		"unknown auth":       `{"key":"x","auth":{"type":"magic"},"request":{"url":"https://example.com"}}`,
		"oauth no token":     `{"key":"x","auth":{"type":"oauth2_client_credentials"},"request":{"url":"https://e.com"}}`,
		"error with no when": `{"key":"x","request":{"url":"https://e.com"},"errors":[{"bpmn_error":"X"}]}`,
		"error with no code": `{"key":"x","request":{"url":"https://e.com"},"errors":[{"when":"status = 1"}]}`,
	}
	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(document)); err == nil {
				t.Error("a manifest that could not work was accepted")
			}
		})
	}
}

// The whole point: a call made from a document.
func TestRunningAManifestMakesTheCallItDescribes(t *testing.T) {
	allowLoopback(t)
	var gotPath, gotAuth, gotTrace string
	var gotBody map[string]any

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotTrace = r.Header.Get("X-Trace")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"00Q5f000001abcd"}`))
	}))
	defer api.Close()

	outputs, err := RunManifest(t.Context(), parse(t, salesforceManifest),
		map[string]any{"instance_url": api.URL, "token": "s3cret"},
		map[string]any{"last_name": "Kowalski", "company": "Northwind", "score": 42.0, "trace_id": "abc"},
		api.Client())
	if err != nil {
		t.Fatalf("run manifest: %v", err)
	}

	if gotPath != "/services/data/v60.0/sobjects/Lead" {
		t.Errorf("path = %q; the URL template was not filled in", gotPath)
	}
	if gotAuth != "Bearer s3cret" {
		t.Errorf("authorization = %q", gotAuth)
	}
	if gotTrace != "abc" {
		t.Errorf("X-Trace = %q; a header template was not filled in", gotTrace)
	}
	if gotBody["LastName"] != "Kowalski" || gotBody["Company"] != "Northwind" {
		t.Errorf("body = %v", gotBody)
	}
	if outputs["lead_id"] != "00Q5f000001abcd" {
		t.Errorf("lead_id = %v; the response mapping did not run", outputs["lead_id"])
	}
}

// A body field that should be a number and arrives as text is rejected by
// roughly every API that cares, so a template that is entirely one expression
// keeps its value's type.
func TestAWholeTemplateKeepsItsType(t *testing.T) {
	allowLoopback(t)
	var body map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()

	manifest := parse(t, `
key: x
request:
  method: POST
  url: "`+api.URL+`"
  body:
    count: "{{input.count}}"
    flag: "{{input.flag}}"
    mixed: "n={{input.count}}"
`)
	if _, err := RunManifest(t.Context(), manifest, nil,
		map[string]any{"count": 7.0, "flag": true}, api.Client()); err != nil {
		t.Fatalf("run manifest: %v", err)
	}

	if body["count"] != 7.0 {
		t.Errorf("count = %#v, want the number 7", body["count"])
	}
	if body["flag"] != true {
		t.Errorf("flag = %#v, want the boolean true", body["flag"])
	}
	if body["mixed"] != "n=7" {
		t.Errorf("mixed = %#v, want the text n=7", body["mixed"])
	}
}

// Most APIs read an explicit null as "clear this field", which is not what an
// absent input meant.
func TestAnAbsentValueIsOmittedRatherThanSentAsNull(t *testing.T) {
	allowLoopback(t)
	var body map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()

	manifest := parse(t, `
key: x
request:
  method: POST
  url: "`+api.URL+`"
  headers:
    X-Optional: "{{input.missing}}"
  body:
    present: "{{input.here}}"
    absent: "{{input.missing}}"
`)
	if _, err := RunManifest(t.Context(), manifest, nil, map[string]any{"here": "yes"}, api.Client()); err != nil {
		t.Fatalf("run manifest: %v", err)
	}

	if _, sent := body["absent"]; sent {
		t.Error("a field whose template found nothing was sent anyway")
	}
	if body["present"] != "yes" {
		t.Errorf("present = %v", body["present"])
	}
}

// An integration failing becomes a path in the diagram rather than a stack
// trace in an incident.
func TestAFailureBecomesABPMNError(t *testing.T) {
	allowLoopback(t)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"slow down"}`))
	}))
	defer api.Close()

	manifest := parse(t, strings.Replace(salesforceManifest, "{{config.instance_url}}", api.URL, 1))
	_, err := RunManifest(t.Context(), manifest,
		map[string]any{"instance_url": api.URL, "token": "s3cret"},
		map[string]any{"last_name": "K", "company": "N"}, api.Client())

	var failure *ManifestError
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want a ManifestError a boundary event could catch", err)
	}
	if failure.BPMNErrorCode() != "RATE_LIMITED" {
		t.Errorf("code = %q, want RATE_LIMITED", failure.BPMNErrorCode())
	}
	if !failure.Retryable {
		t.Error("a 429 was marked as not worth retrying")
	}
	// Honouring what a partner asks for is the difference between backing off
	// and being blocked.
	if failure.RetryAfter != 120*time.Second {
		t.Errorf("retry after = %v, want the two minutes the partner asked for", failure.RetryAfter)
	}
}

// Plenty of APIs answer a failure with 200 and a body saying so, which is why
// the error rules are checked before the success condition.
func TestAFailureDisguisedAsSuccessIsStillCaught(t *testing.T) {
	allowLoopback(t)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":"card_declined"}`))
	}))
	defer api.Close()

	manifest := parse(t, `
key: x
request:
  url: "`+api.URL+`"
errors:
  - when: 'body.error = "card_declined"'
    bpmn_error: CARD_DECLINED
`)
	_, err := RunManifest(t.Context(), manifest, nil, nil, api.Client())

	var failure *ManifestError
	if !errors.As(err, &failure) || failure.BPMNErrorCode() != "CARD_DECLINED" {
		t.Fatalf("error = %v, want CARD_DECLINED", err)
	}
}

// A manifest with no success condition means the usual thing.
func TestSuccessDefaultsToTheUsualMeaning(t *testing.T) {
	allowLoopback(t)
	for status, wantErr := range map[int]bool{200: false, 204: false, 400: true, 500: true} {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		manifest := parse(t, "key: x\nrequest:\n  url: \""+api.URL+"\"\n")
		_, err := RunManifest(t.Context(), manifest, nil, nil, api.Client())
		if (err != nil) != wantErr {
			t.Errorf("status %d gave err=%v, wantErr=%v", status, err, wantErr)
		}
		api.Close()
	}
}

// Credentials come from the tenant's configuration and never from a node's
// input, or a modeller could read one by mapping it into a variable.
func TestCredentialsAreNotReachableFromANodesInput(t *testing.T) {
	allowLoopback(t)
	var gotAuth string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()

	manifest := parse(t, "key: x\nauth:\n  type: bearer\nrequest:\n  url: \""+api.URL+"\"\n")

	// A node supplying its own "token" must not become the credential.
	_, err := RunManifest(t.Context(), manifest,
		map[string]any{"token": "the-real-one"},
		map[string]any{"token": "an-attempt"}, api.Client())
	if err != nil {
		t.Fatalf("run manifest: %v", err)
	}
	if gotAuth != "Bearer the-real-one" {
		t.Errorf("authorization = %q; a node's input reached the credential", gotAuth)
	}
}

func TestAMissingCredentialIsRefusedBeforeTheCall(t *testing.T) {
	allowLoopback(t)
	called := false
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()

	manifest := parse(t, "key: x\nauth:\n  type: bearer\nrequest:\n  url: \""+api.URL+"\"\n")
	if _, err := RunManifest(t.Context(), manifest, nil, nil, api.Client()); err == nil {
		t.Error("a connector with no configured token made the call anyway")
	}
	if called {
		t.Error("the call was made without a credential")
	}
}

// The egress policy exists because configuration reaches places nobody
// intended, and a manifest is configuration.
func TestAManifestCannotReachAPrivateAddress(t *testing.T) {
	manifest := parse(t, "key: x\nrequest:\n  url: \"http://169.254.169.254/latest/meta-data/\"\n")
	if _, err := RunManifest(t.Context(), manifest, nil, nil, nil); err == nil {
		t.Error("a manifest reached the cloud metadata service")
	}
}

func TestQueryParametersAreTemplatesToo(t *testing.T) {
	allowLoopback(t)
	var gotQuery string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()

	manifest := parse(t, `
key: x
request:
  method: GET
  url: "`+api.URL+`"
  query:
    q: "{{input.term}}"
`)
	if _, err := RunManifest(t.Context(), manifest, nil, map[string]any{"term": "north wind"}, api.Client()); err != nil {
		t.Fatalf("run manifest: %v", err)
	}
	if gotQuery != "north wind" {
		t.Errorf("q = %q", gotQuery)
	}
}

// A response that is not JSON is not a failure — plenty of endpoints answer with
// nothing at all.
func TestANonJSONResponseIsNotAFailure(t *testing.T) {
	allowLoopback(t)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`thanks`))
	}))
	defer api.Close()

	manifest := parse(t, "key: x\nrequest:\n  url: \""+api.URL+"\"\n")
	outputs, err := RunManifest(t.Context(), manifest, nil, nil, api.Client())
	if err != nil {
		t.Fatalf("a plain-text response was treated as a failure: %v", err)
	}
	if len(outputs) != 0 {
		t.Errorf("outputs = %v, want none", outputs)
	}
}
