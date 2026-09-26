package bpmn_test

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/endpoints/definition"
)

// A deploy's reply says whether the version it made is live, so the designer
// can say "v4 deployed and live" or "v4 staged, v3 still live" without asking
// again. The reply copied the request instead: staging the first version of a
// process answered "not live", although with no other version to start on it
// is the one new instances get.
func TestADeployTellsTheTruthAboutWhetherTheVersionIsLive(t *testing.T) {
	h := newEngineHarness(t, "Deploy Live Project")
	deploy := definition.MakeCreateDefinitionEndpoint(h.svc)
	stage := func(t *testing.T) definition.CreateDefinitionResponse {
		t.Helper()
		reply, err := deploy(h.Ctx(), definition.CreateDefinitionRequest{
			Definition: &entities.ProcessDefinition{
				Project: &entities.Project{ID: h.projID},
				Key:     "refund",
				Nodes: []*entities.Node{
					{ID: "start", Type: entities.StartEvent},
					{ID: "end", Type: entities.EndEvent},
				},
				Flows: []*entities.SequenceFlow{{ID: "f1", SourceRef: "start", TargetRef: "end"}},
			},
			Stage: true,
		})
		if err != nil {
			t.Fatalf("deploy: %v", err)
		}
		response := reply.(definition.CreateDefinitionResponse)
		if response.Err != nil {
			t.Fatalf("deploy: %v", response.Err)
		}
		return response
	}

	first := stage(t)
	if !first.Live {
		t.Errorf("staging the first version of a process replied that v%d is not live, but it is the only version to start on", first.Version)
	}
	second := stage(t)
	if second.Live {
		t.Errorf("staging v%d replied that it is live, though v%d still is", second.Version, first.Version)
	}
}
