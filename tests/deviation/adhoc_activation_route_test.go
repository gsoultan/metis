package deviation_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

func (h *deviationHarness) startResearch(t *testing.T) uuid.UUID {
	t.Helper()
	h.deployed++
	key := "deviation-research-" + uuid.NewString()[:8]
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: key, Name: "Research",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "research", Type: entities.SubProcess, Name: "Research the claim", IsAdHoc: true, CompletionCondition: "done >= 1",
				Nodes: []*entities.Node{{ID: "call-customer", Type: entities.UserTask, Name: "Call the customer", ParentID: "research"}}},
			{ID: "decide", Type: entities.UserTask, Name: "Decide the claim"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "research"},
			{ID: "f2", SourceRef: "research", TargetRef: "decide"},
			{ID: "f3", SourceRef: "decide", TargetRef: "end"},
		},
	}
	if _, err := h.svc.CreateDefinition(h.tenantContext(), def); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	id, err := h.svc.StartProcess(h.tenantContext(), h.projID, key, map[string]any{"done": 0})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return id
}

func (h *deviationHarness) openTasksOn(t *testing.T, instanceID uuid.UUID, nodeID string) int {
	t.Helper()
	tasks, err := h.svc.ListTasks(h.tenantContext(), h.projID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	n := 0
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == nodeID &&
			(task.Status == entities.TaskUnclaimed || task.Status == entities.TaskClaimed) {
			n++
		}
	}
	return n
}

// The route's role was never asserted by a test, and it now carries a reason.
// Nobody signed in, a member and a designer are refused and nothing starts;
// another organization's operator is told there is no such instance; an
// operator's activation is made and their reason kept, and so is an
// administrator's.
func TestOnlyAnOperatorOrAdministratorActivatesAStepAndTheirReasonIsKept(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startResearch(t)
	body := map[string]any{"instance_id": instanceID.String(), "sub_process_node_id": "research",
		"task_node_id": "call-customer", "reason": "the customer asked for a call back"}

	status, reply := h.do(t, http.MethodPost, "", "/api/v1/processes/adhoc/activate", body)
	if status != http.StatusUnauthorized {
		t.Fatalf("nobody signed in activating a step: %d (%s), want 401", status, reply)
	}
	for _, role := range []string{entities.RoleUser, entities.RoleDesigner} {
		status, reply := h.do(t, http.MethodPost, h.signIn(t, "refused-"+role, role), "/api/v1/processes/adhoc/activate", body)
		if status != http.StatusForbidden {
			t.Fatalf("a %s activating a step: %d (%s), want 403", role, status, reply)
		}
	}
	status, reply = h.do(t, http.MethodPost, h.signInElsewhere(t, "outsider-operator", entities.RoleOperator), "/api/v1/processes/adhoc/activate", body)
	if status != http.StatusNotFound {
		t.Fatalf("another organization's operator: %d (%s), want 404", status, reply)
	}
	if n := h.openTasksOn(t, instanceID, "call-customer"); n != 0 {
		t.Fatalf("refused activations opened %d task(s)", n)
	}

	status, reply = h.do(t, http.MethodPost, h.signIn(t, "olga", entities.RoleOperator), "/api/v1/processes/adhoc/activate", body)
	if status != http.StatusOK {
		t.Fatalf("an operator activating a step: %d (%s)", status, reply)
	}
	rows, err := h.svc.ListInstanceDeviations(h.tenantContext(), instanceID)
	if err != nil || len(rows) != 1 || rows[0].Actor != "olga" || rows[0].Reason != "the customer asked for a call back" {
		t.Fatalf("the ledger after the operator's activation: %+v (err %v)", rows, err)
	}

	status, reply = h.do(t, http.MethodPost, h.signIn(t, "adam", entities.RoleAdmin), "/api/v1/processes/adhoc/activate", body)
	if status != http.StatusOK {
		t.Fatalf("an administrator activating a step: %d (%s)", status, reply)
	}
	rows, err = h.svc.ListInstanceDeviations(h.tenantContext(), instanceID)
	if err != nil || len(rows) != 2 || rows[1].Actor != "adam" || rows[1].Reason != "the customer asked for a call back" {
		t.Fatalf("the ledger after the administrator's activation: %+v (err %v)", rows, err)
	}
	if n := h.openTasksOn(t, instanceID, "call-customer"); n != 2 {
		t.Fatalf("two activations opened %d task(s), want two", n)
	}
}

// A reason is optional for an activation, but one that is given follows the
// ledger's rule: longer than the ledger keeps is the caller's to shorten, and
// the step is not started. With no reason at all the step starts.
func TestAnActivationReasonTooLongIsRefusedAndNoneAtAllIsAccepted(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startResearch(t)
	olga := h.signIn(t, "olga", entities.RoleOperator)
	body := map[string]any{"instance_id": instanceID.String(), "sub_process_node_id": "research",
		"task_node_id": "call-customer", "reason": strings.Repeat("x", entities.MaxDeviationReasonLength+1)}

	status, reply := h.do(t, http.MethodPost, olga, "/api/v1/processes/adhoc/activate", body)
	if status != http.StatusBadRequest {
		t.Fatalf("a reason longer than the ledger keeps: %d (%s), want 400", status, reply)
	}
	if !strings.Contains(reply, "the reason is longer than") || strings.Contains(reply, "call-customer") {
		t.Fatalf("the refusal should say what is wrong with the reason and name no step by id: %s", reply)
	}
	if n := h.openTasksOn(t, instanceID, "call-customer"); n != 0 {
		t.Fatalf("the refused activation opened %d task(s)", n)
	}
	if n := h.rowCount(t, instanceID); n != 0 {
		t.Fatalf("the refused activation left %d ledger row(s)", n)
	}

	delete(body, "reason")
	status, reply = h.do(t, http.MethodPost, olga, "/api/v1/processes/adhoc/activate", body)
	if status != http.StatusOK {
		t.Fatalf("an activation with no reason: %d (%s)", status, reply)
	}
	rows, err := h.svc.ListInstanceDeviations(h.tenantContext(), instanceID)
	if err != nil || len(rows) != 1 || rows[0].Actor != "olga" || rows[0].Reason != "" {
		t.Fatalf("the ledger after an activation with no reason: %+v (err %v)", rows, err)
	}
	if n := h.openTasksOn(t, instanceID, "call-customer"); n != 1 {
		t.Fatalf("the activation opened %d task(s), want one", n)
	}
}
