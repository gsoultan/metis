package impl

import (
	"slices"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// Who does a step is part of the step, and a BPMN file has to carry it both
// ways. A task that lost its candidates on the way in is a task nobody was
// named for, which only an administrator or an operator may take — so the
// people it was meant for are refused it.
//
// Candidate users had no attribute here at all: import dropped them and export
// never wrote them, for every kind of task. A manual task names people the way
// a user task does — BPMN 2.0.2 §10.3 gives every activity its performers, and
// its Human Interactions give the manual task the same potential owners as the
// user task — so each is written with the same Camunda attributes. Camunda's
// own engine does not wait on a manual task and ignores them there; bpmn-js
// keeps them on the element as it found them.
func TestWhoDoesAStepSurvivesTheRoundTrip(t *testing.T) {
	for _, kind := range []entities.NodeType{entities.UserTask, entities.ManualTask} {
		t.Run(string(kind), func(t *testing.T) {
			parser := &BPMNXMLParser{}
			src := `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL"
             xmlns:camunda="http://camunda.org/schema/1.0/bpmn">
  <process id="p" isExecutable="true">
    <` + string(kind) + ` id="given" camunda:assignee="dana"/>
    <` + string(kind) + ` id="offered" camunda:candidateUsers="dana, eli" camunda:candidateGroups="warehouse,finance"/>
  </process>
</definitions>`

			def, err := parser.Parse(strings.NewReader(src))
			if err != nil {
				t.Fatalf("Parse returned an error: %v", err)
			}
			checkWhoDoesIt(t, def, kind, "after import")

			out, err := parser.Export(def)
			if err != nil {
				t.Fatalf("Export returned an error: %v", err)
			}
			if !strings.Contains(string(out), `camunda:candidateUsers="dana,eli"`) {
				t.Errorf("the exported %s does not name its candidate users as Camunda does:\n%s", kind, out)
			}
			again, err := parser.Parse(strings.NewReader(string(out)))
			if err != nil {
				t.Fatalf("re-parsing the exported file failed: %v", err)
			}
			checkWhoDoesIt(t, again, kind, "after the round trip")
		})
	}
}

// checkWhoDoesIt holds the two steps of the round-trip source to who they name.
func checkWhoDoesIt(t *testing.T, def *entities.ProcessDefinition, kind entities.NodeType, stage string) {
	t.Helper()
	given := nodeByID(t, def, "given")
	if given.Type != kind || given.Assignee != "dana" {
		t.Errorf("%s: the %s given to dana is a %s given to %q", stage, kind, given.Type, given.Assignee)
	}
	offered := nodeByID(t, def, "offered")
	if got := usernamesOf(offered.CandidateUsers); !slices.Equal(got, []string{"dana", "eli"}) {
		t.Errorf("%s: the %s offered to dana and eli is offered to %v", stage, kind, got)
	}
	if got := groupNamesOf(offered.CandidateGroups); !slices.Equal(got, []string{"warehouse", "finance"}) {
		t.Errorf("%s: the %s offered to warehouse and finance is offered to %v", stage, kind, got)
	}
}

func usernamesOf(users []*entities.User) []string {
	names := make([]string, 0, len(users))
	for _, u := range users {
		names = append(names, u.Username)
	}
	return names
}

func groupNamesOf(groups []*entities.Group) []string {
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.Name)
	}
	return names
}
