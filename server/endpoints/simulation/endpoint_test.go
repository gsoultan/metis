package simulation

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

func TestRunnable(t *testing.T) {
	project := uuid.NewString()
	draft := &entities.ProcessDefinition{Key: "expense-approval"}

	tests := []struct {
		name    string
		req     SimulateRequest
		refused string
	}{
		{name: "a deployed definition", req: SimulateRequest{ProjectID: project, DefinitionKey: "expense-approval"}},
		{name: "a draft definition with no key on the request", req: SimulateRequest{ProjectID: project, Definition: draft}},
		{name: "a draft as BPMN XML", req: SimulateRequest{ProjectID: project, BPMNXML: "<definitions/>"}},
		{name: "an unreadable project", req: SimulateRequest{ProjectID: "nope", DefinitionKey: "k"}, refused: "project_id"},
		{name: "nothing to run", req: SimulateRequest{ProjectID: project}, refused: "definition_key is required"},
		{
			name:    "a draft sent twice",
			req:     SimulateRequest{ProjectID: project, Definition: draft, BPMNXML: "<definitions/>"},
			refused: "not both",
		},
		{name: "a replay on a named process", req: SimulateRequest{ProjectID: project, DefinitionKey: "k", ReplayInstanceID: uuid.NewString()}},
		{name: "a replay on the instance's own process", req: SimulateRequest{ProjectID: project, ReplayInstanceID: uuid.NewString()}},
		{
			name:    "a replay of an unreadable instance",
			req:     SimulateRequest{ProjectID: project, ReplayInstanceID: "nope"},
			refused: "replay_instance_id",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entity, err := runnable(tc.req)
			if tc.refused == "" {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				if entity.IsDraft() != (tc.req.Definition != nil || tc.req.BPMNXML != "") {
					t.Errorf("IsDraft = %v for %+v", entity.IsDraft(), tc.req)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.refused) {
				t.Fatalf("err = %v, want a refusal mentioning %q", err, tc.refused)
			}
		})
	}
}

func TestBatchRefusesACaseItCannotRun(t *testing.T) {
	endpoint := MakeSimulateBatchEndpoint(nil)
	reply, err := endpoint(t.Context(), BatchRequest{Scenarios: []SimulateRequest{
		{ProjectID: uuid.NewString(), DefinitionKey: "k", ReplayInstanceID: "nope"},
	}})
	if err != nil {
		t.Fatalf("endpoint error: %v", err)
	}
	failed := reply.(SimulateBatchResponse).Failed()
	if failed == nil || !strings.Contains(failed.Error(), "case 0") {
		t.Fatalf("Failed() = %v, want case 0 refused before anything ran", failed)
	}
}
