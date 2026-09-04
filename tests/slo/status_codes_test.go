package slo

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// A caller's own mistake must not be reported as a server failure.
//
// Every endpoint that takes an identifier used to return the raw uuid.Parse
// error, and the HTTP encoder maps anything it does not recognise to 500. So a
// client typo — 42 of them across the API — spent the roadmap's 0.1% 5xx error
// budget, and would page whoever is on call for a request the server answered
// exactly as it should have.
//
// This runs through the real handler, because the mapping only exists at the
// transport boundary: an endpoint test would see the error and never learn what
// status it becomes.
func TestMalformedIdentifiersAreClientErrors(t *testing.T) {
	h := newHarness(t)

	testCases := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"list instances with no project", http.MethodGet, "/api/v1/instances", nil},
		{"list instances with a nonsense project", http.MethodGet, "/api/v1/instances?project_id=banana", nil},
		{"get an instance by a nonsense id", http.MethodGet, "/api/v1/instances/banana", nil},
		// The REST surface exposes export rather than a bare get — a single
		// definition is fetched over Connect RPC, which is what the UI uses.
		{"export a definition by a nonsense id", http.MethodGet, "/api/v1/definitions/banana/export", nil},
		{"delete a definition by a nonsense id", http.MethodDelete, "/api/v1/definitions/banana", nil},
		{"get a task by a nonsense id", http.MethodGet, "/api/v1/tasks/banana", nil},
		{"import into a nonsense project", http.MethodPost, "/api/v1/definitions/import",
			map[string]any{"project_id": "banana", "xml": []byte("<x/>")}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			status := h.call(tc.method, tc.path, h.token, tc.body, nil)
			if status >= 500 {
				t.Errorf("%s %s returned %d: a malformed identifier is the caller's error, and a 5xx spends the error budget the engine is measured by",
					tc.method, tc.path, status)
			}
			if status != http.StatusBadRequest {
				t.Errorf("%s %s returned %d, want 400 so the caller knows to fix the request rather than retry it",
					tc.method, tc.path, status)
			}
		})
	}
}

// The counterpart: a well-formed request for something that is simply not there
// must not be a 400 either, or a caller cannot tell "you asked wrongly" from
// "it is not here".
func TestWellFormedRequestsStillSucceed(t *testing.T) {
	h := newHarness(t)

	if status := h.call(http.MethodGet, "/api/v1/instances?project_id="+h.projID.String(), h.token, nil, nil); status != http.StatusOK {
		t.Errorf("listing a real project's instances returned %d, want 200", status)
	}
	if status := h.call(http.MethodGet, "/api/v1/tasks", h.token, nil, nil); status != http.StatusOK {
		t.Errorf("listing tasks returned %d, want 200", status)
	}
}

// Something that is not there is not a server failure either.
//
// The sibling of the test above, and it was missing. A well-formed identifier
// that names nothing reached GORM, came back as ErrRecordNotFound, and the HTTP
// encoder turned anything it did not recognise into a 500 — so following a
// bookmark to a deleted instance, or asking for a task that has been completed
// and cleaned up, spent the 0.1% error budget and would page whoever is on
// call for a request the server had answered correctly.
//
// Measured before the repositories classified it: six of these nine returned
// 500.
func TestAbsentThingsAreClientErrors(t *testing.T) {
	h := newHarness(t)

	// Well-formed and belonging to nothing. Not a typo — the case where a
	// caller holds an identifier that used to be real.
	gone := uuid.New().String()

	testCases := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"an instance that is gone", http.MethodGet, "/api/v1/instances/" + gone, nil},
		{"a task that is gone", http.MethodGet, "/api/v1/tasks/" + gone, nil},
		{"exporting a definition that is gone", http.MethodGet, "/api/v1/definitions/" + gone + "/export", nil},
		{"a project that is gone", http.MethodGet, "/api/v1/projects/" + gone, nil},
		{"a user that is gone", http.MethodGet, "/api/v1/users/" + gone, nil},
		{"evaluating a decision that was never deployed", http.MethodPost, "/api/v1/decisions/evaluate",
			map[string]any{"project_id": h.projID.String(), "decision_key": "no-such-decision", "variables": map[string]any{}}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			status := h.call(tc.method, tc.path, h.token, tc.body, nil)
			if status >= 500 {
				t.Errorf("%s %s returned %d: absent is the caller's business, and a 5xx here spends the error budget the engine is measured by",
					tc.method, tc.path, status)
			}
			if status != http.StatusNotFound {
				t.Errorf("%s %s returned %d, want 404: a caller cannot tell an empty answer from a missing one",
					tc.method, tc.path, status)
			}
		})
	}
}
