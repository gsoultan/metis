package validation

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

func scheduleOf(every string) map[string]any {
	return map[string]any{entities.StartScheduleProperty: map[string]any{
		"every": every, "at": "09:00", "time_zone": "Asia/Jakarta",
	}}
}

func errorsFor(nodes ...*entities.Node) []string {
	visitor := NewVisitor()
	(&entities.ProcessDefinition{Key: "scheduled", Nodes: nodes}).Accept(visitor)
	return visitor.Errors()
}

// A schedule the engine can keep, on the process's own start step, deploys.
func TestAStartStepWithAScheduleDeploys(t *testing.T) {
	start := &entities.Node{ID: "start", Type: entities.StartEvent, Properties: scheduleOf("day")}
	if errs := errorsFor(start); len(errs) != 0 {
		t.Fatalf("a daily schedule was refused: %v", errs)
	}
}

// A schedule nothing would read, or that cannot run, is refused at deploy
// with the step and the reason, instead of deploying a process that never
// starts.
func TestAScheduleTheEngineCannotKeepIsRefusedAtDeploy(t *testing.T) {
	both := scheduleOf("day")
	both["message_name"] = "order-placed"

	for _, tc := range []struct {
		name   string
		nodes  []*entities.Node
		reason string
	}{
		{"a rule that is not offered",
			[]*entities.Node{{ID: "start", Type: entities.StartEvent, Properties: scheduleOf("fortnight")}},
			"Flow node start: the schedule runs every \"fortnight\""},
		{"a schedule on a step that is not a start",
			[]*entities.Node{{ID: "approve", Type: entities.UserTask, Properties: scheduleOf("day")}},
			"only a start step can start a process on a schedule"},
		{"a schedule on a sub-process's start",
			[]*entities.Node{{ID: "sub", Type: entities.SubProcess, Nodes: []*entities.Node{
				{ID: "inner-start", Type: entities.StartEvent, Properties: scheduleOf("day")},
			}}},
			"Flow node inner-start has a schedule, but it is inside sub"},
		{"a schedule on a start that names a sub-process as its parent",
			[]*entities.Node{
				{ID: "sub", Type: entities.SubProcess},
				{ID: "inner-start", Type: entities.StartEvent, ParentID: "sub", Properties: scheduleOf("day")},
			},
			"Flow node inner-start has a schedule, but it is inside sub"},
		{"a schedule and a message on one start",
			[]*entities.Node{{ID: "start", Type: entities.StartEvent, Properties: both}},
			"both a schedule and a message_name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := errorsFor(tc.nodes...)
			if !strings.Contains(strings.Join(errs, "\n"), tc.reason) {
				t.Fatalf("errors %q do not mention %q", errs, tc.reason)
			}
		})
	}
}
