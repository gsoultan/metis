package bpmn_test

import (
	"slices"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// A manual task says who does it the way a user task does, and the task the
// engine opens for it carries that — from a BPMN file as much as from the
// designer. BPMN 2.0.2 §10.3 gives every activity its performers, and its
// Human Interactions give a manual task the same potential owners as a user
// task.
//
// A file's candidate users were dropped on import, so a manual step offered to
// dana and eli opened a task that named neither of them.
func TestAManualTaskFromAFileIsOfferedToThePeopleItNames(t *testing.T) {
	h := newEngineHarness(t, "Manual Task Project")
	ctx := h.Ctx()

	for _, tc := range []struct {
		key, names   string
		wantAssignee string
		wantUsers    []string
		wantGroups   []string
		wantStatus   entities.TaskStatus
	}{
		{
			key: "ship-by-hand", names: `camunda:candidateUsers="dana,eli" camunda:candidateGroups="warehouse"`,
			wantUsers: []string{"dana", "eli"}, wantGroups: []string{"warehouse"}, wantStatus: entities.TaskUnclaimed,
		},
		{
			key: "ship-by-dana", names: `camunda:assignee="dana"`,
			wantAssignee: "dana", wantStatus: entities.TaskClaimed,
		},
	} {
		t.Run(tc.key, func(t *testing.T) {
			file := `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL"
             xmlns:camunda="http://camunda.org/schema/1.0/bpmn">
  <process id="` + tc.key + `" isExecutable="true">
    <startEvent id="start"><outgoing>f1</outgoing></startEvent>
    <manualTask id="ship" name="Ship the parcel" ` + tc.names + `>
      <incoming>f1</incoming><outgoing>f2</outgoing>
    </manualTask>
    <endEvent id="end"><incoming>f2</incoming></endEvent>
    <sequenceFlow id="f1" sourceRef="start" targetRef="ship"/>
    <sequenceFlow id="f2" sourceRef="ship" targetRef="end"/>
  </process>
</definitions>`
			if _, err := h.svc.ImportDefinition(ctx, h.projID, []byte(file)); err != nil {
				t.Fatalf("import: %v", err)
			}
			instanceID, err := h.svc.StartProcess(ctx, h.projID, tc.key, nil)
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			page, err := h.svc.ListTasksByInstancePaged(ctx, instanceID, repocontracts.Pagination{Page: 1, PageSize: 10})
			if err != nil {
				t.Fatalf("list the instance's tasks: %v", err)
			}
			if len(page.Items) != 1 {
				t.Fatalf("the instance opened %d tasks, want 1", len(page.Items))
			}
			task := page.Items[0]

			users := make([]string, 0, len(task.CandidateUsers))
			for _, u := range task.CandidateUsers {
				users = append(users, u.Username)
			}
			groups := make([]string, 0, len(task.CandidateGroups))
			for _, g := range task.CandidateGroups {
				groups = append(groups, g.Name)
			}
			if task.Type != entities.ManualTask || task.AssigneeUsername() != tc.wantAssignee ||
				!slices.Equal(users, tc.wantUsers) || !slices.Equal(groups, tc.wantGroups) {
				t.Fatalf("the manual step %s opened a %s given to %q and offered to %v and %v; "+
					"want a manual task given to %q and offered to %v and %v",
					tc.names, task.Type, task.AssigneeUsername(), users, groups, tc.wantAssignee, tc.wantUsers, tc.wantGroups)
			}
			if task.Status != tc.wantStatus {
				t.Fatalf("the manual step %s opened a task that is %q, want %q", tc.names, task.Status, tc.wantStatus)
			}
		})
	}
}
