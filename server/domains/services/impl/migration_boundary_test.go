package impl

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/repositories/models"
)

// A boundary event may be mapped only to a boundary event, and then only to
// one that watches the step its own step is mapped to. Each mapping gets the
// refusal that is its own, and a sound one gets none.
func TestABoundaryEventIsMappedOnlyToABoundaryEventOnTheSameStep(t *testing.T) {
	t.Parallel()
	source := map[string]models.FlowNode{
		"approve":  {ID: "approve", Name: "Approve", Type: models.UserTask},
		"deadline": {ID: "deadline", Name: "Three days passed", Type: models.BoundaryEvent, AttachedToRef: "approve"},
	}
	target := map[string]models.FlowNode{
		"review":   {ID: "review", Name: "Review", Type: models.UserTask},
		"escalate": {ID: "escalate", Name: "Escalate", Type: models.UserTask},
		"timeout":  {ID: "timeout", Name: "Timed out", Type: models.BoundaryEvent, AttachedToRef: "review"},
		"late":     {ID: "late", Name: "Late", Type: models.BoundaryEvent, AttachedToRef: "escalate"},
	}
	cases := []struct {
		name    string
		mapping map[string]string
		want    string
	}{
		{"onto a step", map[string]string{"approve": "review", "deadline": "review"}, "only to a boundary event"},
		{"onto a boundary event watching another step", map[string]string{"approve": "review", "deadline": "late"}, "would detach a boundary event"},
		{"onto the boundary event watching the step its own is mapped to", map[string]string{"approve": "review", "deadline": "timeout"}, ""},
		{"a step onto a step", map[string]string{"approve": "review"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			refusals := boundaryRefusals(source, target, c.mapping)
			if c.want == "" {
				if len(refusals) != 0 {
					t.Fatalf("a sound mapping was refused: %v", refusals)
				}
				return
			}
			if len(refusals) != 1 || !strings.Contains(refusals[0], c.want) {
				t.Fatalf("want the one refusal saying %q, got %v", c.want, refusals)
			}
		})
	}
}

// What the landing refusal adds for boundary events agrees in number with how
// many there are, and says nothing when there are none.
func TestTheAdviceForBoundaryEventsAgreesInNumber(t *testing.T) {
	t.Parallel()
	source := map[string]models.FlowNode{
		"approve":  {ID: "approve", Name: "Approve", Type: models.UserTask},
		"deadline": {ID: "deadline", Name: "Three days passed", Type: models.BoundaryEvent, AttachedToRef: "approve"},
		"withdrew": {ID: "withdrew", Name: "The customer withdrew", Type: models.BoundaryEvent, AttachedToRef: "approve"},
	}
	if got := boundaryAdvice(source, []string{"approve"}); got != "" {
		t.Errorf("no boundary event among the nodes, and advice all the same: %q", got)
	}
	one := boundaryAdvice(source, []string{"approve", "deadline"})
	for _, want := range []string{`"Three days passed" is a boundary event: it can be mapped only to a boundary event`, "name the event in the same decision"} {
		if !strings.Contains(one, want) {
			t.Errorf("for one event the advice does not say %q: %s", want, one)
		}
	}
	several := boundaryAdvice(source, []string{"deadline", "withdrew"})
	for _, want := range []string{
		`"Three days passed" and "The customer withdrew" are boundary events: each can be mapped only to a boundary event`,
		"name its events in the same decision",
	} {
		if !strings.Contains(several, want) {
			t.Errorf("for several events the advice does not say %q: %s", want, several)
		}
	}
	if strings.Contains(several, " is a boundary event") {
		t.Errorf("several events, and the advice says \"is\": %s", several)
	}
}
