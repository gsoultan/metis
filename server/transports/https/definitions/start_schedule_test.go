package definitions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/endpoints/definition"
	"github.com/gsoultan/metis/server/transports/https/definitions"
)

// The start-schedule routes sit beside /definitions/{id}: each has to reach
// its own endpoint with the process it names, and registering them must not
// collide with the routes already there, which ServeMux refuses by panicking.
func TestStartScheduleRoutesReachTheirEndpointsWithTheProcessNamed(t *testing.T) {
	var reached string
	var got definition.StartScheduleRequest
	record := func(name string) func(context.Context, any) (any, error) {
		return func(_ context.Context, request any) (any, error) {
			req, ok := request.(definition.StartScheduleRequest)
			if !ok {
				t.Errorf("%s was handed a %T", name, request)
			}
			reached, got = name, req
			return definition.StartScheduleResponse{}, nil
		}
	}
	other := func(context.Context, any) (any, error) {
		reached = "another route"
		return definition.StartScheduleResponse{}, nil
	}
	mux := http.NewServeMux()
	definitions.RegisterHandlers(mux, definition.Endpoints{
		ListDefinitions: other, CreateDefinition: other, GetDefinition: other, DeleteDefinition: other,
		ExportDefinition: other, ImportDefinition: other, ListJavaScriptConditions: other, ListScriptTasks: other,
		PromoteDefinition: other, ListDefinitionVersions: other, ListLiveVersions: other,
		ScheduleDefinition: other, CancelScheduledDefinition: other, MigrateInstances: other,
		GetStartSchedule:    record("get"),
		PauseStartSchedule:  record("pause"),
		ResumeStartSchedule: record("resume"),
	}, nil)

	const project = "0190a1b2-0000-7000-8000-000000000001"
	body := `{"project_id":"` + project + `","key":"nightly-report"}`
	cases := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{"get", http.MethodGet, "/api/v1/definitions/start-schedule?project_id=" + project + "&key=nightly-report", ""},
		{"pause", http.MethodPost, "/api/v1/definitions/start-schedule/pause", body},
		{"resume", http.MethodPost, "/api/v1/definitions/start-schedule/resume", body},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reached, got = "", definition.StartScheduleRequest{}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body)))
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			if reached != tc.name {
				t.Fatalf("the request reached %q, want %q", reached, tc.name)
			}
			if got.ProjectID != project || got.Key != "nightly-report" {
				t.Fatalf("the endpoint was told %+v", got)
			}
		})
	}
}
