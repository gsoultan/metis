package bpmn_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A service task set to run a script is refused at deploy.
//
// The designer offered "Run a script here" on a service task, and the engine
// never runs a service task's script — only a script task's. The step was
// taken for one with nothing to call and skipped, and the process moved on as
// though the script had run.
func TestAServiceTaskSetToRunAScriptIsRefusedAtDeploy(t *testing.T) {
	h := newEngineHarness(t, "Script Service Task Project")

	_, err := h.svc.CreateDefinition(h.Ctx(), &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "score-applicant",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "score", Type: entities.ServiceTask, Name: "Score the applicant", Script: "vars.score = 7",
				Properties: map[string]any{"implementation": "script"}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "score"},
			{ID: "f2", SourceRef: "score", TargetRef: "end"},
		},
	})
	if err == nil {
		t.Fatal("a service task set to run a script deployed; the engine would skip it as if it had run")
	}
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "score") || !strings.Contains(err.Error(), "script task") {
		t.Fatalf("the refusal does not name the step and point to a script task: %v", err)
	}
}
