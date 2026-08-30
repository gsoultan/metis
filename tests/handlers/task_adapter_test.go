package handlers_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/transports/adapters"
)

// A task must reach the client with the things it is completed with.
//
// The protobuf Task had no field for the node type, and the adapter never set
// form_definition even though the field existed. So the inbox saw every task as
// a user task — a manual task offered a form to fill in instead of "Mark as
// Done" — and a task with a custom form rendered the default one, because the
// form never arrived.
func TestTaskToProto_CarriesTheTypeAndTheForm(t *testing.T) {
	due := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	form := `[{"id":"decision","label":"Decision","type":"select","options":["approved","rejected"]}]`

	got := adapters.TaskPBAdapter{Task: entities.Task{
		ID:             uuid.Must(uuid.NewV7()),
		Name:           "Compliance review",
		Description:    "Check the supplier before we onboard them.",
		Type:           entities.NodeType("manualTask"),
		Status:         entities.TaskUnclaimed,
		Instance:       &entities.ProcessInstance{ID: uuid.Must(uuid.NewV7())},
		Assignee:       &entities.User{Username: "carol"},
		FormKey:        "supplier-review",
		FormDefinition: form,
		Priority:       3,
		DueDate:        &due,
	}}.ToProto()

	if got.GetType() != "manualTask" {
		t.Errorf("type = %q, want \"manualTask\" — the inbox cannot tell a manual task from a user task without it", got.GetType())
	}
	if got.GetFormDefinition() != form {
		t.Errorf("form definition did not survive: %q", got.GetFormDefinition())
	}
	if got.GetDescription() == "" {
		t.Error("description was dropped")
	}
	if got.GetFormKey() != "supplier-review" {
		t.Errorf("form key = %q", got.GetFormKey())
	}
	if got.GetAssignee().GetUsername() != "carol" {
		t.Errorf("assignee = %q", got.GetAssignee().GetUsername())
	}
	if got.GetInstance().GetId() == "" {
		t.Error("the instance reference was dropped, so the task cannot be traced back to its process")
	}
	if got.GetDueDate() == "" {
		t.Error("due date was dropped")
	}
}

// A task with nothing optional set must not gain anything on the way out.
func TestTaskToProto_LeavesUnsetFieldsEmpty(t *testing.T) {
	got := adapters.TaskPBAdapter{Task: entities.Task{
		ID:     uuid.Must(uuid.NewV7()),
		Name:   "Bare task",
		Status: entities.TaskUnclaimed,
	}}.ToProto()

	if got.GetDueDate() != "" {
		t.Errorf("due date = %q, want empty for a task with none", got.GetDueDate())
	}
	if got.GetAssignee().GetUsername() != "" {
		t.Errorf("assignee = %q, want empty for an unassigned task", got.GetAssignee().GetUsername())
	}
	if got.GetFormDefinition() != "" {
		t.Errorf("form definition = %q, want empty", got.GetFormDefinition())
	}
}
