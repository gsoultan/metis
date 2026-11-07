package approvals_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// lineWithSupervisor is the expense line with its first step given to one
// person by name.
func lineWithSupervisor(supervisor string, withUnitHead bool) map[string]any {
	steps := []map[string]any{{"id": "supervisor", "name": "Supervisor approval", "users": []string{supervisor}}}
	if withUnitHead {
		steps = append(steps, map[string]any{"id": "unit-head", "name": "Unit head approval", "groups": []string{"unit-heads"}})
	}
	return map[string]any{
		"key": "expense", "name": "Expense approval", "allow_revise": true,
		"fields": []map[string]any{{"id": "amount", "label": "Amount", "type": "number"}},
		"steps":  steps,
	}
}

func (w world) defineLine(t *testing.T, line map[string]any) {
	t.Helper()
	if code, body := w.do(t, "dana", http.MethodPost, "/api/v1/projects/"+w.project.String()+"/approval-lines", line); code != http.StatusOK {
		t.Fatalf("define the line: %d %s", code, body)
	}
}

func (w world) behind(t *testing.T) entities.ApprovalTakeover {
	t.Helper()
	code, body := w.do(t, "ada", http.MethodGet, "/api/v1/projects/"+w.project.String()+"/approval-lines/expense/behind", nil)
	var out struct {
		Takeover entities.ApprovalTakeover `json:"takeover"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &out) != nil {
		t.Fatalf("behind: %d %s", code, body)
	}
	return out.Takeover
}

func (w world) move(t *testing.T, ids ...string) entities.ApprovalMoveResult {
	t.Helper()
	code, body := w.do(t, "ada", http.MethodPost, "/api/v1/projects/"+w.project.String()+"/approval-lines/expense/move", map[string]any{"requests": ids})
	var out struct {
		Result entities.ApprovalMoveResult `json:"result"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &out) != nil {
		t.Fatalf("move: %d %s", code, body)
	}
	return out.Result
}

func (w world) get(t *testing.T, id string) entities.ApprovalRequest {
	t.Helper()
	code, body := w.do(t, "rina", http.MethodGet, "/api/v1/approval-requests/"+id, nil)
	return w.request(t, code, body)
}

// Budi leaves, and the line is changed to give his step to Tono. The
// requests already waiting for Budi are listed as still on the old line, and
// an administrator moves the one that should go to Tono; the other is kept
// on the old line, as chosen, and a request sent back to its submitter stays
// hers after the move.
func TestRequestsOnAnOldLineAreListedAndMovedOneByOne(t *testing.T) {
	w := newWorld(t)
	w.defineLine(t, lineWithSupervisor("budi", true))
	waitingForBudi := w.submit(t, map[string]any{"amount": 10})
	keptOnOld := w.submit(t, map[string]any{"amount": 20})
	sentBack := w.submit(t, map[string]any{"amount": 30})
	if code, body := w.decide(t, "budi", sentBack.Steps[0].Waiting.TaskID, map[string]any{"decision": "revise", "comment": "Add the receipt"}); code != http.StatusOK {
		t.Fatalf("send back: %d %s", code, body)
	}

	w.defineLine(t, lineWithSupervisor("tono", true))

	if code, _ := w.do(t, "dana", http.MethodGet, "/api/v1/projects/"+w.project.String()+"/approval-lines/expense/behind", nil); code != http.StatusForbidden {
		t.Errorf("a designer read the requests to move: %d, want 403", code)
	}
	takeover := w.behind(t)
	if takeover.LiveVersion != 2 || len(takeover.Behind) != 1 || takeover.Behind[0].Version != 1 ||
		len(takeover.Behind[0].Requests) != 3 || len(takeover.Behind[0].Plan.Refusals) != 0 {
		t.Fatalf("behind = %+v, want version 1's three requests and a plan that moves them", takeover)
	}
	if first := takeover.Behind[0].Requests[0]; first.Steps[0].Waiting == nil || first.Steps[0].Waiting.Users[0] != "budi" {
		t.Errorf("a request behind does not say who it waits on: %+v", first.Steps[0])
	}

	got := w.move(t, waitingForBudi.InstanceID.String(), sentBack.InstanceID.String())
	if got.Moved != 2 || len(got.Left) != 0 {
		t.Fatalf("move = %+v, want two moved", got)
	}
	moved := w.get(t, waitingForBudi.InstanceID.String())
	if moved.Version != 2 || moved.Steps[0].Waiting == nil || len(moved.Steps[0].Waiting.Users) != 1 || moved.Steps[0].Waiting.Users[0] != "tono" {
		t.Fatalf("the moved request = %+v waiting %+v, want it on version 2 waiting for Tono", moved, moved.Steps[0].Waiting)
	}
	if code, _ := w.decide(t, "budi", moved.Steps[0].Waiting.TaskID, map[string]any{"decision": "approve"}); code == http.StatusOK {
		t.Error("Budi decided a request that was moved to Tono")
	}
	if code, body := w.decide(t, "tono", moved.Steps[0].Waiting.TaskID, map[string]any{"decision": "approve"}); code != http.StatusOK {
		t.Fatalf("Tono approves: %d %s", code, body)
	}

	back := w.get(t, sentBack.InstanceID.String())
	if back.Version != 2 || back.Revision == nil || back.Revision.Assignee != "rina" {
		t.Fatalf("the sent-back request = %+v, want it on version 2 and still Rina's to revise", back)
	}
	if code, body := w.decide(t, "rina", back.Revision.TaskID, map[string]any{"decision": "resubmit", "variables": map[string]any{"amount": 25}}); code != http.StatusOK {
		t.Fatalf("Rina resubmits the moved request: %d %s", code, body)
	}

	kept := w.get(t, keptOnOld.InstanceID.String())
	if kept.Version != 1 || kept.Steps[0].Waiting == nil || kept.Steps[0].Waiting.Users[0] != "budi" {
		t.Fatalf("the request not moved = %+v, want it on version 1, still Budi's", kept)
	}
	if takeover := w.behind(t); len(takeover.Behind) != 1 || len(takeover.Behind[0].Requests) != 1 {
		t.Fatalf("behind after the move = %+v, want only the kept request", takeover)
	}
}

// A request whose step the new line no longer has cannot be moved: it is
// left on its line, with why, and so is one that is not on an older version.
func TestAMoveTheMigrationRefusesLeavesTheRequest(t *testing.T) {
	w := newWorld(t)
	w.defineLine(t, lineWithSupervisor("budi", true))
	atUnitHead := w.submit(t, map[string]any{"amount": 10})
	if code, body := w.decide(t, "budi", atUnitHead.Steps[0].Waiting.TaskID, map[string]any{"decision": "approve"}); code != http.StatusOK {
		t.Fatalf("approve: %d %s", code, body)
	}
	w.defineLine(t, lineWithSupervisor("budi", false))
	fresh := w.submit(t, map[string]any{"amount": 20})

	got := w.move(t, atUnitHead.InstanceID.String(), fresh.InstanceID.String())
	if got.Moved != 0 || len(got.Left) != 2 {
		t.Fatalf("move = %+v, want both left", got)
	}
	for _, left := range got.Left {
		if left.Why == "" {
			t.Errorf("left %s without saying why", left.InstanceID)
		}
	}
	if still := w.get(t, atUnitHead.InstanceID.String()); still.Version != 1 || still.Steps[1].Waiting == nil {
		t.Fatalf("the refused request = %+v, want it on version 1 at the unit head", still)
	}
	if code, _ := w.do(t, "ada", http.MethodPost, "/api/v1/projects/"+w.project.String()+"/approval-lines/expense/move", map[string]any{"requests": []string{}}); code != http.StatusBadRequest {
		t.Errorf("a move of nothing: %d, want 400", code)
	}
}
