package impl

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A connector dragged from the designer's palette names its catalogue entry,
// connector_id, and no connector_instance_id — the instance is resolved at run
// time from the project. serviceCallTarget only looked for the instance id, so
// every such node got an empty target, and an empty target is a no-op to both
// the breaker group and the rate limiter. A palette-created step calling a
// partner that had fallen over was retried against it with no breaker at all,
// and a calls-a-minute limit an operator set on the connection was skipped.
//
// Root cause: the target was derived from one of the two properties a node can
// name its connector by.
func TestAPaletteConnectorStepHasATarget(t *testing.T) {
	project := uuid.New()
	def := &entities.ProcessDefinition{Project: &entities.Project{ID: project}}
	node := entities.Node{Properties: map[string]any{"connector_id": uuid.NewString()}}

	if target := serviceCallTarget(def, node); target == "" {
		t.Fatal("a step naming its connector by connector_id has no breaker or rate-limit target")
	}
}

// The catalogue is shared by the installation: the Slack entry has one id in
// every project. Keying on it alone would let one organization's dead webhook
// open the breaker — and spend the quota — of every organization using Slack.
func TestTheSameCatalogueConnectorInTwoProjectsIsTwoTargets(t *testing.T) {
	slack := uuid.NewString()
	node := entities.Node{Properties: map[string]any{"connector_id": slack}}

	first := serviceCallTarget(&entities.ProcessDefinition{Project: &entities.Project{ID: uuid.New()}}, node)
	second := serviceCallTarget(&entities.ProcessDefinition{Project: &entities.Project{ID: uuid.New()}}, node)
	if first == second {
		t.Fatalf("two projects' connections share the target %q, so one tenant's failures trip the other's breaker", first)
	}
}

// A step with no project cannot be attributed to anybody's connection. Giving it
// a shared key would pool it with every other unattributed step; giving it none
// is the behaviour it had.
func TestAPaletteConnectorStepWithNoProjectHasNoSharedTarget(t *testing.T) {
	node := entities.Node{Properties: map[string]any{"connector_id": uuid.NewString()}}
	if target := serviceCallTarget(&entities.ProcessDefinition{}, node); target != "" {
		t.Fatalf("a step with no project was given the target %q", target)
	}
	if target := serviceCallTarget(nil, node); target != "" {
		t.Fatalf("a step with no definition was given the target %q", target)
	}
}

func TestTheOtherTargetsAreUnchanged(t *testing.T) {
	def := &entities.ProcessDefinition{Project: &entities.Project{ID: uuid.New()}}
	instance := uuid.NewString()

	for name, tc := range map[string]struct {
		properties map[string]any
		want       string
	}{
		"a named instance wins": {
			map[string]any{"connector_instance_id": instance, "connector_id": uuid.NewString()},
			"connector:" + instance,
		},
		"a plain HTTP task is its host": {
			map[string]any{"http_url": "https://api.example.com/v1/orders"},
			"host:api.example.com",
		},
		"nothing to call": {map[string]any{}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := serviceCallTarget(def, entities.Node{Properties: tc.properties}); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
