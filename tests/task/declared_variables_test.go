package task_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// Completing a task set whatever variables the request carried: the service
// wrote each one into the instance. So whoever completed an approval could
// also rewrite the amount being approved, or name somebody else as the one who
// approved it — business data beyond their step. A task sets only the
// variables its form declares, the id of each of its fields, and a completion
// carrying any other is refused before anything changes.

// approval is a step assigned to alice whose form asks whether the refund is
// approved and, only when it is not, why.
func approval() entities.Node {
	return entities.Node{
		Name: "Approve the refund", Type: entities.UserTask, Assignee: "alice",
		Properties: map[string]any{"form_definition": []any{
			map[string]any{"id": "approved", "label": "Approved", "type": "boolean"},
			map[string]any{"id": "reason", "label": "Why not", "type": "textarea",
				"logic": map[string]any{"hiddenIf": "data.approved == true"}},
		}},
	}
}

// what the form in approval() says about a variable it has no field for
const refusedAmount = "this task's form has no field named amount; a task can set only the variables its form declares"

func TestCompletingATaskCannotSetAVariableItsFormDoesNotDeclare(t *testing.T) {
	h := newTaskHarness(t)
	taskID, instanceID := h.openTaskWith(t, approval(), map[string]any{"amount": 120})

	status, body := h.complete(t, "alice", taskID, map[string]any{"approved": true, "amount": 1200000, "approved_by": "mallory"})
	if status != http.StatusBadRequest {
		t.Fatalf("alice approving the refund and rewriting its amount and approver: got %d (%s), want 400",
			status, strings.TrimSpace(body))
	}
	want := "this task's form has no fields named amount, approved_by; a task can set only the variables its form declares"
	if !strings.Contains(body, want) {
		t.Fatalf("the refusal reads %s; want it to name what was refused, sorted, and why: %q", strings.TrimSpace(body), want)
	}

	// Refused before anything changed: the task is still hers to do, and the
	// instance holds what it held — not even the one variable that was declared.
	if got := h.taskStatus(t, taskID); got != string(entities.TaskClaimed) {
		t.Fatalf("after the refusal the task is %q, want it still open", got)
	}
	vars := h.instanceVariables(t, instanceID)
	if fmt.Sprint(vars["amount"]) != "120" {
		t.Fatalf("after the refusal the amount is %v, want the 120 the refund was raised for", vars["amount"])
	}
	for _, name := range []string{"approved", "approved_by"} {
		if value, set := vars[name]; set {
			t.Fatalf("after the refusal the instance holds %s = %v, from a completion that did not happen", name, value)
		}
	}

	// What the form asks for is set, and the process moves on.
	status, body = h.complete(t, "alice", taskID, map[string]any{"approved": false, "reason": "the receipt is missing"})
	if status != http.StatusOK {
		t.Fatalf("alice refusing the refund with the form's two fields: got %d (%s), want 200", status, strings.TrimSpace(body))
	}
	if got := h.taskStatus(t, taskID); got != string(entities.TaskCompleted) {
		t.Fatalf("after alice completed it the task is %q, want completed", got)
	}
	vars = h.instanceVariables(t, instanceID)
	if vars["approved"] != false || vars["reason"] != "the receipt is missing" || fmt.Sprint(vars["amount"]) != "120" {
		t.Fatalf("after alice completed it the instance holds %v; want approved false, her reason, and the amount unchanged", vars)
	}
}

// A field the form hides is still one of its fields. The inbox sends every
// field's value, the hidden ones' too, so refusing them would refuse the
// inbox's own submission.
func TestAFieldTheFormHidesIsStillDeclared(t *testing.T) {
	h := newTaskHarness(t)
	taskID, instanceID := h.openTaskWith(t, approval(), nil)

	// reason is hidden while approved is true: this is the inbox's submission.
	if status, body := h.complete(t, "alice", taskID, map[string]any{"approved": true, "reason": ""}); status != http.StatusOK {
		t.Fatalf("alice approving with the hidden field sent as the inbox sends it: got %d (%s), want 200",
			status, strings.TrimSpace(body))
	}
	if vars := h.instanceVariables(t, instanceID); vars["approved"] != true {
		t.Fatalf("after alice approved it the instance holds %v, want approved true", vars)
	}
}

// A task with no form declares nothing, so it sets nothing: absent constraint
// means deny. It still completes — with no variables.
func TestATaskWithNoFormCompletesOnlyWithNoVariables(t *testing.T) {
	h := newTaskHarness(t)
	shipping := entities.Node{Name: "Ship the parcel", Type: entities.UserTask, Assignee: "alice"}
	taskID, instanceID := h.openTaskWith(t, shipping, map[string]any{"amount": 120})

	status, body := h.complete(t, "alice", taskID, map[string]any{"amount": 1})
	if status != http.StatusBadRequest {
		t.Fatalf("alice completing a task with no form and setting the amount: got %d (%s), want 400",
			status, strings.TrimSpace(body))
	}
	if want := "this task has no form to declare amount; a task can set only the variables its form declares"; !strings.Contains(body, want) {
		t.Fatalf("the refusal reads %s; want %q", strings.TrimSpace(body), want)
	}
	if got := h.taskStatus(t, taskID); got != string(entities.TaskClaimed) {
		t.Fatalf("after the refusal the task is %q, want it still open", got)
	}
	if vars := h.instanceVariables(t, instanceID); fmt.Sprint(vars["amount"]) != "120" {
		t.Fatalf("after the refusal the amount is %v, want 120", vars["amount"])
	}

	if status, body := h.complete(t, "alice", taskID, map[string]any{}); status != http.StatusOK {
		t.Fatalf("alice completing a task with no form and no variables: got %d (%s), want 200", status, strings.TrimSpace(body))
	}
	if got := h.taskStatus(t, taskID); got != string(entities.TaskCompleted) {
		t.Fatalf("after alice completed it the task is %q, want completed", got)
	}
}

// A task can name a stored form by its key instead of carrying one. The stored
// form's fields are its declaration, as an inline form's are — and a key that
// names no form stored here declares nothing.
func TestAStoredFormDeclaresTheVariablesOfATaskThatNamesIt(t *testing.T) {
	h := newTaskHarness(t)
	h.storeForm(t, "refund-approval", map[string]any{"fields": []any{
		map[string]any{"id": "approved", "label": "Approved", "type": "boolean"},
		map[string]any{"id": "reason", "label": "Why not", "type": "textarea"},
	}})
	step := entities.Node{Name: "Approve the refund", Type: entities.UserTask, Assignee: "alice", FormKey: "refund-approval"}

	taskID, instanceID := h.openTaskWith(t, step, map[string]any{"amount": 120})
	status, body := h.complete(t, "alice", taskID, map[string]any{"approved": true, "amount": 1})
	if status != http.StatusBadRequest || !strings.Contains(body, refusedAmount) {
		t.Fatalf("alice setting the amount on a task whose stored form has no such field: got %d (%s); want 400 saying %q",
			status, strings.TrimSpace(body), refusedAmount)
	}
	if vars := h.instanceVariables(t, instanceID); fmt.Sprint(vars["amount"]) != "120" {
		t.Fatalf("after the refusal the amount is %v, want 120", vars["amount"])
	}
	if status, body := h.complete(t, "alice", taskID, map[string]any{"approved": false, "reason": "no receipt"}); status != http.StatusOK {
		t.Fatalf("alice completing with the stored form's fields: got %d (%s), want 200", status, strings.TrimSpace(body))
	}
	if vars := h.instanceVariables(t, instanceID); vars["approved"] != false || vars["reason"] != "no receipt" {
		t.Fatalf("after alice completed it the instance holds %v; want the stored form's two fields", vars)
	}

	// A form kept somewhere else — an imported process's embedded form — is
	// not one Metis can read the fields of.
	elsewhere := step
	elsewhere.FormKey = "embedded:app:forms/refund.html"
	taskID, _ = h.openTaskWith(t, elsewhere, nil)
	if status, body := h.complete(t, "alice", taskID, map[string]any{"approved": true}); status != http.StatusBadRequest {
		t.Fatalf("alice setting a variable on a task whose form is not stored here: got %d (%s), want 400",
			status, strings.TrimSpace(body))
	}
	if status, body := h.complete(t, "alice", taskID, map[string]any{}); status != http.StatusOK {
		t.Fatalf("alice completing that task with no variables: got %d (%s), want 200", status, strings.TrimSpace(body))
	}
}

// legacyUndeclaredVariables is the setting that brings the old rule back.
const legacyUndeclaredVariables = "METIS_ALLOW_UNDECLARED_TASK_VARIABLES"

// An installation whose integrations complete tasks with variables no form
// declares can have the old rule back for a migration window. While it is on,
// each step completed that way is named in the log once, with the variables it
// was sent, so its form can be given those fields before the setting goes.
func TestTheLegacySettingSetsUndeclaredVariablesAndNamesEachStepOnce(t *testing.T) {
	t.Setenv(legacyUndeclaredVariables, "true")
	logs := captureLogs(t)
	h := newTaskHarness(t)

	// The same step twice: named once.
	for range 2 {
		taskID, instanceID := h.openTaskWith(t, approval(), map[string]any{"amount": 120})
		status, body := h.complete(t, "alice", taskID, map[string]any{"approved": true, "amount": 5, "approved_by": "mallory"})
		if status != http.StatusOK {
			t.Fatalf("with %s on, alice completing with variables the form does not declare: got %d (%s), want 200",
				legacyUndeclaredVariables, status, strings.TrimSpace(body))
		}
		if vars := h.instanceVariables(t, instanceID); fmt.Sprint(vars["amount"]) != "5" || vars["approved_by"] != "mallory" {
			t.Fatalf("with %s on, the instance holds %v; want every variable the completion carried", legacyUndeclaredVariables, vars)
		}
	}
	// Another step: named in a line of its own.
	other := h.assignTaskTo(t, "alice")
	if status, body := h.complete(t, "alice", other, map[string]any{"approved": true, "amount": 7}); status != http.StatusOK {
		t.Fatalf("with %s on, alice completing another step with an undeclared variable: got %d (%s), want 200",
			legacyUndeclaredVariables, status, strings.TrimSpace(body))
	}

	var said []string
	for _, line := range logs.linesNaming(legacyUndeclaredVariables) {
		said = append(said, fmt.Sprintf("%v %v/%v %v", line["level"], line["definition"], line["node"], line["variables"]))
	}
	want := []string{"warn one-step/step [amount approved_by]", "warn actor-approval/approve [amount]"}
	if strings.Join(said, "; ") != strings.Join(want, "; ") {
		t.Fatalf("with %s on, the log named %q; want each step once, with what it was sent: %q",
			legacyUndeclaredVariables, said, want)
	}
}

// complete posts a completion as the person named, with the variables given.
func (h *taskHarness) complete(t *testing.T, who, taskID string, variables map[string]any) (int, string) {
	t.Helper()
	return h.post(t, h.tokens[who], "/api/v1/tasks/"+taskID+"/complete", map[string]any{"variables": variables})
}

// instanceVariables reads an instance's variables as the engine holds them.
func (h *taskHarness) instanceVariables(t *testing.T, instanceID uuid.UUID) map[string]any {
	t.Helper()
	instance, err := h.svc.GetInstance(h.tenantContext(), instanceID)
	if err != nil {
		t.Fatalf("read instance %s: %v", instanceID, err)
	}
	return instance.Variables
}

// storeForm keeps a form in the harness's project under key.
func (h *taskHarness) storeForm(t *testing.T, key string, schema map[string]any) {
	t.Helper()
	repo := repositories.NewRepository(testutils.StormConn(h.db))
	if err := repo.Form().Create(h.tenantContext(), models.FormModel{
		Base:      models.Base{ID: models.FromUUID(uuid.Must(uuid.NewV7()))},
		ProjectID: models.FromUUID(h.projID),
		Key:       key,
		Name:      key,
		Schema:    schema,
	}); err != nil {
		t.Fatalf("store the form %q: %v", key, err)
	}
}

// tenantContext is a context inside the harness's organization.
func (h *taskHarness) tenantContext() context.Context {
	return entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: h.orgID.String()})
}
