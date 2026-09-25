package bpmn_test

import (
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

// The designer suggests a due date "such as PT24H or 2026-03-01", and task
// creation read only RFC 3339 instants. Both suggestions were dropped without
// a word: the task had no due date, so it was never late and never counted as
// at risk, while the process author believed it had a deadline.
func TestADueDateWrittenAsTheDesignerSuggestsReachesTheTask(t *testing.T) {
	h := newEngineHarness(t, "Due Date Project")
	ctx := h.Ctx()

	for _, tc := range []struct {
		key, due string
		want     func(created time.Time) time.Time
	}{
		{key: "due-in-a-day", due: "PT24H", want: func(created time.Time) time.Time { return created.Add(24 * time.Hour) }},
		{key: "due-on-a-date", due: "2026-03-01", want: func(time.Time) time.Time { return time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC) }},
		{key: "due-at-an-instant", due: "2026-03-01T09:30:00Z", want: func(time.Time) time.Time { return time.Date(2026, 3, 1, 9, 30, 0, 0, time.UTC) }},
	} {
		t.Run(tc.due, func(t *testing.T) {
			if _, err := h.svc.CreateDefinition(ctx, &entities.ProcessDefinition{
				Project: &entities.Project{ID: h.projID},
				Key:     tc.key,
				Nodes: []*entities.Node{
					{ID: "start", Type: entities.StartEvent},
					{ID: "review", Type: entities.UserTask, Name: "Review", DueDate: tc.due},
					{ID: "end", Type: entities.EndEvent},
				},
				Flows: []*entities.SequenceFlow{
					{ID: "f1", SourceRef: "start", TargetRef: "review"},
					{ID: "f2", SourceRef: "review", TargetRef: "end"},
				},
			}); err != nil {
				t.Fatalf("deploy: %v", err)
			}
			started := time.Now()
			instanceID, err := h.svc.StartProcess(ctx, h.projID, tc.key, nil)
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			tasks, err := h.svc.ListTasks(ctx, h.projID)
			if err != nil {
				t.Fatalf("list tasks: %v", err)
			}
			for _, task := range tasks {
				if task.Instance == nil || task.Instance.ID != instanceID {
					continue
				}
				if task.DueDate == nil {
					t.Fatalf("a task due %q has no due date", tc.due)
				}
				if drift := task.DueDate.Sub(tc.want(started)).Abs(); drift > time.Minute {
					t.Fatalf("a task due %q is due %v, want %v", tc.due, task.DueDate, tc.want(started))
				}
				return
			}
			t.Fatal("no task was created")
		})
	}
}
