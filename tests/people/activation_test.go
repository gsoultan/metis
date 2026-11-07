package people_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
)

type activationReply struct {
	Result entities.ActivationResult `json:"result"`
	Err    string                    `json:"err"`
}

func (w world) setActive(t *testing.T, person uuid.UUID, body map[string]any) (int, activationReply) {
	t.Helper()
	code, raw := w.do(t, http.MethodPut, "/api/v1/participants/"+person.String()+"/active?project_id="+w.project.String(), w.userToken, body)
	var reply activationReply
	if code == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &reply); err != nil {
			t.Fatalf("read %s: %v", raw, err)
		}
	}
	return code, reply
}

func (w world) holder(t *testing.T, id uuid.UUID) (string, entities.TaskStatus) {
	t.Helper()
	task, err := w.svc.GetTask(entities.WithTenantContext(t.Context(), w.tenant(t)), id)
	if err != nil {
		t.Fatalf("read task %s: %v", id, err)
	}
	return task.AssigneeUsername(), task.Status
}

func (w world) active(t *testing.T, username string) bool {
	t.Helper()
	people, err := w.svc.ListWorkflowUsers(entities.WithTenantContext(t.Context(), w.tenant(t)), w.project, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range people {
		if p.Username == username {
			return p.Active
		}
	}
	t.Fatalf("no person %s", username)
	return false
}

// TestDeactivatingSomebodyHandsTheirWorkOver: the tasks a person holds are
// listed, and deactivating them hands those tasks to the account named, with
// the reason, so nothing is left waiting for somebody who can no longer act.
func TestDeactivatingSomebodyHandsTheirWorkOver(t *testing.T) {
	w := newWorld(t)
	bo := w.seedPerson(t, "bo", "bo@example.com")
	first := w.startTask(t, w.project, "held-by-bo", entities.Node{Assignee: "bo"})
	second := w.startTask(t, w.project, "also-held-by-bo", entities.Node{Assignee: "bo"})
	w.startTask(t, w.project, "held-by-dana", entities.Node{Assignee: "dana"})

	code, raw := w.do(t, http.MethodGet, "/api/v1/participants/"+bo.String()+"/held-tasks?project_id="+w.project.String(), w.userToken, nil)
	var held struct {
		Tasks []entities.HeldTask `json:"tasks"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(raw), &held) != nil || len(held.Tasks) != 2 {
		t.Fatalf("bo's held tasks: %d %s, want his two", code, raw)
	}

	// Moving somebody's work without saying why is refused, and refused
	// before anybody is deactivated.
	if code, _ := w.setActive(t, bo, map[string]any{"active": false, "work": map[string]any{"do": "hand_over", "to": "dana"}}); code != http.StatusBadRequest {
		t.Fatalf("a hand-over with no reason: %d, want 400", code)
	}
	if !w.active(t, "bo") {
		t.Fatal("bo was deactivated by a refused request")
	}

	code, reply := w.setActive(t, bo, map[string]any{"active": false, "work": map[string]any{
		"do": "hand_over", "to": "dana", "reason": "Bo has left; Dana covers his approvals",
	}})
	if code != http.StatusOK || reply.Result.Moved != 2 || len(reply.Result.Kept) != 0 {
		t.Fatalf("deactivate bo: %d %+v, want both tasks moved", code, reply)
	}
	if w.active(t, "bo") {
		t.Error("bo is still active")
	}
	for _, task := range []entities.Task{first, second} {
		if who, status := w.holder(t, task.ID); who != "dana" || status != entities.TaskClaimed {
			t.Errorf("%s: held by %q (%s), want dana", task.Name, who, status)
		}
	}
	if _, stranded := w.stranded(t, w.userToken); len(stranded) != 0 {
		t.Errorf("stranded after the hand-over: %+v", stranded)
	}

	// Reactivating moves nothing back.
	code, reply = w.setActive(t, bo, map[string]any{"active": true})
	if code != http.StatusOK || !w.active(t, "bo") || reply.Result.Moved != 0 {
		t.Fatalf("reactivate bo: %d %+v", code, reply)
	}
	if who, _ := w.holder(t, first.ID); who != "dana" {
		t.Errorf("reactivating bo took the task back from dana: held by %q", who)
	}
}

// TestDeactivatingSomebodyCanReleaseOrKeepTheirWork: releasing puts their
// tasks back on offer; keeping leaves them, named in the result and in the
// stranded report; a hand-over the rules refuse keeps the task, with why.
func TestDeactivatingSomebodyCanReleaseOrKeepTheirWork(t *testing.T) {
	w := newWorld(t)
	cy := w.seedPerson(t, "cy", "cy@example.com")
	released := w.startTask(t, w.project, "held-by-cy", entities.Node{Assignee: "cy", CandidateGroups: []*entities.Group{{Name: "night-shift"}}})
	code, reply := w.setActive(t, cy, map[string]any{"active": false, "work": map[string]any{"do": "release", "reason": "Cy has left"}})
	if code != http.StatusOK || reply.Result.Moved != 1 {
		t.Fatalf("deactivate cy, releasing: %d %+v", code, reply)
	}
	if who, status := w.holder(t, released.ID); who != "" || status != entities.TaskUnclaimed {
		t.Errorf("cy's task: held by %q (%s), want it back on offer", who, status)
	}

	di := w.seedPerson(t, "di", "di@example.com")
	kept := w.startTask(t, w.project, "held-by-di", entities.Node{Assignee: "di"})
	code, reply = w.setActive(t, di, map[string]any{"active": false})
	if code != http.StatusOK || reply.Result.Moved != 0 || len(reply.Result.Kept) != 1 || reply.Result.Kept[0].TaskID != kept.ID {
		t.Fatalf("deactivate di, keeping: %d %+v, want her task named as kept", code, reply)
	}
	if _, stranded := w.stranded(t, w.userToken); stranded[kept.ID.String()].Kind != entities.StrandedAssigneeInactive {
		t.Errorf("di's kept task is not reported as stranded: %+v", stranded)
	}

	ed := w.seedPerson(t, "ed", "ed@example.com")
	refused := w.startTask(t, w.project, "held-by-ed", entities.Node{Assignee: "ed"})
	code, reply = w.setActive(t, ed, map[string]any{"active": false, "work": map[string]any{"do": "hand_over", "to": "nobody-here", "reason": "Ed has left"}})
	if code != http.StatusOK || len(reply.Result.Kept) != 1 || reply.Result.Kept[0].TaskID != refused.ID || reply.Result.Kept[0].Why == "" {
		t.Fatalf("hand ed's work to nobody: %d %+v, want the task kept with why", code, reply)
	}
}
