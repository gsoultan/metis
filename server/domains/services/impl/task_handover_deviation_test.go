package impl

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A ledger row is written exactly when slice 2 asks for a reason: the caller
// is not the holder, or an administrator holder sends the task to somebody it
// was not offered to.
func TestAHandOverIsLedgeredExactlyWhenItNeedsAReason(t *testing.T) {
	t.Parallel()
	cases := []struct {
		record handOverRecord
		want   bool
	}{
		{handOverRecord{byHolder: true}, false},
		{handOverRecord{byHolder: false}, true},
		{handOverRecord{byHolder: true, candidateOverride: true}, true},
	}
	for _, c := range cases {
		if got := c.record.needsLedger(); got != c.want {
			t.Errorf("%+v.needsLedger() = %v, want %v", c.record, got, c.want)
		}
	}
}

func TestHandOverDeviation(t *testing.T) {
	t.Parallel()
	taskID, instanceID, projectID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	task := entities.Task{
		ID: taskID, Name: "Approve the refund",
		Project: &entities.Project{ID: projectID}, Instance: &entities.ProcessInstance{ID: instanceID},
		Node: &entities.Node{ID: "approve"},
	}
	cases := []struct {
		auditType     string
		record        handOverRecord
		kind          entities.DeviationKind
		before, after map[string]any
	}{
		{EventTaskAssigned, handOverRecord{actor: "boss", previousHolder: "alice", target: "bob", reason: "r"},
			entities.DeviationReassign, map[string]any{"assignee": "alice"}, map[string]any{"assignee": "bob"}},
		{EventTaskAssigned, handOverRecord{actor: "olga", target: "bob", reason: "r"},
			entities.DeviationReassign, map[string]any{"assignee": nil}, map[string]any{"assignee": "bob"}},
		{EventTaskDelegated, handOverRecord{actor: "boss", previousHolder: "alice", owner: "alice", target: "bob", reason: "r"},
			entities.DeviationDelegate, map[string]any{"assignee": "alice"},
			map[string]any{"assignee": "bob", "owner": "alice", "delegation_state": "pending"}},
		{EventTaskResolved, handOverRecord{actor: "boss", previousHolder: "bob", owner: "alice", target: "alice", reason: "r"},
			entities.DeviationResolve, map[string]any{"assignee": "bob", "delegation_state": "pending"},
			map[string]any{"assignee": "alice", "delegation_state": "resolved"}},
		{EventTaskUnclaimed, handOverRecord{actor: "boss", previousHolder: "alice", reason: "r"},
			entities.DeviationRelease, map[string]any{"assignee": "alice"}, map[string]any{"assignee": nil}},
		{EventTaskEdited, handOverRecord{actor: "boss", holder: "alice", reason: "r", changes: map[string]any{
			"name": fieldChange("Approve", "Approve the refund"), "priority": fieldChange(0, 90)}},
			entities.DeviationTaskEdit, map[string]any{"name": "Approve", "priority": 0},
			map[string]any{"name": "Approve the refund", "priority": 90}},
	}
	for _, c := range cases {
		d := handOverDeviation(task, c.auditType, c.record)
		if d.Kind != c.kind || d.Origin != entities.DeviationOriginTask || d.Status != entities.DeviationApplied ||
			d.Scope != entities.DeviationScopeTask || d.Actor != c.record.actor || d.Reason != c.record.reason {
			t.Errorf("%s: %+v", c.auditType, d)
		}
		if d.Task == nil || d.Task.ID != taskID || d.Instance.ID != instanceID || d.Project.ID != projectID ||
			d.Node == nil || d.Node.ID != "approve" || d.Node.Name != "Approve the refund" {
			t.Errorf("%s: names the wrong task, instance, project or step: %+v", c.auditType, d)
		}
		wantBefore := map[string]any{"tasks": map[string]any{taskID.String(): c.before}}
		wantAfter := map[string]any{"tasks": map[string]any{taskID.String(): c.after}}
		if !reflect.DeepEqual(d.Before, wantBefore) || !reflect.DeepEqual(d.After, wantAfter) {
			t.Errorf("%s: before %v after %v, want %v and %v", c.auditType, d.Before, d.After, wantBefore, wantAfter)
		}
	}

	// One run of a repeating step is the iteration, and an override says so.
	task.IterationID = "2"
	d := handOverDeviation(task, EventTaskAssigned, handOverRecord{actor: "boss", previousHolder: "alice", target: "bob", reason: "r", candidateOverride: true})
	if d.Scope != entities.DeviationScopeIteration || d.IterationID != "2" {
		t.Errorf("a task of iteration 2 is scoped %q / %q", d.Scope, d.IterationID)
	}
	if d.Details["override"] != "not_a_candidate" {
		t.Errorf("an override carries details %v", d.Details)
	}
}
