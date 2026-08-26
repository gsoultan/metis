package slo

import (
	"net/http"
	"testing"
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
